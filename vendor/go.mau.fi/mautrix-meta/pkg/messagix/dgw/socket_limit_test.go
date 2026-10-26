package dgw

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// Pin the policy independently. Every fixture is finite, including when the
// production limit is removed for the negative regression check.
const incomingMessageLimit = 32 << 20

type socketLimitTransport struct {
	compression atomic.Bool
}

func (tr *socketLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if resp != nil {
		tr.compression.Store(strings.Contains(resp.Header.Get("Sec-WebSocket-Extensions"), "permessage-deflate"))
	}
	return resp, err
}

// Three individually valid DGW frames make one message of exactly size bytes.
// Each frame fits DGW's 24-bit length field even at the WebSocket ceiling.
func socketLimitMessage(size int) []byte {
	message := make([]byte, 0, size)
	remaining := size - 3*8
	for i := 0; i < 3; i++ {
		n := remaining / (3 - i)
		payload := make([]byte, n)
		for j := range payload {
			payload[j] = 'x'
		}
		message = (&DataFrame{StreamID: 1, Payload: payload}).MarshalAppend(message)
		remaining -= n
	}
	return message
}

func TestSocketIncomingMessageLimit(t *testing.T) {
	for _, compression := range []struct {
		name string
		mode websocket.CompressionMode
	}{
		{"uncompressed", websocket.CompressionDisabled},
		{"compressed_no_context", websocket.CompressionNoContextTakeover},
		{"compressed_context", websocket.CompressionContextTakeover},
	} {
		for _, tc := range []struct {
			name       string
			size       int
			fragmented bool
			overflow   bool
			unfinished bool
		}{
			{"normal", 1024, false, false, false},
			{"exact_limit", incomingMessageLimit, false, false, false},
			{"over_limit", incomingMessageLimit + 1, false, true, false},
			{"fragmented_exact_limit", incomingMessageLimit, true, false, false},
			{"fragmented_over_limit", incomingMessageLimit + 1, true, true, false},
			{"unfinished_over_limit", incomingMessageLimit + (64 << 10), true, true, true},
		} {
			t.Run(compression.name+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				ready := make(chan struct{})
				delivered := make(chan int, 6)
				serverDone := make(chan error, 1)
				transport := &socketLimitTransport{}
				var stream *PersistentStream
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
						CompressionMode: compression.mode, CompressionThreshold: 1,
					})
					if err != nil {
						serverDone <- err
						return
					}
					defer conn.CloseNow()
					select {
					case <-ready:
					case <-ctx.Done():
						serverDone <- ctx.Err()
						return
					}
					message := socketLimitMessage(tc.size)
					if tc.unfinished {
						// Force the compressor to emit its pending block without
						// closing the writer or sending the final continuation.
						if _, err := rand.Read(message[len(message)-(64<<10):]); err != nil {
							serverDone <- err
							return
						}
					}
					if tc.fragmented {
						var writer io.WriteCloser
						writer, err = conn.Writer(ctx, websocket.MessageBinary)
						if err == nil {
							for offset := 0; offset < len(message) && err == nil; offset += 4096 {
								_, err = writer.Write(message[offset:min(offset+4096, len(message))])
							}
							if err == nil && !tc.unfinished {
								err = writer.Close()
							}
						}
					} else {
						err = conn.Write(ctx, websocket.MessageBinary, message)
					}
					if !tc.overflow && err == nil {
						// Wait for all frames before closing, then send a second small
						// message to prove the allowance resets between messages.
						for index, size := range []int{tc.size, 1024} {
							total := 0
							for i := 0; i < 3; i++ {
								select {
								case n := <-delivered:
									total += n
								case <-ctx.Done():
									serverDone <- ctx.Err()
									return
								}
							}
							if total != size-24 {
								serverDone <- fmt.Errorf("received %d payload bytes, want %d", total, size-24)
								return
							}
							if index == 0 {
								if err = conn.Write(ctx, websocket.MessageBinary, socketLimitMessage(1024)); err != nil {
									serverDone <- err
									return
								}
							}
						}
						serverDone <- conn.Close(websocket.StatusNormalClosure, "fixture complete")
						return
					}
					// Reading the close frame verifies the peer receives 1009.
					_, _, readErr := conn.Read(ctx)
					serverDone <- readErr
				}))
				defer server.Close()
				socket := NewSocket(SocketOptions{
					WSURL:      "ws" + strings.TrimPrefix(server.URL, "http"),
					GetCookies: func() string { return "" },
					Log:        zerolog.Nop(),
					DialOpts: websocket.DialOptions{
						CompressionMode: compression.mode,
						HTTPClient:      &http.Client{Transport: transport},
					},
				})
				socket.OnConnect = func(context.Context) error {
					stream = newStream(socket.conn.Load(), 1, func(_ context.Context, payload []byte) error {
						delivered <- len(payload)
						return nil
					}, zerolog.Nop())
					socket.streams.Set(1, stream)
					close(ready)
					return nil
				}
				err := socket.Connect(ctx)
				if ctx.Err() != nil {
					t.Fatalf("socket did not complete before deadline: %v", err)
				}
				if tc.overflow {
					if !errors.Is(err, websocket.ErrMessageTooBig) {
						t.Errorf("Connect error = %v, want ErrMessageTooBig", err)
					}
					if len(delivered) != 0 {
						t.Errorf("oversized message delivered %d partial DGW frames", len(delivered))
					}
				} else if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					t.Errorf("Connect error = %v, want normal peer close", err)
				}
				if stream == nil {
					t.Fatalf("connection never initialized: %v", err)
				}
				if socket.IsConnected() || !socket.stopped.IsSet() || !stream.closed.IsSet() || socket.fatalError.Load() != nil {
					t.Error("socket/stream state was not cleaned up after disconnect")
				}
				if got := transport.compression.Load(); got != (compression.mode != websocket.CompressionDisabled) {
					t.Errorf("compression negotiated = %v", got)
				}
				select {
				case serverErr := <-serverDone:
					if tc.overflow && websocket.CloseStatus(serverErr) != websocket.StatusMessageTooBig {
						t.Errorf("peer close = %v, want 1009", serverErr)
					} else if !tc.overflow && serverErr != nil {
						t.Errorf("server: %v", serverErr)
					}
				case <-ctx.Done():
					t.Fatal("server did not observe disconnect")
				}
			})
		}
	}
}
