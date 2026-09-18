package daemon

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

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func newGifDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	return &Daemon{
		log:    zerolog.Nop(),
		paths:  &store.Paths{Data: dir, Cache: dir, Runtime: dir},
		config: store.NewConfigStore(dir + "/config.json"),
	}
}

func TestGifSearchWithoutKeyAsksForOne(t *testing.T) {
	d := newGifDaemon(t)
	// Must not attempt a request or error out: the picker needs to explain
	// how to get a key, not show a failure.
	res, err := d.GifSearch(context.Background(), wire.GifSearchParams{Query: "cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.NeedsKey {
		t.Error("expected NeedsKey when no API key is configured")
	}
	if res.Attribution == "" {
		t.Error("GIPHY requires attribution to be displayed")
	}
}

func TestGifFetchRefusesUntrustedSources(t *testing.T) {
	d := newGifDaemon(t)
	ctx := context.Background()

	for _, bad := range []string{
		"",
		"http://media.giphy.com/x.gif",         // not https
		"https://evil.example/x.gif",           // wrong host
		"https://giphy.com.evil.example/x.gif", // suffix trickery on the label
		"file:///etc/passwd",
	} {
		if _, err := d.GifFetch(ctx, wire.GifFetchParams{URL: bad}); err == nil {
			t.Errorf("expected %q to be refused", bad)
		}
	}
}

func TestSendRenditionPrefersSmallest(t *testing.T) {
	var r giphyResponse
	r.Data = make([]struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Images struct {
			FixedHeight      giphyRendition `json:"fixed_height"`
			FixedHeightSmall giphyRendition `json:"fixed_height_small"`
			Downsized        giphyRendition `json:"downsized"`
			DownsizedMedium  giphyRendition `json:"downsized_medium"`
			Original         giphyRendition `json:"original"`
		} `json:"images"`
	}, 1)

	r.Data[0].Images.Original = giphyRendition{URL: "https://media.giphy.com/original.gif"}
	r.Data[0].Images.DownsizedMedium = giphyRendition{URL: "https://media.giphy.com/medium.gif"}
	r.Data[0].Images.Downsized = giphyRendition{URL: "https://media.giphy.com/small.gif"}

	// Smallest first: carriers reject large files, so never reach for the
	// original when a downsized one exists.
	if got := r.sendURL(0); got != "https://media.giphy.com/small.gif" {
		t.Errorf("sendURL = %q, want the downsized rendition", got)
	}

	// With only the original present it must still return something.
	r.Data[0].Images.Downsized = giphyRendition{}
	r.Data[0].Images.DownsizedMedium = giphyRendition{}
	if got := r.sendURL(0); got != "https://media.giphy.com/original.gif" {
		t.Errorf("sendURL = %q, want the original as a fallback", got)
	}
}

func TestGifSanitization(t *testing.T) {
	d := newGifDaemon(t)
	if err := d.config.SetGiphyAPIKey("SECRET_KEY_123"); err != nil {
		t.Fatal(err)
	}

	// Mock avatarHTTP transport to fail
	originalTransport := avatarHTTP.Transport
	avatarHTTP.Transport = &mockTransport{
		err: errors.New("simulated transport error"),
	}
	defer func() { avatarHTTP.Transport = originalTransport }()

	_, err := d.GifSearch(context.Background(), wire.GifSearchParams{Query: "cat"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), "SECRET_KEY_123") {
		t.Errorf("error contains secret key: %v", err)
	}
}

type mockTransport struct {
	err error
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The standard library url.Error includes the URL which is what we want to test sanitization against
	return nil, &url.Error{Op: "Get", URL: req.URL.String(), Err: m.err}
}

// recordingTransport serves a fixed JSON body and keeps the request it saw.
type recordingTransport struct {
	req *http.Request
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.req = req
	body := `{"data":[{"id":"abc","title":"cat","images":{` +
		`"fixed_height":{"url":"https://media.giphy.com/p.gif","width":"200","height":"200"},` +
		`"downsized":{"url":"https://media.giphy.com/s.gif"}}}],"meta":{"status":200}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// TestGifSearchKeyStaysInsideTheProcess pins how the key travels: only as
// GIPHY's api_key query parameter over HTTPS, never in a header, and never
// in what the daemon logs or returns, on the success path and on failure.
func TestGifSearchKeyStaysInsideTheProcess(t *testing.T) {
	const key = "SECRET_KEY_456"
	d := newGifDaemon(t)
	var logs bytes.Buffer
	d.log = zerolog.New(&logs).Level(zerolog.DebugLevel)
	if err := d.config.SetGiphyAPIKey(key); err != nil {
		t.Fatal(err)
	}

	originalTransport := avatarHTTP.Transport
	defer func() { avatarHTTP.Transport = originalTransport }()

	rec := &recordingTransport{}
	avatarHTTP.Transport = rec
	res, err := d.GifSearch(context.Background(), wire.GifSearchParams{Query: "cat"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Gifs) != 1 {
		t.Fatalf("got %d gifs, want 1", len(res.Gifs))
	}
	if rec.req == nil {
		t.Fatal("no request was sent")
	}
	if rec.req.URL.Scheme != "https" || rec.req.URL.Host != "api.giphy.com" {
		t.Errorf("request went to %s://%s, want https://api.giphy.com", rec.req.URL.Scheme, rec.req.URL.Host)
	}
	if got := rec.req.URL.Query().Get("api_key"); got != key {
		t.Errorf("api_key query parameter = %q, want the configured key", got)
	}
	for name, values := range rec.req.Header {
		if strings.Contains(strings.Join(values, " "), key) {
			t.Errorf("key leaked into %s header", name)
		}
	}
	for i, g := range res.Gifs {
		if strings.Contains(g.PreviewURL+g.SendURL+g.Title, key) {
			t.Errorf("gif %d carries the key", i)
		}
	}

	avatarHTTP.Transport = &mockTransport{err: errors.New("simulated transport error")}
	if _, err := d.GifSearch(context.Background(), wire.GifSearchParams{Query: "cat"}); err == nil {
		t.Fatal("expected a transport error")
	} else if strings.Contains(err.Error(), key) {
		t.Errorf("error contains the key: %v", err)
	}

	if strings.Contains(logs.String(), key) {
		t.Errorf("daemon log contains the key:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "GIF search") {
		t.Errorf("expected the search to be logged at debug level, got:\n%s", logs.String())
	}
}
