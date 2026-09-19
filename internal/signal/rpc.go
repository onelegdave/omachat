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
	Start(context.Context, string, func(json.RawMessage)) error
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

// RPCClient owns one signal-cli jsonRpc child. The child is deliberately tied
// to OmaChat's service lifetime rather than installed as a system service.
type RPCClient struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	pending map[string]chan rpcReply
	nextID  atomic.Uint64
	done    chan struct{}
	waitErr error
}

func NewRPCClient() *RPCClient {
	return &RPCClient{pending: make(map[string]chan rpcReply)}
}

func (c *RPCClient) Start(parent context.Context, dataDir string, notify func(json.RawMessage)) error {
	if _, err := exec.LookPath("signal-cli"); err != nil {
		return errors.New("signal-cli is not installed; see the Signal setup guide, then retry")
	}
	c.mu.Lock()
	if c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, "signal-cli", "--data-dir", dataDir, "--output=json", "jsonRpc")
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
	c.cmd, c.stdin, c.cancel, c.done = cmd, stdin, cancel, make(chan struct{})
	c.mu.Unlock()
	go c.readLoop(stdout, notify)
	go c.waitLoop()
	return nil
}

func (c *RPCClient) readLoop(stdout io.Reader, notify func(json.RawMessage)) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 8192), 16<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
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
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		if frame.Error != nil {
			ch <- rpcReply{err: fmt.Errorf("signal-cli RPC %d: %s", frame.Error.Code, frame.Error.Message)}
		} else {
			ch <- rpcReply{result: frame.Result}
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
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- rpcReply{err: fmt.Errorf("signal-cli stopped: %w", err)}
	}
	close(done)
	c.mu.Unlock()
}

func (c *RPCClient) Call(ctx context.Context, method string, params any, out any) error {
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
	c.pending[id] = ch
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
	c.cmd, c.stdin, c.cancel, c.done = nil, nil, nil, nil
	c.mu.Unlock()
	return err
}
