package boundedfile

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func fixtureClient(t *testing.T, body func(context.Context) io.ReadCloser, declared int64) *whatsmeow.Client {
	t.Helper()
	cli := whatsmeow.NewClient(&waStore.Device{}, nil)
	cli.SetMediaHTTPClient(&http.Client{Transport: fixtureTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "media.fixture.invalid" {
			t.Fatalf("test attempted a non-fixture request: %s", req.URL.Host)
		}
		return &http.Response{StatusCode: http.StatusOK, ContentLength: declared,
			Header: make(http.Header), Body: body(req.Context()), Request: req}, nil
	})})
	return cli
}

type endlessFixture struct{ ctx context.Context }

func (e endlessFixture) Read(p []byte) (int, error) {
	// Even a regressed implementation must not fill the test machine's disk.
	select {
	case <-e.ctx.Done():
		return 0, e.ctx.Err()
	case <-time.After(time.Millisecond):
	}
	if len(p) > 256 {
		p = p[:256]
	}
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestVendoredDownloaderRejectsUnderstatedAndEndlessBodies(t *testing.T) {
	for _, declared := range []int64{-1, 1} {
		for _, endless := range []bool{false, true} {
			file, raw := testFile(t, 1024)
			cli := fixtureClient(t, func(ctx context.Context) io.ReadCloser {
				if endless {
					return io.NopCloser(endlessFixture{ctx})
				}
				return io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1025)))
			}, declared)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := cli.DangerousInternals().DownloadAndDecryptToFile(ctx, "https://media.fixture.invalid/attachment", bytes.Repeat([]byte{1}, 32), whatsmeow.MediaImage, make([]byte, 32), make([]byte, 32), file)
			cancel()
			if !errors.Is(err, ErrLimit) {
				t.Fatalf("limit must stop before hash/decrypt checks (declared=%d endless=%v): %v", declared, endless, err)
			}
			checkExtent(t, raw, 1024)
		}
	}
}

func encryptedFixture(t *testing.T, plain []byte) (body, key, encryptedHash, plainHash []byte) {
	t.Helper()
	key = bytes.Repeat([]byte{7}, 32)
	expanded := hkdfutil.SHA256(key, nil, []byte(whatsmeow.MediaImage), 112)
	body, err := cbcutil.Encrypt(expanded[16:48], expanded[:16], plain)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, expanded[48:80])
	mac.Write(expanded[:16])
	mac.Write(body)
	body = append(body, mac.Sum(nil)[:10]...)
	eh, ph := sha256.Sum256(body), sha256.Sum256(plain)
	return body, key, eh[:], ph[:]
}

func TestVendoredEncryptedDownloadStillDecryptsAtExactLimit(t *testing.T) {
	plain := bytes.Repeat([]byte("synthetic photo "), 2500)
	body, key, encryptedHash, plainHash := encryptedFixture(t, plain)
	file, raw := testFile(t, int64(len(body)))
	cli := fixtureClient(t, func(context.Context) io.ReadCloser { return io.NopCloser(bytes.NewReader(body)) }, int64(len(body)))
	if err := cli.DangerousInternals().DownloadAndDecryptToFile(context.Background(), "https://media.fixture.invalid/attachment", key, whatsmeow.MediaImage, encryptedHash, plainHash, file); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("normal encrypted media changed: size=%d err=%v", len(got), err)
	}
	checkExtent(t, raw, int64(len(body)))
}

func TestVendoredDownloadKeepsHashVerification(t *testing.T) {
	body, key, encryptedHash, plainHash := encryptedFixture(t, []byte("fixture"))
	for _, corruptEncryptedHash := range []bool{true, false} {
		file, raw := testFile(t, int64(len(body)))
		eh, ph := bytes.Clone(encryptedHash), bytes.Clone(plainHash)
		want := whatsmeow.ErrInvalidMediaSHA256
		if corruptEncryptedHash {
			eh[0] ^= 1
			want = whatsmeow.ErrInvalidMediaEncSHA256
		} else {
			ph[0] ^= 1
		}
		cli := fixtureClient(t, func(context.Context) io.ReadCloser { return io.NopCloser(bytes.NewReader(body)) }, -1)
		err := cli.DangerousInternals().DownloadAndDecryptToFile(context.Background(), "https://media.fixture.invalid/attachment", key, whatsmeow.MediaImage, eh, ph, file)
		if !errors.Is(err, want) {
			t.Fatalf("hash verification changed: %v", err)
		}
		checkExtent(t, raw, int64(len(body)))
	}
}

type cancelledBody struct{ ctx context.Context }

func (c cancelledBody) Read([]byte) (int, error) { <-c.ctx.Done(); return 0, c.ctx.Err() }
func (cancelledBody) Close() error               { return nil }

func TestVendoredDownloadHonorsCancellation(t *testing.T) {
	file, raw := testFile(t, 1024)
	cli := fixtureClient(t, func(ctx context.Context) io.ReadCloser { return cancelledBody{ctx} }, -1)
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	err := cli.DangerousInternals().DownloadAndDecryptToFile(ctx, "https://media.fixture.invalid/attachment", nil, whatsmeow.MediaImage, nil, nil, file)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation changed: %v", err)
	}
	checkExtent(t, raw, 1024)
}

type retryBody struct{ delivered bool }

func (r *retryBody) Read(p []byte) (int, error) {
	if !r.delivered {
		r.delivered = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("stream error: synthetic retry")
}
func (*retryBody) Close() error { return nil }

func TestVendoredDownloadRetryCanOverwritePartialFile(t *testing.T) {
	file, raw := testFile(t, 8)
	calls := 0
	cli := fixtureClient(t, func(context.Context) io.ReadCloser {
		calls++
		if calls == 1 {
			return &retryBody{}
		}
		return io.NopCloser(bytes.NewReader([]byte("complete")))
	}, -1)
	if err := cli.DangerousInternals().DownloadAndDecryptToFile(context.Background(), "https://media.fixture.invalid/attachment", nil, whatsmeow.MediaImage, nil, nil, file); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("retry count changed: %d", calls)
	}
	checkExtent(t, raw, 8)
}
