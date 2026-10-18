package signal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
)

// Caller is the small signal-cli seam used by Backend. Tests provide an
// in-memory implementation; production uses RPCClient below.
type Caller interface {
	Start(context.Context, string, func(json.RawMessage), func(error)) error
	Call(context.Context, string, any, any) error
	Close() error
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type rpcFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcReply struct {
	result json.RawMessage
	err    error
}

type pendingCall struct {
	method string
	reply  chan rpcReply
}

// maxRPCLineBytes bounds one line from signal-cli. getAttachment returns the
// attachment base64-encoded on one line, so the limit sits just above the
// encoding of the largest attachment OmaChat accepts.
const maxRPCLineBytes = maxIncomingMediaBytes/3*4 + 1<<20

// signalCLIArgs starts signal-cli in JSON-RPC mode. Receiving starts only
// after Backend has selected its account; otherwise startup notifications can
// arrive before listAccounts has completed. Stories are not shown, so they are
// not fetched or saved either.
func signalCLIArgs(dataDir string) []string {
	return []string{"--data-dir", dataDir, "--output=json", "jsonRpc", "--receive-mode=manual", "--ignore-stories"}
}

// RPCClient owns one signal-cli jsonRpc child. The child is deliberately tied
// to OmaChat's service lifetime rather than installed as a system service.
type RPCClient struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	pending map[string]pendingCall
	nextID  atomic.Uint64
	done    chan struct{}
	waitErr error
	onExit  func(error)
}

func NewRPCClient() *RPCClient {
	return &RPCClient{pending: make(map[string]pendingCall)}
}

func (c *RPCClient) Start(parent context.Context, dataDir string, notify func(json.RawMessage), onExit func(error)) error {
	if _, err := exec.LookPath("signal-cli"); err != nil {
		return errors.New("signal-cli is not installed; see the Signal setup guide, then retry")
	}
	c.mu.Lock()
	if c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, "signal-cli", signalCLIArgs(dataDir)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		stdin.Close()
		c.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		c.mu.Unlock()
		return fmt.Errorf("start signal-cli: %w", err)
	}
	c.cmd, c.stdin, c.cancel, c.done, c.onExit = cmd, stdin, cancel, make(chan struct{}), onExit
	c.mu.Unlock()
	go c.readLoop(stdout, notify)
	go c.waitLoop()
	return nil
}

// readLine returns the next line without its newline. A line longer than max
// is consumed and reported as tooLong, so one oversized reply cannot stop
// the stream.
func readLine(r *bufio.Reader, max int) (line []byte, tooLong bool, err error) {
	for {
		chunk, err := r.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > max+1 {
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case err == nil:
			if tooLong {
				return nil, true, nil
			}
			return line[:len(line)-1], false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, false, err
		}
	}
}

func (c *RPCClient) readLoop(stdout io.Reader, notify func(json.RawMessage)) {
	reader := bufio.NewReaderSize(stdout, 64<<10)
	var readErr error
	for {
		line, tooLong, err := readLine(reader, int(maxRPCLineBytes))
		if err != nil {
			readErr = err
			break
		}
		if tooLong {
			// Only getAttachment can return a line this long, and its id is
			// inside the discarded line. Fail every download waiting for one.
			c.failPending("getAttachment", errors.New("Signal attachment is too large to download"))
			continue
		}
		var frame rpcFrame
		if json.Unmarshal(line, &frame) != nil {
			continue
		}
		if len(frame.ID) == 0 || string(frame.ID) == "null" {
			if frame.Method == "receive" && notify != nil {
				notify(frame.Params)
			}
			continue
		}
		id := string(frame.ID)
		if len(id) >= 2 && id[0] == '"' {
			if unquoted, err := strconv.Unquote(id); err == nil {
				id = unquoted
			}
		}
		c.mu.Lock()
		call, ok := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if !ok {
			continue
		}
		ch := call.reply
		if frame.Error != nil {
			// Upstream messages may contain private account or message data.
			ch <- rpcReply{err: fmt.Errorf("signal-cli RPC failed (code %d); check Signal setup and retry", frame.Error.Code)}
		} else {
			ch <- rpcReply{result: frame.Result}
		}
	}
	if errors.Is(readErr, io.EOF) {
		return
	}
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (c *RPCClient) failPending(method string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, call := range c.pending {
		if call.method == method {
			delete(c.pending, id)
			call.reply <- rpcReply{err: err}
		}
	}
}

func (c *RPCClient) waitLoop() {
	c.mu.Lock()
	cmd, done := c.cmd, c.done
	c.mu.Unlock()
	err := cmd.Wait()
	if err == nil {
		err = errors.New("signal-cli exited")
	}
	c.mu.Lock()
	c.waitErr = err
	for id, call := range c.pending {
		delete(c.pending, id)
		call.reply <- rpcReply{err: fmt.Errorf("signal-cli stopped: %w", err)}
	}
	close(done)
	onExit := c.onExit
	c.mu.Unlock()
	if onExit != nil {
		onExit(err)
	}
}

func (c *RPCClient) Call(ctx context.Context, method string, params any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := strconv.FormatUint(c.nextID.Add(1), 10)
	frame := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		frame["params"] = params
	}
	line, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	ch := make(chan rpcReply, 1)
	c.mu.Lock()
	if c.cmd == nil || c.stdin == nil {
		c.mu.Unlock()
		return errors.New("signal-cli is not running")
	}
	c.pending[id] = pendingCall{method: method, reply: ch}
	stdin := c.stdin
	c.mu.Unlock()

	c.writeMu.Lock()
	_, err = stdin.Write(append(line, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	case reply := <-ch:
		if reply.err != nil {
			return reply.err
		}
		if out == nil || len(reply.result) == 0 || string(reply.result) == "null" {
			return nil
		}
		return json.Unmarshal(reply.result, out)
	}
}

func (c *RPCClient) Close() error {
	c.mu.Lock()
	if c.cmd == nil {
		c.mu.Unlock()
		return nil
	}
	cancel, stdin, done := c.cancel, c.stdin, c.done
	c.mu.Unlock()
	stdin.Close()
	cancel()
	<-done
	c.mu.Lock()
	err := c.waitErr
	c.cmd, c.stdin, c.cancel, c.done, c.onExit = nil, nil, nil, nil, nil
	c.mu.Unlock()
	return err
}
