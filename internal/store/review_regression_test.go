package store

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

func syntheticTestAuth() *libgm.AuthData {
	auth := libgm.NewAuthData()
	auth.TachyonAuthToken = []byte("synthetic-tachyon-token-review-test")
	auth.Browser = &gmproto.Device{
		SourceID: "browser-dev-1",
	}
	auth.Mobile = &gmproto.Device{
		SourceID: "mobile-dev-1",
	}
	auth.SetCookies(map[string]string{
		"SID":              "synthetic-sid",
		"HSID":             "synthetic-hsid",
		"SSID":             "synthetic-ssid",
		"APISID":           "synthetic-apisid",
		"SAPISID":          "synthetic-sapisid",
		"__Secure-1PSID":   "synthetic-1psid",
		"__Secure-1PSIDTS": "synthetic-1psidts-initial",
		"__Secure-3PSID":   "synthetic-3psid",
		"__Secure-3PSIDTS": "synthetic-3psidts-initial",
		"OSID":             "synthetic-osid",
	})
	return auth
}

// TestConcurrentCookiePersistenceAndRace verifies that concurrent cookie rotations
// (simulating HTTP responses) and SaveSession calls do not trigger map races or file corruption.
func TestConcurrentCookiePersistenceAndRace(t *testing.T) {
	dataDir := t.TempDir()
	p := &Paths{Data: dataDir, Cache: t.TempDir(), Runtime: t.TempDir()}

	auth := syntheticTestAuth()
	if err := p.SaveSession(auth); err != nil {
		t.Fatalf("initial save failed: %v", err)
	}

	stop := make(chan struct{})
	var (
		wgWriters sync.WaitGroup
		wgSavers  sync.WaitGroup
	)

	// Writer goroutines: continuously rotate cookies via SetCookies
	for w := 0; w < 3; w++ {
		wgWriters.Add(1)
		go func(workerID int) {
			defer wgWriters.Done()
			round := 0
			for {
				select {
				case <-stop:
					return
				default:
					round++
					fresh := map[string]string{
						"SID":              "synthetic-sid",
						"HSID":             "synthetic-hsid",
						"SSID":             "synthetic-ssid",
						"APISID":           "synthetic-apisid",
						"SAPISID":          "synthetic-sapisid",
						"__Secure-1PSID":   "synthetic-1psid",
						"__Secure-1PSIDTS": fmt.Sprintf("ts-%d-%d", workerID, round),
						"__Secure-3PSID":   "synthetic-3psid",
						"__Secure-3PSIDTS": fmt.Sprintf("3ts-%d-%d", workerID, round),
						"OSID":             "synthetic-osid",
					}
					auth.SetCookies(fresh)
					auth.UpdateCookiesFromResponse(&http.Response{Header: http.Header{"Set-Cookie": []string{"SID=rotated; Path=/"}}})
				}
			}
		}(w)
	}

	// Saver goroutines: repeatedly persist session to disk
	for s := 0; s < 3; s++ {
		wgSavers.Add(1)
		go func() {
			defer wgSavers.Done()
			for i := 0; i < 25; i++ {
				if err := p.SaveSession(auth); err != nil {
					t.Errorf("SaveSession failed: %v", err)
					return
				}
			}
		}()
	}

	// Loader goroutines: concurrently read session from disk
	for l := 0; l < 2; l++ {
		wgSavers.Add(1)
		go func() {
			defer wgSavers.Done()
			for i := 0; i < 25; i++ {
				loaded, paired, err := p.LoadSession()
				if err != nil {
					t.Errorf("LoadSession failed: %v", err)
					return
				}
				if !paired || loaded == nil {
					t.Errorf("expected loaded session to be paired")
					return
				}
			}
		}()
	}

	// Wait for savers and loaders to finish
	wgSavers.Wait()
	close(stop)
	wgWriters.Wait()

	// Final verification of persisted session
	finalAuth, paired, err := p.LoadSession()
	if err != nil {
		t.Fatalf("final LoadSession failed: %v", err)
	}
	if !paired {
		t.Error("expected final session to be paired")
	}
	if len(finalAuth.Cookies) == 0 {
		t.Error("expected non-empty cookies in persisted session")
	}
}

// TestClearSessionRecreatesMediaDir ensures that ClearSession wipes cached attachments
// and leaves the media directory existing and writable for subsequent pairings in the same process.
func TestClearSessionRecreatesMediaDir(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	p := &Paths{Data: dataDir, Cache: cacheDir, Runtime: t.TempDir()}

	// Ensure media directory starts created
	if err := os.MkdirAll(p.MediaDir(), 0o700); err != nil {
		t.Fatalf("setup MediaDir: %v", err)
	}

	// Create dummy session and media file
	auth := syntheticTestAuth()
	if err := p.SaveSession(auth); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	testMediaFile := filepath.Join(p.MediaDir(), "attachment-1.png")
	if err := os.WriteFile(testMediaFile, []byte("attachment-content"), 0o600); err != nil {
		t.Fatalf("create test media file: %v", err)
	}

	// First ClearSession
	if err := p.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}

	// Verify session file is removed
	if _, err := os.Stat(p.SessionFile()); !os.IsNotExist(err) {
		t.Errorf("session file should have been deleted, err=%v", err)
	}

	// Verify old attachment was deleted
	if _, err := os.Stat(testMediaFile); !os.IsNotExist(err) {
		t.Errorf("cached attachment should have been deleted, err=%v", err)
	}

	// Verify MediaDir still exists as a directory
	info, err := os.Stat(p.MediaDir())
	if err != nil {
		t.Fatalf("MediaDir does not exist after ClearSession: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("MediaDir is not a directory after ClearSession")
	}

	// Verify new attachment can be written immediately without restart
	newMediaFile := filepath.Join(p.MediaDir(), "attachment-after-repair.png")
	if err := os.WriteFile(newMediaFile, []byte("new-media-bytes"), 0o600); err != nil {
		t.Fatalf("writing attachment into MediaDir failed after ClearSession: %v", err)
	}

	// Second ClearSession: verify idempotency
	if err := p.ClearSession(); err != nil {
		t.Fatalf("second ClearSession failed: %v", err)
	}
	info2, err := os.Stat(p.MediaDir())
	if err != nil || !info2.IsDir() {
		t.Fatalf("MediaDir missing after second ClearSession: %v", err)
	}
}

func TestSaveSessionRejectsNilAuth(t *testing.T) {
	p := &Paths{Data: t.TempDir(), Cache: t.TempDir(), Runtime: t.TempDir()}
	if err := p.SaveSession(nil); err == nil {
		t.Fatal("nil auth should be rejected")
	}
}
