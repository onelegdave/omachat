// Package daemon runs the Google Messages client and serves its state to the
// Quickshell plugin over a Unix socket.
package daemon

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/telegram"
	"github.com/onelegdave/omachat/internal/whatsapp"
	"github.com/onelegdave/omachat/internal/wire"
)

// convListLimit caps how many conversations we hold and serve. The panel is a
// bar popup, not an archive browser.
const convListLimit = 50

// Daemon owns the libgm client and the cached view of the account.
type Daemon struct {
	log   zerolog.Logger
	paths *store.Paths

	// sessionMu orders account resets, event handling, and persistence.
	sessionMu     sync.Mutex
	sessionCtx    context.Context
	sessionCancel context.CancelFunc
	maintCancel   context.CancelFunc
	gaiaCancel    context.CancelFunc
	gaiaCtx       context.Context
	gaiaActive    bool

	mu      sync.RWMutex
	client  *libgm.Client
	auth    *libgm.AuthData
	status  wire.Status
	convs   map[string]wire.Conversation
	order   []string // conversation IDs, newest first
	selfIDs map[string]bool

	media   *mediaCache
	avatars *avatarStore
	config  *store.ConfigStore

	// Reactions carry the participant IDs that left them, which is the only
	// way to tell whether one of them is the user's own — and that decides
	// whether tapping an emoji adds, removes, or switches.
	reactMu   sync.RWMutex
	reactions map[string][]reactionRecord
	// reactionOrder bounds the map above; see trimReactionsLocked.
	reactionOrder []string

	cookies cookieRefresher
	syncing bool // guarded by sessionMu

	// Maintenance starts either after a stored session connects or after a
	// fresh pairing, whichever happens first, and must run exactly once. It is
	// tied to the daemon's own context so it stops with the daemon rather than
	// leaking past shutdown.
	maintOnce sync.Once
	maintCtx  context.Context

	// pairCancel stops the QR refresh loop when pairing ends, however it ends.
	pairCancel context.CancelFunc

	// paired gates session persistence. Signing in to Google fills in device
	// identity and refreshes the auth token BEFORE the phone has confirmed
	// anything, so persisting on every token refresh writes a session that
	// looks complete but is not. On the next start the daemon connects with
	// it, the server answers "logged out", and the session is discarded —
	// which looks exactly like a pairing that worked and then broke. Only a
	// PairSuccessful event flips this.
	paired bool

	subMu sync.Mutex
	subs  map[chan wire.Event]struct{}

	wa *whatsapp.Backend
	tg *telegram.Backend
}

// New builds a daemon around already-resolved paths.
func New(log zerolog.Logger, paths *store.Paths) *Daemon {
	d := &Daemon{
		log:       log,
		paths:     paths,
		convs:     make(map[string]wire.Conversation),
		selfIDs:   make(map[string]bool),
		subs:      make(map[chan wire.Event]struct{}),
		media:     newMediaCache(paths.MediaDir()),
		avatars:   newAvatarStore(paths.MediaDir()),
		config:    store.NewConfigStore(paths.ConfigFile()),
		reactions: make(map[string][]reactionRecord),
		// PhoneOK starts true: it is only ever falsified by an explicit
		// PhoneNotResponding event. Starting false meant the bar read
		// "Phone not responding" forever, because the event that clears it
		// (PhoneRespondingAgain) only fires after a failure that never
		// happened.
		status: wire.Status{Network: wire.NetworkGMessages, State: wire.StateUnpaired, PhoneOK: true},
	}
	d.wa = whatsapp.New(log, paths, d.PublishEvent)
	d.tg = telegram.New(log, paths, d.PublishEvent, d.config)
	return d
}

// SetWhatsApp overrides the WhatsApp backend instance (useful for unit testing).
func (d *Daemon) SetWhatsApp(wa *whatsapp.Backend) {
	d.wa = wa
}

// WhatsApp returns the WhatsApp backend instance.
func (d *Daemon) WhatsApp() *whatsapp.Backend {
	return d.wa
}

// SetTelegram overrides the Telegram backend instance (useful for unit testing).
func (d *Daemon) SetTelegram(tg *telegram.Backend) {
	d.tg = tg
}

// Telegram returns the Telegram backend instance.
func (d *Daemon) Telegram() *telegram.Backend {
	return d.tg
}

