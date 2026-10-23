package messagix

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
)

type responseLimitTransport func(*http.Request) (*http.Response, error)

func (f responseLimitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type e2eeFixtureBody struct {
	io.Reader
	closed bool
}

func (b *e2eeFixtureBody) Close() error { b.closed = true; return nil }

type failingE2EEReader struct{ err error }

func (r failingE2EEReader) Read([]byte) (int, error) { return 0, r.err }

func TestE2EEResponseLimit(t *testing.T) {
	readErr := errors.New("fixture read failed")
	for _, name := range []string{"normal", "oversized", "read_error"} {
		payload := `{"status":200}`
		if name == "oversized" {
			payload = strings.Repeat(" ", 32<<20) + payload
		}
		t.Run(name, func(t *testing.T) {
			body := &e2eeFixtureBody{Reader: strings.NewReader(payload)}
			if name == "read_error" {
				body.Reader = io.MultiReader(body.Reader, failingE2EEReader{readErr})
			}
			calls := 0
			c := &Client{endpoints: map[string]string{"fixture": "https://fixture.invalid/register", "base_url": "https://fixture.invalid", "messages": "https://fixture.invalid/messages"}, http: &httpclient.HTTPClient{HTTP: &http.Client{Transport: responseLimitTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header), Request: r}, nil
			})}}}
			var result ICDCRegisterResponse
			var trace bytes.Buffer
			log := zerolog.New(&trace).Level(zerolog.TraceLevel)
			err := c.doE2EERequest(log.WithContext(context.Background()), "fixture", url.Values{}, &result)
			if !body.closed || calls != 1 {
				t.Fatalf("closed=%v, requests=%d", body.closed, calls)
			}
			if name != "normal" && strings.Contains(trace.String(), "response_body") {
				t.Fatal("failed response was included in trace log")
			}
			if name == "oversized" {
				if err == nil || !httpclient.IsPermanentRequestError(err) || result.Status != 0 {
					t.Fatalf("oversized registration accepted: result=%+v err=%v", result, err)
				}
			} else if name == "read_error" {
				if !errors.Is(err, readErr) || result.Status != 0 {
					t.Fatalf("partial registration parsed or error lost: result=%+v err=%v", result, err)
				}
			} else if err != nil || result.Status != 200 || !strings.Contains(trace.String(), "response_body") {
				t.Fatalf("normal registration: result=%+v err=%v", result, err)
			}
		})
	}
}
