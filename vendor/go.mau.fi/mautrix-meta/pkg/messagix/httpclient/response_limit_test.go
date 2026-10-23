package httpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/types"
	"go.mau.fi/util/exhttp"
)

// Pin the policy independently of the implementation. Fixture readers are
// finite so removing the production limit cannot exhaust the test machine.
const responseLimit = 32 << 20

type fixtureBody struct {
	remaining int
	read      int
	closed    bool
	readErr   error
}

func (b *fixtureBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		if b.readErr != nil {
			return 0, b.readErr
		}
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	for i := range p[:n] {
		p[i] = 'x'
	}
	b.remaining -= n
	b.read += n
	return n, nil
}
func (b *fixtureBody) Close() error { b.closed = true; return nil }

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResponseLimit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		size   int
		length int64
		status int
	}{
		{"empty", 0, 0, 200},
		{"normal", 128, 128, 200},
		{"exact_limit", responseLimit, responseLimit, 200},
		{"unknown_length", responseLimit + 1, -1, 200},
		{"understated_length", responseLimit + 8192, 1, 200},
		{"oversized_length", responseLimit + 8192, responseLimit + 8192, 200},
		{"oversized_server_error", responseLimit + 1, -1, 500},
		{"oversized_rate_limit", responseLimit + 1, -1, 429},
		{"normal_client_error", 128, 128, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &fixtureBody{remaining: tc.size}
			calls := 0
			log := zerolog.Nop()
			c := &HTTPClient{log: &log, HTTP: &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: body, ContentLength: tc.length, Request: r}, nil
			})}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			resp, data, err := c.MakeRequest(ctx, "https://fixture.invalid/inbox", http.MethodGet, make(http.Header), nil, types.NONE)
			if resp == nil || !body.closed || calls != 1 {
				t.Fatalf("response=%v, closed=%v, requests=%d", resp, body.closed, calls)
			}
			if tc.size > responseLimit {
				if err == nil || !IsPermanentRequestError(err) || len(data) != 0 || body.read > responseLimit+1 {
					t.Fatalf("oversized response: err=%v, permanent=%v, returned=%d, read=%d", err, IsPermanentRequestError(err), len(data), body.read)
				}
			} else {
				if (err != nil) != (tc.status >= 400) || len(data) != tc.size || body.read != tc.size {
					t.Fatalf("normal response: err=%v, returned=%d, read=%d", err, len(data), body.read)
				}
			}
		})
	}
}

func TestResponseReadFailure(t *testing.T) {
	readErr := errors.New("fixture read failed")
	body := &fixtureBody{remaining: 123, readErr: readErr}
	log := zerolog.Nop()
	c := &HTTPClient{log: &log, HTTP: &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header), Request: r}, nil
	})}}
	_, data, err := c.MakeRequest(context.Background(), "https://fixture.invalid/inbox", http.MethodGet, make(http.Header), nil, types.NONE)
	if !errors.Is(err, readErr) || !errors.Is(err, ErrResponseReadFailed) || data != nil || !body.closed {
		t.Fatalf("data=%d, closed=%v, err=%v", len(data), body.closed, err)
	}
}

func TestResponseLimitGzipTransport(t *testing.T) {
	for _, http2 := range []bool{false, true} {
		t.Run("http2="+strconv.FormatBool(http2), func(t *testing.T) {
			for _, size := range []int{128, responseLimit + 1} {
				t.Run(strconv.Itoa(size), func(t *testing.T) {
					var compressed bytes.Buffer
					zw := gzip.NewWriter(&compressed)
					if _, err := io.Copy(zw, &fixtureBody{remaining: size}); err != nil {
						t.Fatal(err)
					}
					if err := zw.Close(); err != nil {
						t.Fatal(err)
					}
					srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Header.Get("Accept-Encoding") != "gzip" {
							t.Errorf("unexpected encoding: %s", r.Header.Get("Accept-Encoding"))
						}
						w.Header().Set("Content-Encoding", "gzip")
						w.Header().Set("Content-Length", strconv.Itoa(compressed.Len()))
						_, _ = w.Write(compressed.Bytes())
					}))
					srv.EnableHTTP2 = http2
					srv.StartTLS()
					defer srv.Close()
					roots := x509.NewCertPool()
					roots.AddCert(srv.Certificate())
					log := zerolog.Nop()
					c := &HTTPClient{log: &log}
					c.SetConfig(exhttp.ClientSettings{TLSConfig: &tls.Config{RootCAs: roots}, DisableHTTP2: !http2})
					defer c.HTTP.CloseIdleConnections()
					defer c.websocketClient.CloseIdleConnections()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					resp, data, err := c.MakeRequest(ctx, srv.URL, http.MethodGet, make(http.Header), nil, types.NONE)
					if resp == nil || !resp.Uncompressed {
						t.Fatalf("not transparently decompressed: %v, %v", resp, err)
					}
					if (resp.ProtoMajor == 2) != http2 {
						t.Fatalf("unexpected protocol: %s", resp.Proto)
					}
					if size > responseLimit {
						if err == nil || !IsPermanentRequestError(err) || data != nil {
							t.Fatalf("gzip expansion accepted: bytes=%d, err=%v", len(data), err)
						}
					} else if err != nil || len(data) != size {
						t.Fatalf("normal gzip: bytes=%d, err=%v", len(data), err)
					}
				})
			}
		})
	}
}

func TestResponseReadCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	log := zerolog.Nop()
	c := &HTTPClient{log: &log}
	c.SetConfig(exhttp.ClientSettings{})
	defer c.HTTP.CloseIdleConnections()
	defer c.websocketClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, data, err := c.MakeRequest(ctx, srv.URL, http.MethodGet, make(http.Header), nil, types.NONE)
	if !errors.Is(err, context.DeadlineExceeded) || data != nil {
		t.Fatalf("data=%d, err=%v", len(data), err)
	}
}