// Start loads any stored session and connects, or parks in the unpaired state
// waiting for the plugin to ask for a QR code.
func (d *Daemon) Start(ctx context.Context) error {
	// Held so maintenance started later (after pairing) still stops with the
	// daemon instead of outliving it.
	d.maintCtx, d.maintCancel = context.WithCancel(ctx)
	d.sessionCtx, d.sessionCancel = context.WithCancel(d.maintCtx)

	// Independent network start: start WhatsApp and Telegram regardless of Google session status
	if d.wa != nil {
		if err := d.wa.Start(ctx); err != nil {
			d.log.Error().Err(err).Msg("WhatsApp backend initialization failed")
			d.wa.SetState(wire.StateDisconnected, "WhatsApp initialization error: "+err.Error())
		}
	}
	if d.tg != nil {
		if err := d.tg.Start(ctx); err != nil {
			d.log.Error().Err(err).Msg("Telegram backend initialization failed")
			d.tg.SetState(wire.StateDisconnected, "Telegram initialization error: "+err.Error())
		}
	}

	auth, paired, err := d.paths.LoadSession()
	if err != nil {
		d.log.Warn().Err(err).Msg("Google session read failed; marking disconnected")
		d.setState(wire.StateDisconnected, "load session: "+err.Error())
		return nil
	}
	d.mu.Lock()
	d.auth = auth
	d.client = libgm.NewClient(auth, nil, d.log.With().Str("component", "libgm").Logger())
	d.bindClient(d.client, d.sessionCtx)
	d.mu.Unlock()

	if !paired {
		d.setState(wire.StateUnpaired, "")
		d.log.Info().Msg("No stored Google session; waiting for pairing")
		return nil
	}
	// Only completed pairings are ever written, so a session on disk means
	// this daemon may persist refreshed tokens from here on.
	d.mu.Lock()
	d.paired = true
	d.mu.Unlock()

	d.setState(wire.StateConnecting, "")
	if err := d.client.Connect(); err != nil {
		// A failed connect is recoverable (phone offline, token expired), so
		// surface it rather than exiting; the plugin shows a retry affordance.
		if isAuthError(err) {
			d.requirePairing(d.sessionContext())
		} else {
			d.setState(wire.StateDisconnected, err.Error())
		}
		d.log.Warn().Err(err).Msg("Initial Google connect failed")
		return nil
	}
	go d.initialSync(d.sessionContext())
	d.startMaintenance()
	return nil
}

// startMaintenance launches the credential upkeep loop at most once.
func (d *Daemon) startMaintenance() {
	d.maintOnce.Do(func() {
		ctx := d.maintCtx
		if ctx == nil {
			ctx = context.Background()
		}
		go d.runMaintenance(ctx)
	})
}

// initialSync brings a reconnected session up to date.
//
// libgm does not emit ClientReady when resuming a stored session — readiness
// only shows up as ListenRecovered — so nothing else pulls the conversation
// list on start. Without this the panel shows "No conversations yet" until the
// user hits refresh, which reads exactly like a broken pairing.
func (d *Daemon) initialSync(parent context.Context) {
	d.sessionMu.Lock()
	if parent.Err() != nil || !d.canSyncLocked() || d.syncing {
		d.sessionMu.Unlock()
		return
	}
	d.syncing = true
	d.sessionMu.Unlock()
	defer d.inSession(parent, func() { d.syncing = false })

	// An open long poll is not proof that the phone has answered.
	if !waitContext(parent, 3*time.Second) {
		return
	}
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(parent, 45*time.Second)
		err := d.Refresh(ctx)
		cancel()
		if err == nil || parent.Err() != nil || isAuthError(err) {
			return
		}
		d.log.Warn().Err(err).Int("attempt", attempt).Msg("Initial conversation fetch failed")
		if attempt < 3 && !waitContext(parent, time.Duration(attempt*5)*time.Second) {
			return
		}
	}
	d.inSession(parent, func() {
		if d.canSyncLocked() {
			d.setState(wire.StateDisconnected, "Could not sync conversations. Keep your phone online, then select Refresh.")
		}
	})
}

// Caller holds sessionMu. Readiness requires a confirmed, usable pairing.
func (d *Daemon) canSyncLocked() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.client != nil && d.paired && !d.gaiaActive &&
		d.status.State != wire.StateUnpaired && d.status.State != wire.StatePairing &&
		d.status.State != wire.StateGaiaPairing && d.status.State != wire.StateError
}

// Stop disconnects cleanly and persists the session.
func (d *Daemon) Stop() {
	d.sessionMu.Lock()
	if d.maintCancel != nil {
		d.maintCancel()
	}
	if d.sessionCancel != nil {
		d.sessionCancel()
	}
	if d.gaiaCancel != nil {
		d.gaiaCancel()
	}
	d.stopPairRefresh()
	d.mu.RLock()
	c := d.client
	d.mu.RUnlock()
	d.saveSessionLocked()
	d.sessionMu.Unlock()
	if c != nil {
		c.Disconnect()
	}
	if d.wa != nil {
		d.wa.Stop()
	}
}

