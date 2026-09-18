package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/onelegdave/omachat/internal/buildinfo"
	"github.com/onelegdave/omachat/internal/wire"
)

var osChmod = os.Chmod

// maxFrame bounds a single request line so a runaway client cannot exhaust
// memory. Requests are small; replies can be large.
const maxFrame = 1 << 20

// Bound per-client work while allowing independent requests to make progress.
const maxRequestsPerConnection = 16

// Serve accepts plugin connections on the Unix socket until ctx is cancelled.
func (d *Daemon) Serve(ctx context.Context, socketPath string) error {
	// A stale socket from an unclean shutdown would block Listen.
	if err := removeStaleSocket(socketPath); err != nil {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen %s: %w", socketPath, err)
	}
	// The socket carries message content; keep it owner-only.
	if err := osChmod(socketPath, 0o600); err != nil {
		ln.Close()
		os.Remove(socketPath)
		return fmt.Errorf("secure socket: %w", err)
	}
	defer func() {
		ln.Close()
		os.Remove(socketPath)
	}()

	d.log.Info().Str("socket", socketPath).Msg("Listening")

	// Live connections have to be closed explicitly on shutdown. The bar keeps
	// a socket open per monitor and those goroutines sit blocked in Scan();
	// closing only the listener leaves them there, wg.Wait() never returns,
	// and systemd ends up SIGKILLing the daemon after its stop timeout.
	var (
		connMu sync.Mutex
		conns  = make(map[net.Conn]struct{})
	)
	closeAllConns := func() {
		connMu.Lock()
		for c := range conns {
			c.Close()
		}
		clear(conns)
		connMu.Unlock()
	}

	go func() {
		<-ctx.Done()
		ln.Close()
		closeAllConns()
	}()

	var wg sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		connMu.Lock()
		conns[conn] = struct{}{}
		connMu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				connMu.Lock()
				delete(conns, conn)
				connMu.Unlock()
			}()
			d.handleConn(ctx, conn)
		}()
	}
}

// removeStaleSocket deletes a socket file left behind by a crashed daemon,
// but refuses to touch one a live daemon is still listening on.
func removeStaleSocket(path string) error {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if c, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
		c.Close()
		return fmt.Errorf("another omachatd is already listening on %s", path)
	}
	return os.Remove(path)
}

// connWriter serialises writes from the request path and the event pump.
type connWriter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func (w *connWriter) send(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.enc.Encode(v)
}

func (d *Daemon) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-ctx.Done(); conn.Close() }()
	slots := make(chan struct{}, maxRequestsPerConnection)

	w := &connWriter{enc: json.NewEncoder(conn)}

	events, unsubscribe := d.Subscribe()
	defer unsubscribe()

	// Push current status immediately so a freshly-connected plugin renders
	// without having to ask.
	_ = w.send(wire.Event{Event: wire.EventStatus, Network: wire.NetworkGMessages, Data: d.Status()})
	if d.wa != nil {
		_ = w.send(wire.Event{Event: wire.EventStatus, Network: wire.NetworkWhatsApp, Data: d.wa.Status()})
	}
	if d.tg != nil {
		_ = w.send(wire.Event{Event: wire.EventStatus, Network: wire.NetworkTelegram, Data: d.tg.Status()})
	}
	if d.fb != nil {
		_ = w.send(wire.Event{Event: wire.EventStatus, Network: wire.NetworkMessenger, Data: d.fb.Status()})
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-events:
				if !ok {
					return
				}
				if err := w.send(evt); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 8192), maxFrame)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req wire.Request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = w.send(wire.Response{OK: false, Error: "malformed request: " + err.Error()})
			continue
		}
		// Apply backpressure before creating a goroutine.
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		go func(req wire.Request) {
			defer func() { <-slots }()
			// A pre-change config snapshot must not be written after the setter's
			// restart-required acknowledgement on another concurrent request.
			if globalSetting(req.Method) || req.Method == wire.MethodSetEnabledServices {
				d.configResponseMu.Lock()
				defer d.configResponseMu.Unlock()
			}
			resp := d.dispatch(ctx, req)
			if err := w.send(resp); err != nil {
				cancel()
				return
			}
			if req.Method == wire.MethodSetEnabledServices {
				d.acknowledgeServiceChange(resp)
			}
		}(req)
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		d.log.Debug().Err(err).Msg("Connection read ended")
	}
}