func (d *Daemon) saveSession() {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	d.saveSessionLocked()
}

func (d *Daemon) saveSessionLocked() {
	d.mu.RLock()
	auth, paired := d.auth, d.paired
	d.mu.RUnlock()
	if auth == nil || !paired {
		return
	}
	if err := d.paths.SaveSession(auth); err != nil {
		d.log.Error().Err(err).Msg("Failed to save session")
	}
}

// --- event fan-out ---

// Subscribe returns a channel of daemon events plus a cancel func. The channel
// is buffered and lossy on overflow: a wedged UI must not stall the client.
func (d *Daemon) Subscribe() (<-chan wire.Event, func()) {
	ch := make(chan wire.Event, 64)
	d.subMu.Lock()
	d.subs[ch] = struct{}{}
	d.subMu.Unlock()
	return ch, func() {
		d.subMu.Lock()
		if _, ok := d.subs[ch]; ok {
			delete(d.subs, ch)
			close(ch)
		}
		d.subMu.Unlock()
	}
}

// PublishEvent pushes an event to all connected subscribers.
func (d *Daemon) PublishEvent(evt wire.Event) {
	d.subMu.Lock()
	defer d.subMu.Unlock()
	for ch := range d.subs {
		select {
		case ch <- evt:
		default:
			d.log.Warn().Str("event", evt.Event).Str("network", evt.Network).Msg("Subscriber lagging, dropping event")
		}
	}
}

func (d *Daemon) publish(name string, data any) {
	d.PublishEvent(wire.Event{Event: name, Network: wire.NetworkGMessages, Data: data})
}

func (d *Daemon) setState(state wire.ConnState, errMsg string) {
	d.mu.Lock()
	d.status.State = state
	d.status.Error = errMsg
	// Hint belongs to the error it was set with; a new state invalidates it.
	d.status.Hint = ""
	if state != wire.StatePairing {
		d.status.QRURL = ""
	}
	if state != wire.StateGaiaPairing {
		d.status.Emoji = ""
	}
	d.status.Unread = d.unreadCountLocked()
	st := d.status
	d.mu.Unlock()
	d.publish(wire.EventStatus, st)
}

func (d *Daemon) unreadCountLocked() int {
	n := 0
	for _, c := range d.convs {
		if c.Unread {
			n++
		}
	}
	return n
}

// publishStatus recomputes and pushes status without changing state.
func (d *Daemon) publishStatus() {
	d.mu.Lock()
	d.status.Unread = d.unreadCountLocked()
	st := d.status
	d.mu.Unlock()
	d.publish(wire.EventStatus, st)
}

// --- libgm event handling ---

func (d *Daemon) handleEvent(raw any) {
	switch evt := raw.(type) {
	case *events.ClientReady:
		if !d.canSyncLocked() {
			return
		}
		d.log.Info().Int("conversations", len(evt.Conversations)).Msg("Client ready")
		d.replaceConversations(evt.Conversations)
		d.mu.Lock()
		d.status.LastSyncSec = time.Now().Unix()
		d.mu.Unlock()
		d.setState(wire.StateConnected, "")

	case *events.PairSuccessful:
		if d.gaiaActive && d.gaiaCtx != nil && d.gaiaCtx.Err() != nil {
			return
		}
		if !d.gaiaActive && d.Status().State != wire.StatePairing {
			return
		}
		d.log.Info().Msg("Pairing successful")
		d.stopPairRefresh()
		d.mu.Lock()
		d.paired = true
		d.mu.Unlock()
		d.startMaintenance()
		d.saveSessionLocked()
		d.publish(wire.EventPaired, nil)
		d.setState(wire.StateConnecting, "")
		if !d.gaiaActive {
			go d.initialSync(d.sessionContext())
		}

	case *events.AuthTokenRefreshed:
		d.saveSessionLocked()

	case *gmproto.Conversation:
		d.upsertConversation(evt)

	case *libgm.WrappedMessage:
		if evt.IsOld {
			return
		}
		d.handleMessage(evt.Message)

	case *gmproto.Message:
		d.handleMessage(evt)

	case *events.BrowserActive:
		d.log.Debug().Msg("Browser active elsewhere")

	case *events.PhoneNotResponding:
		d.mu.Lock()
		d.status.PhoneOK = false
		d.mu.Unlock()
		d.publishStatus()

	case *events.PhoneRespondingAgain:
		d.mu.Lock()
		d.status.PhoneOK = true
		d.mu.Unlock()
		d.publishStatus()

	case *events.ListenTemporaryError:
		if d.canSyncLocked() {
			d.setState(wire.StateDisconnected, "connection interrupted")
		}

	case *events.ListenRecovered:
		if !d.canSyncLocked() {
			return
		}
		d.setState(wire.StateConnecting, "")
		go d.initialSync(d.sessionContext())

	case *events.ListenFatalError:
		if !d.canSyncLocked() {
			return
		}
		// A fatal listener has stopped. Never start an interactive pairing
		// from its callback or allow a later event to clear this failure.
		if isAuthError(evt.Error) {
			d.requirePairingLocked()
		} else {
			d.setState(wire.StateError, fmt.Sprintf("%v", evt.Error))
		}

	case *events.GaiaLoggedOut:
		d.log.Warn().Msg("Logged out by server")
		// Reset after leaving the event callback; reset also takes sessionMu.
		parent := d.sessionContext()
		go d.resetLoggedOut(parent)

	default:
		d.log.Trace().Type("type", raw).Msg("Unhandled libgm event")
	}
}

func (d *Daemon) handleMessage(msg *gmproto.Message) {
	// Attachment decryption keys only ever arrive attached to a message, so
	// harvest them on the way past; Media() needs them later, on demand.
	d.media.record(msg)
	d.recordReactions(msg)
	m := convertMessage(msg, d.senderName(msg))
	d.markMyReactions(m.ConversationID, &m)
	d.publish(wire.EventMessage, m)
	// The conversation list preview and unread dot both derive from
	// conversation events, but those can lag behind the message itself.
	d.touchConversation(m)
}

func (d *Daemon) senderName(msg *gmproto.Message) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	conv, ok := d.convs[msg.GetConversationID()]
	if !ok {
		return ""
	}
	for _, p := range conv.Participants {
		if p.ID == msg.GetParticipantID() {
			return p.Name
		}
	}
	return ""
}

// touchConversation moves a conversation to the top of the list when a message
// lands, so the UI reorders immediately instead of waiting for a sync.
func (d *Daemon) touchConversation(m wire.Message) {
	d.mu.Lock()
	conv, ok := d.convs[m.ConversationID]
	if !ok {
		d.mu.Unlock()
		return
	}
	if m.Timestamp >= conv.Timestamp {
		conv.Timestamp = m.Timestamp
		conv.PreviewMine = m.FromMe
		if m.Text != "" {
			conv.Preview = m.Text
		} else if len(m.Attachments) > 0 {
			conv.Preview = "Attachment"
		}
		if !m.FromMe {
			conv.Unread = true
		}
		d.convs[m.ConversationID] = conv
		d.reorderLocked()
	}
	out := d.convs[m.ConversationID]
	d.mu.Unlock()
	d.publish(wire.EventConversation, out)
	d.publishStatus()
}

func (d *Daemon) replaceConversations(convs []*gmproto.Conversation) {
	d.mu.Lock()
	d.convs = make(map[string]wire.Conversation, len(convs))
	for _, c := range convs {
		w := convertConversation(c)
		if p, ok := d.avatars.cached(w.ID); ok {
			w.AvatarPath = p
		}
		d.convs[w.ID] = w
		for _, p := range w.Participants {
			if p.IsMe {
				d.selfIDs[p.ID] = true
			}
		}
	}
	d.reorderLocked()
	list := d.listLocked()
	d.mu.Unlock()

	for _, c := range list {
		d.publish(wire.EventConversation, c)
	}

	go d.fetchAvatars(d.sessionContext(), convs)
}

func (d *Daemon) upsertConversation(c *gmproto.Conversation) {
	w := convertConversation(c)
	d.mu.Lock()
	if p, ok := d.avatars.cached(w.ID); ok {
		w.AvatarPath = p
	}
	d.convs[w.ID] = w
	d.reorderLocked()
	d.mu.Unlock()
	d.publish(wire.EventConversation, w)
	d.publishStatus()
}

// reorderLocked rebuilds the ordering: pinned first, then newest-first.
func (d *Daemon) reorderLocked() {
	ids := make([]string, 0, len(d.convs))
	for id := range d.convs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := d.convs[ids[i]], d.convs[ids[j]]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if a.Timestamp != b.Timestamp {
			return a.Timestamp > b.Timestamp
		}
		return a.ID < b.ID
	})
	if len(ids) > convListLimit {
		ids = ids[:convListLimit]
	}
	d.order = ids
}

func (d *Daemon) listLocked() []wire.Conversation {
	out := make([]wire.Conversation, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.convs[id])
	}
	return out
}