// decodeParams re-marshals the loosely-typed params into a concrete struct.
func decodeParams[T any](raw any) (T, error) {
	var out T
	if raw == nil {
		return out, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

func (d *Daemon) dispatch(ctx context.Context, req wire.Request) wire.Response {
	if req.Method == "buildInfo" {
		return wire.Response{ID: req.ID, OK: true, Result: map[string]string{"sourceID": buildinfo.SourceID}}
	}
	if req.Network != "" && !wire.IsKnownNetwork(req.Network) {
		return wire.Response{ID: req.ID, Error: "unknown network: " + req.Network}
	}
	if req.Method == wire.MethodSetEnabledServices {
		return d.handleSetEnabledServices(req)
	}
	if globalSetting(req.Method) {
		return d.dispatchGMessages(ctx, req)
	}
	network := req.Network
	if network == "" {
		network = wire.NetworkGMessages
	}
	if wire.IsKnownNetwork(network) && req.Method != wire.MethodStatus {
		if err := d.serviceRequestError(network); err != nil {
			return wire.Response{ID: req.ID, Error: err.Error()}
		}
	}
	switch req.Network {
	case wire.NetworkWhatsApp:
		return d.dispatchWhatsApp(ctx, req)
	case wire.NetworkTelegram:
		return d.dispatchTelegram(ctx, req)
	case wire.NetworkMessenger:
		return d.dispatchMessenger(ctx, req)
	case "", wire.NetworkGMessages:
		return d.dispatchGMessages(ctx, req)
	default:
		return wire.Response{ID: req.ID, OK: false, Error: "unknown network: " + req.Network}
	}
}

func (d *Daemon) dispatchMessenger(ctx context.Context, req wire.Request) wire.Response {
	fail := func(err error) wire.Response { return wire.Response{ID: req.ID, Error: err.Error()} }
	ok := func(result any) wire.Response { return wire.Response{ID: req.ID, OK: true, Result: result} }
	if d.fb == nil {
		return fail(errors.New("Messenger backend not initialized"))
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	switch req.Method {
	case wire.MethodStatus:
		return ok(d.fb.Status())
	case wire.MethodConversations:
		p, err := decodeParams[wire.ConversationsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		return ok(d.fb.Conversations(p.Count))
	case wire.MethodMessages:
		p, err := decodeParams[wire.MessagesParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.fb.Messages(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)
	case wire.MethodSend:
		p, err := decodeParams[wire.SendParams](req.Params)
		if err != nil {
			return fail(err)
		}
		msg, err := d.fb.Send(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(msg)
	case wire.MethodSendMedia:
		p, err := decodeParams[wire.SendMediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.fb.SendMedia(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)
	case wire.MethodMedia:
		p, err := decodeParams[wire.MediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.fb.Media(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)
	case wire.MethodPickImage:
		path, err := d.PickFile(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(wire.PickImageResult{Path: path})
	case wire.MethodMarkRead:
		p, err := decodeParams[wire.MarkReadParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.fb.MarkRead(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodRefresh:
		if err := d.fb.Refresh(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodPairFromBrowser:
		if err := d.fb.PairFromBrowser(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodListProfiles:
		return ok(d.ListProfiles())
	case wire.MethodSetProfile:
		p, err := decodeParams[wire.SetProfileParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.SetProfile(p.Name); err != nil {
			return fail(err)
		}
		return ok(d.ListProfiles())
	case wire.MethodUnpair:
		if err := d.fb.Unpair(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodConfig:
		return ok(d.PluginConfig())
	case wire.MethodSetUiScale:
		p, err := decodeParams[wire.SetUiScaleParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.SetUiScale(p.Scale); err != nil {
			return fail(err)
		}
		return ok(d.PluginConfig())
	case wire.MethodReact:
		p, err := decodeParams[wire.ReactParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.fb.React(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodDiscardCapture:
		p, err := decodeParams[wire.DiscardCaptureParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.DiscardCapture(p.Path); err != nil {
			return fail(err)
		}
		return ok(nil)
	case wire.MethodStartPairing, wire.MethodGaiaPairing:
		return fail(errors.New("use browser pairing for Messenger"))
	default:
		return fail(fmt.Errorf("unknown method %q for network messenger", req.Method))
	}
}

func (d *Daemon) dispatchGMessages(ctx context.Context, req wire.Request) wire.Response {
	fail := func(err error) wire.Response {
		return wire.Response{ID: req.ID, OK: false, Error: err.Error()}
	}
	ok := func(result any) wire.Response {
		return wire.Response{ID: req.ID, OK: true, Result: result}
	}

	// A request must not outlive the account it was issued against.
	session := d.sessionContext()
	ctx, sessionCancel := context.WithCancel(ctx)
	stopSession := func() bool { return true }
	if req.Method != wire.MethodUnpair {
		stopSession = context.AfterFunc(session, sessionCancel)
	}
	defer stopSession()
	defer sessionCancel()
	if req.Method != wire.MethodUnpair && session.Err() != nil {
		sessionCancel()
	}

	// Network calls must not hang the UI forever.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if req.Method != wire.MethodUnpair {
		ctx = sessionBoundContext{Context: ctx, session: session}
	}

	switch req.Method {
	case wire.MethodStatus:
		return ok(d.Status())

	case wire.MethodConversations:
		p, err := decodeParams[wire.ConversationsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		return ok(d.Conversations(p.Count))

	case wire.MethodMessages:
		p, err := decodeParams[wire.MessagesParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.Messages(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodSend:
		p, err := decodeParams[wire.SendParams](req.Params)
		if err != nil {
			return fail(err)
		}
		msg, err := d.Send(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(msg)

	case wire.MethodMarkRead:
		p, err := decodeParams[wire.MarkReadParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.MarkRead(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodMedia:
		p, err := decodeParams[wire.MediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.Media(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodRefresh:
		if err := d.Refresh(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodStartPairing:
		qr, err := d.StartPairing()
		if err != nil {
			return fail(err)
		}
		return ok(map[string]string{"url": qr})

	case wire.MethodGaiaPairing:
		p, err := decodeParams[wire.GaiaPairingParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.startGaiaPairing(ctx, p.Cookies); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodPairFromBrowser:
		if err := d.pairFromBrowser(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodSendMedia:
		p, err := decodeParams[wire.SendMediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		msg, err := d.SendMedia(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(msg)

	case wire.MethodPickImage:
		path, err := d.PickImage(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(wire.PickImageResult{Path: path})

	case wire.MethodListProfiles:
		return ok(d.ListProfiles())

	case wire.MethodSetProfile:
		p, err := decodeParams[wire.SetProfileParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.SetProfile(p.Name); err != nil {
			return fail(err)
		}
		return ok(d.ListProfiles())

	case wire.MethodReact:
		p, err := decodeParams[wire.ReactParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.React(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodDiscardCapture:
		p, err := decodeParams[wire.DiscardCaptureParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.DiscardCapture(p.Path); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodSetUiScale:
		p, err := decodeParams[wire.SetUiScaleParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.SetUiScale(p.Scale); err != nil {
			return fail(err)
		}
		return ok(d.PluginConfig())

	case wire.MethodConfig:
		return ok(d.PluginConfig())

	case wire.MethodSetTelegramCredentials:
		p, err := decodeParams[wire.SetTelegramCredentialsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.SetTelegramCredentials(ctx, p.APIID, p.APIHash); err != nil {
			return fail(err)
		}
		return ok(d.PluginConfig())

	case wire.MethodUnpair:
		if err := d.Unpair(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	default:
		return fail(fmt.Errorf("unknown method %q", req.Method))
	}
}

func (d *Daemon) dispatchWhatsApp(ctx context.Context, req wire.Request) wire.Response {
	fail := func(err error) wire.Response {
		return wire.Response{ID: req.ID, OK: false, Error: err.Error()}
	}
	ok := func(result any) wire.Response {
		return wire.Response{ID: req.ID, OK: true, Result: result}
	}
	if d.wa == nil {
		return fail(errors.New("WhatsApp backend not initialized"))
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch req.Method {
	case wire.MethodStatus:
		return ok(d.wa.Status())

	case wire.MethodConversations:
		p, err := decodeParams[wire.ConversationsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		return ok(d.wa.Conversations(p.Count))

	case wire.MethodMessages:
		p, err := decodeParams[wire.MessagesParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.wa.Messages(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodSend:
		p, err := decodeParams[wire.SendParams](req.Params)
		if err != nil {
			return fail(err)
		}
		msg, err := d.wa.Send(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(msg)

	case wire.MethodSendMedia:
		p, err := decodeParams[wire.SendMediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.wa.SendMedia(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodMedia:
		p, err := decodeParams[wire.MediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.wa.Media(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodMarkRead:
		p, err := decodeParams[wire.MarkReadParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.wa.MarkRead(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodStartPairing:
		qr, err := d.wa.StartPairing(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(map[string]string{"url": qr})

	case wire.MethodUnpair:
		if err := d.wa.Unpair(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodRefresh:
		if err := d.wa.Refresh(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodPickImage:
		path, err := d.PickImage(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(wire.PickImageResult{Path: path})

	case wire.MethodDiscardCapture:
		p, err := decodeParams[wire.DiscardCaptureParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.DiscardCapture(p.Path); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodConfig:
		return ok(d.PluginConfig())

	case wire.MethodSetUiScale:
		p, err := decodeParams[wire.SetUiScaleParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.SetUiScale(p.Scale); err != nil {
			return fail(err)
		}
		return ok(d.PluginConfig())

	case wire.MethodGaiaPairing, wire.MethodPairFromBrowser:
		return fail(errors.New("Google account pairing is not supported on WhatsApp"))

	case wire.MethodListProfiles, wire.MethodSetProfile:
		return fail(errors.New("browser profiles are not supported on WhatsApp"))

	case wire.MethodReact:
		p, err := decodeParams[wire.ReactParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err = d.wa.React(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	default:
		return fail(fmt.Errorf("unknown method %q for network whatsapp", req.Method))
	}
}

func (d *Daemon) dispatchTelegram(ctx context.Context, req wire.Request) wire.Response {
	fail := func(err error) wire.Response {
		return wire.Response{ID: req.ID, OK: false, Error: err.Error()}
	}
	ok := func(result any) wire.Response {
		return wire.Response{ID: req.ID, OK: true, Result: result}
	}
	if d.tg == nil {
		return fail(errors.New("Telegram backend not initialized"))
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	switch req.Method {
	case wire.MethodStatus:
		return ok(d.tg.Status())

	case wire.MethodConversations:
		p, err := decodeParams[wire.ConversationsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		return ok(d.tg.Conversations(p.Count))

	case wire.MethodMessages:
		p, err := decodeParams[wire.MessagesParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.tg.Messages(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodSend:
		p, err := decodeParams[wire.SendParams](req.Params)
		if err != nil {
			return fail(err)
		}
		msg, err := d.tg.Send(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(msg)

	case wire.MethodSendMedia:
		p, err := decodeParams[wire.SendMediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.tg.SendMedia(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodMedia:
		p, err := decodeParams[wire.MediaParams](req.Params)
		if err != nil {
			return fail(err)
		}
		res, err := d.tg.Media(ctx, p)
		if err != nil {
			return fail(err)
		}
		return ok(res)

	case wire.MethodMarkRead:
		p, err := decodeParams[wire.MarkReadParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.tg.MarkRead(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodStartPairing:
		qr, err := d.tg.StartPairing(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(map[string]string{"url": qr})

	case wire.MethodUnpair:
		if err := d.tg.Unpair(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodRefresh:
		if err := d.tg.Refresh(ctx); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodPickImage:
		path, err := d.PickImage(ctx)
		if err != nil {
			return fail(err)
		}
		return ok(wire.PickImageResult{Path: path})

	case wire.MethodDiscardCapture:
		p, err := decodeParams[wire.DiscardCaptureParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.DiscardCapture(p.Path); err != nil {
			return fail(err)
		}
		return ok(nil)

	case wire.MethodConfig:
		return ok(d.PluginConfig())

	case wire.MethodSetUiScale:
		p, err := decodeParams[wire.SetUiScaleParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.SetUiScale(p.Scale); err != nil {
			return fail(err)
		}
		return ok(d.PluginConfig())

	case wire.MethodGaiaPairing, wire.MethodPairFromBrowser:
		return fail(errors.New("Google account pairing is not supported on Telegram"))

	case wire.MethodListProfiles, wire.MethodSetProfile:
		return fail(errors.New("browser profiles are not supported on Telegram"))

	case wire.MethodReact:
		p, err := decodeParams[wire.ReactParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.tg.React(ctx, p); err != nil {
			return fail(err)
		}
		return ok(nil)

	default:
		return fail(fmt.Errorf("unknown method %q for network telegram", req.Method))
	}
}
