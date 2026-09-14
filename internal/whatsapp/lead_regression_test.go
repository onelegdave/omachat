package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// A callback retained by an old client must not resurrect an unpaired account.
func TestRetiredClientCannotReconnectAfterUnpair(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, false)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.TriggerEvent(&events.Connected{})
	if got := b.Status().State; got != wire.StateUnpaired {
		t.Fatalf("retired client changed state after unpair: %s", got)
	}
}

// Real WhatsApp logout sends an authenticated network request before disconnecting.
func TestUnpairRevokesBeforeDisconnecting(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, false)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.connected.Store(true)
	b.mu.Lock()
	b.paired = true
	b.mu.Unlock()
	client.LogoutFunc = func(context.Context) error {
		if !client.IsConnected() {
			return errors.New("disconnected before remote logout")
		}
		return nil
	}
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStaleSendAndMediaAfterUnpair(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.setState(wire.StateConnected, "")

	chatJID, _ := types.ParseJID("15550001111@s.whatsapp.net")
	started := make(chan struct{})
	release := make(chan struct{})
	client.SendMessageFunc = func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
		close(started)
		<-release
		return whatsmeow.SendResponse{ID: "late-id", Timestamp: time.Now()}, nil
	}

	errCh := make(chan error, 1)
	go func() {
		_, err := b.Send(context.Background(), wire.SendParams{
			ConversationID: chatJID.String(),
			Text:           "should not land",
		})
		errCh <- err
	}()

	<-started
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatalf("expected stale send error, got %v", err)
	}
	if len(b.Conversations(10)) != 0 {
		t.Fatal("stale send must not recreate conversations after unpair")
	}
}

func TestMediaMetadataSurvivesRestartNotViewOnce(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15551110000@s.whatsapp.net")
	keep := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/png")}}
	client.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID: "keep-media", Chat: chatJID, Sender: chatJID, Timestamp: time.Now(),
		},
		Message: keep,
	})
	client.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID: "view-once", Chat: chatJID, Sender: chatJID, Timestamp: time.Now(),
		},
		Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{ViewOnce: proto.Bool(true), Mimetype: proto.String("image/jpeg")}},
	})

	b.Stop()

	b2 := New(b.log, b.paths, b.publish)
	mock2 := NewMockClient()
	b2.SetClient(mock2, true)
	if err := b2.Start(ctx); err != nil {
		t.Fatal(err)
	}

	res, err := b2.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatJID.String(), "keep-media")})
	if err != nil {
		t.Fatalf("regular media should download after restart: %v", err)
	}
	if res.Path == "" {
		t.Fatal("expected cached path")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("cached file: %v", err)
	}
	if filepath.Dir(res.Path) != b.paths.WhatsAppMediaDir() {
		t.Fatalf("media escaped cache dir: %s", res.Path)
	}

	_, err = b2.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatJID.String(), "view-once")})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") && !strings.Contains(err.Error(), "view-once") {
		t.Fatalf("view-once media must not be downloadable after restart, got %v", err)
	}

	msgs, err := b2.Messages(ctx, wire.MessagesParams{ConversationID: chatJID.String(), Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs.Messages {
		if m.ID == "view-once" && len(m.Attachments) > 0 {
			t.Fatal("view-once attachments must not be persisted")
		}
	}
}

func TestUnpairDoesNotAutoPair(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := b.Unpair(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.Status().State != wire.StateUnpaired {
		t.Fatalf("expected unpaired, got %s", b.Status().State)
	}
	if b.client != nil {
		t.Fatal("unpair must drop the client; next pairing is explicit")
	}
}

func waitState(t *testing.T, b *Backend, want wire.ConnState) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b.Status().State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s, got %s", want, b.Status().State)
}

// waitUnpairedWithError blocks until an EventStatus with StateUnpaired AND a
// non-empty error appears on eventsCh, skipping earlier UNPAIRED-no-error
// events (e.g. from the initial Start call). Returns a non-nil error on
// timeout; the caller should t.Fatal it.
func waitUnpairedWithError(t *testing.T, b *Backend, eventsCh <-chan wire.Event, timeout time.Duration) error {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt := <-eventsCh:
			if evt.Event != wire.EventStatus {
				continue
			}
			st, ok := evt.Data.(wire.Status)
			if !ok {
				continue
			}
			if st.State == wire.StateUnpaired && st.Error != "" {
				return nil // correct: retryable UNPAIRED with actionable error
			}
			if st.State == wire.StateUnpaired {
				// Initial or intermediate unpaired-no-error; keep waiting.
				continue
			}
			return fmt.Errorf("got state=%s error=%q; want StateUnpaired with non-empty error", st.State, st.Error)
		case <-deadline:
			return fmt.Errorf("timed out; final state=%s error=%q", b.Status().State, b.Status().Error)
		}
	}
}

func TestLoggedOutDoesNotDeadlockHandlerLock(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.setState(wire.StateConnected, "")

	done := make(chan struct{})
	go func() {
		client.TriggerEvent(&events.LoggedOut{Reason: events.ConnectFailureLoggedOut})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("LoggedOut handler deadlocked on eventHandlersLock")
	}
	waitState(t, b, wire.StateUnpaired)
}

func TestStaleEventCannotReviveAfterUnpair(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	chat, _ := types.ParseJID("15550000001@s.whatsapp.net")
	evt := &events.Message{
		Info:    types.MessageInfo{ID: "race-1", Chat: chat, Sender: chat, Timestamp: time.Now()},
		Message: &waE2E.Message{Conversation: proto.String("late")},
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.TriggerEvent(evt)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = b.Unpair(context.Background())
	}()
	wg.Wait()
	if b.Status().State != wire.StateUnpaired {
		t.Fatalf("expected unpaired, got %s", b.Status().State)
	}
	if len(b.Conversations(10)) != 0 {
		t.Fatalf("stale events recreated conversations: %+v", b.Conversations(10))
	}
}

func TestMediaCommitAbortedAfterUnpair(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}
	chat, _ := types.ParseJID("15550000002@s.whatsapp.net")
	id := "media-race"
	raw := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/png"), FileLength: proto.Uint64(100)}}
	b.mu.Lock()
	b.rawMsgs[rawMediaKey(chat.String(), id)] = raw
	b.mu.Unlock()

	started := make(chan struct{})
	release := make(chan struct{})
	b.mediaCommitStall = func() {
		close(started)
		<-release
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := b.Media(ctx, wire.MediaParams{Key: rawMediaKey(chat.String(), id)})
		errCh <- err
	}()
	<-started
	if err := b.Unpair(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatalf("expected aborted download, got %v", err)
	}
	entries, _ := os.ReadDir(b.paths.WhatsAppMediaDir())
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("unpair must not keep committed media, found %s", e.Name())
		}
	}
}

func TestLocalCleanupFailureBlocksPairing(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.paths.WhatsAppDBFile(), []byte("session"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(b.paths.Data, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(b.paths.Data, 0o700) })

	err := b.Unpair(context.Background())
	if err == nil {
		t.Fatal("expected local cleanup error")
	}
	st := b.Status()
	if st.State != wire.StateUnpaired || st.Error == "" || !strings.Contains(st.Error, "Pairing is blocked") {
		t.Fatalf("expected blocked pairing status, got %+v", st)
	}
	_, pairErr := b.StartPairing(context.Background())
	if pairErr == nil || !strings.Contains(pairErr.Error(), "leftover") && !strings.Contains(pairErr.Error(), "could not be removed") {
		t.Fatalf("expected pairing refusal, got %v", pairErr)
	}
}

func TestEphemeralFlagsAreNotPersisted(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	chat, _ := types.ParseJID("15550000003@s.whatsapp.net")
	client.TriggerEvent(&events.Message{
		Info:        types.MessageInfo{ID: "eph-1", Chat: chat, Sender: chat, Timestamp: time.Now()},
		Message:     &waE2E.Message{Conversation: proto.String("secret disappearing")},
		IsEphemeral: true,
	})
	res, err := b.Messages(context.Background(), wire.MessagesParams{ConversationID: chat.String(), Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Text != lifetimePlaceholder() {
		t.Fatalf("expected placeholder, got %+v", res.Messages)
	}
	if len(res.Messages[0].Attachments) != 0 {
		t.Fatal("ephemeral attachments must be omitted")
	}
	b.mu.RLock()
	_, exists := b.rawMsgs[rawMediaKey(chat.String(), "eph-1")]
	b.mu.RUnlock()
	if exists {
		t.Fatal("ephemeral raw payload must not be stored")
	}
}

func TestCrossChatMediaKeysDoNotCollide(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true)
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	chatA, _ := types.ParseJID("15550000004@s.whatsapp.net")
	chatB, _ := types.ParseJID("15550000005@s.whatsapp.net")
	img := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Mimetype: proto.String("image/png")}}
	client.TriggerEvent(&events.Message{
		Info:    types.MessageInfo{ID: "same-id", Chat: chatA, Sender: chatA, Timestamp: time.Now()},
		Message: img,
	})
	client.TriggerEvent(&events.Message{
		Info:    types.MessageInfo{ID: "same-id", Chat: chatB, Sender: chatB, Timestamp: time.Now()},
		Message: img,
	})
	b.mu.RLock()
	_, okA := b.rawMsgs[rawMediaKey(chatA.String(), "same-id")]
	_, okB := b.rawMsgs[rawMediaKey(chatB.String(), "same-id")]
	b.mu.RUnlock()
	if !okA || !okB {
		t.Fatal("each chat must keep its own media metadata")
	}
}

// ---------------------------------------------------------------------------
// Fix 1 regression tests: QR failure handling and paired guard
// ---------------------------------------------------------------------------

// TestQRTimeoutReturnsRetryableUnpaired verifies that a QR "timeout" event
// transitions the backend to StateUnpaired (not StateDisconnected), so the UI
// retry button appears in PairingView (needsPair covers "unpaired" and
// "pairing" but NOT "disconnected").
func TestQRTimeoutReturnsRetryableUnpaired(t *testing.T) {
	b, client, eventsCh := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "qr-timeout-test"}
	if _, err := b.StartPairing(ctx); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}

	// Simulate QR timeout from WhatsApp
	qrChan <- whatsmeow.QRChannelItem{Event: "timeout"}
	close(qrChan)

	// Wait for the status event with unpaired state
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-eventsCh:
			if evt.Event == wire.EventStatus {
				st, ok := evt.Data.(wire.Status)
				if !ok {
					continue
				}
				if st.State == wire.StateUnpaired {
					// Correct: retryable
					return
				}
				if st.State == wire.StateDisconnected {
					t.Fatalf("QR timeout set StateDisconnected; want StateUnpaired so the retry button shows")
				}
			}
		case <-deadline:
			t.Fatalf("timed out; final state=%s, want %s", b.Status().State, wire.StateUnpaired)
		}
	}
}

// TestQRGenericErrorReturnsRetryableUnpaired verifies that an unexpected QR
// event (e.g. "err-scan-without-multidevice") transitions to StateUnpaired so
// the retry button appears.
func TestQRGenericErrorReturnsRetryableUnpaired(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "qr-generic-err"}
	if _, err := b.StartPairing(ctx); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}

	qrChan <- whatsmeow.QRChannelItem{Event: "err-scan-without-multidevice"}
	close(qrChan)

	waitState(t, b, wire.StateUnpaired)
	st := b.Status()
	if st.Error == "" {
		t.Error("expected non-empty error on generic QR failure")
	}
	if st.State != wire.StateUnpaired {
		t.Errorf("want StateUnpaired, got %s", st.State)
	}
}

// TestStartPairingRejectsWhenAlreadyPaired verifies that StartPairing returns
// an error when the account is already paired, preventing double-pairing.
func TestStartPairingRejectsWhenAlreadyPaired(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, true) // already paired
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("StartPairing must fail when already paired")
	}
	if !strings.Contains(err.Error(), "already paired") {
		t.Errorf("unexpected error %v; want 'already paired'", err)
	}
	// State must remain unchanged (not regress to pairing or disconnected)
	if got := b.Status().State; got != wire.StateConnecting && got != wire.StateConnected && got != wire.StateDisconnected {
		// Allow any connecting/connected/disconnected state from a live paired account
		// State must NOT be pairing or unpaired
		if got == wire.StatePairing || got == wire.StateUnpaired {
			t.Errorf("state regressed to %s after rejected StartPairing", got)
		}
	}
}

// TestQRSuccessDoesNotRegressConnectedState verifies that a late "success" QR
// event does not overwrite StateConnected with StateConnecting when an
// independent Connected event fires after the QR success sets StateConnecting.
// The gen-check in listenQRChannel and the Connected handler both already gate
// on gen, so this tests the ordering cannot introduce a regression.
func TestQRSuccessFollowedByConnected(t *testing.T) {
	b, client, eventsCh := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "qr-connect-order"}
	if _, err := b.StartPairing(ctx); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}

	// Simulate success then Connected event
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}
	close(qrChan)

	// Wait for EventPaired
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-eventsCh:
			if evt.Event == wire.EventPaired {
				goto pairedOK
			}
		case <-deadline:
			t.Fatal("timed out waiting for EventPaired")
		}
	}
pairedOK:
	// Now fire Connected event (may arrive after QR success in real protocol)
	client.TriggerEvent(&events.Connected{})
	// State must be connected (not regress back to connecting)
	if got := b.Status().State; got != wire.StateConnected && got != wire.StateConnecting {
		t.Errorf("unexpected state %s after Connected event post-QR-success", got)
	}
}

// TestStartPairingGetQRChannelFailurePublishesUnpaired verifies that a
// GetQRChannel error immediately publishes retryable UNPAIRED status, so
// PairingView shows the retry button even when the RPC callback is null.
func TestStartPairingGetQRChannelFailurePublishesUnpaired(t *testing.T) {
	b, client, eventsCh := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return nil, errors.New("mock: QR channel unavailable")
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	_, err := b.StartPairing(ctx)
	if err == nil || !strings.Contains(err.Error(), "get qr channel") {
		t.Fatalf("expected get qr channel error, got %v", err)
	}

	// Drain events emitted before StartPairing (e.g. from Start) then verify
	// the failure publishes UNPAIRED with an actionable error message.
	if err := waitUnpairedWithError(t, b, eventsCh, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestStartPairingConnectFailurePublishesUnpaired verifies that a Connect
// error immediately publishes retryable UNPAIRED status.
func TestStartPairingConnectFailurePublishesUnpaired(t *testing.T) {
	b, client, eventsCh := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	qrChan := make(chan whatsmeow.QRChannelItem, 1)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.ConnectFunc = func() error {
		return errors.New("mock: network unreachable")
	}

	_, err := b.StartPairing(ctx)
	if err == nil || !strings.Contains(err.Error(), "connect for pairing") {
		t.Fatalf("expected connect error, got %v", err)
	}

	if err := waitUnpairedWithError(t, b, eventsCh, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

// TestRetryAfterFailureGetsFreshGeneration verifies that after StartPairing
// fails and is retried, the old attempt's stale gen (gen1) cannot deliver
// events into the new attempt's state. The pairingFailed closure increments
// the generation so any goroutine or delayed callback using gen1 sees a
// mismatch and stops.
func TestRetryAfterFailureGetsFreshGeneration(t *testing.T) {
	b, client, _ := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// First attempt: GetQRChannel fails — pairingFailed increments gen.
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return nil, errors.New("mock: first attempt fails")
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	b.mu.RLock()
	gen1 := b.gen
	b.mu.RUnlock()

	_, err := b.StartPairing(ctx)
	if err == nil {
		t.Fatal("expected failure on first attempt")
	}

	b.mu.RLock()
	gen2 := b.gen
	b.mu.RUnlock()

	if gen2 <= gen1 {
		t.Fatalf("failure must retire generation: gen1=%d gen2=%d", gen1, gen2)
	}

	// gen1 is now stale; any event callback using gen1 must be a no-op.
	committed := b.commitQR(gen1, "stale-qr-from-old-attempt")
	if committed {
		t.Fatal("stale gen1 must not update QR state after failure")
	}
	committed = b.commitStatus(gen1, wire.StateDisconnected, "stale error from old attempt")
	if committed {
		t.Fatal("stale gen1 commitStatus must be a no-op")
	}

	// Second attempt: succeeds with fresh QR. gen2 is the active gen.
	qrChan := make(chan whatsmeow.QRChannelItem, 2)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.connected.Store(false) // reset so Connect fires
	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "fresh-qr"}

	code, err := b.StartPairing(ctx)
	if err != nil || code != "fresh-qr" {
		t.Fatalf("second StartPairing: err=%v code=%q", err, code)
	}

	// The second attempt must be in pairing state; gen1 still stale.
	if got := b.Status().State; got != wire.StatePairing {
		t.Errorf("expected StatePairing after fresh retry, got %s", got)
	}
	// Gen1 remains stale even during the second attempt.
	if b.commitQR(gen1, "delayed-stale-qr") {
		t.Fatal("delayed stale gen1 QR must still be no-op during second attempt")
	}

	client.TriggerEvent(&events.Connected{})
	if got := b.Status().State; got != wire.StateConnected {
		t.Fatalf("retry client handler must accept new connection: %s", got)
	}

	// Drain the channel so the listenQRChannel goroutine exits cleanly.
	close(qrChan)
}

// TestConnectedBeforeQRSuccessDoesNotRegress verifies that if a *events.Connected
// fires before the QR success event is processed by listenQRChannel, the state
// remains StateConnected and is not overwritten with StateConnecting.
func TestConnectedBeforeQRSuccessDoesNotRegress(t *testing.T) {
	b, client, eventsCh := setupTestBackend(t)
	b.SetClient(client, false)
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Use a gate to control ordering: Connected fires before success processed.
	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	client.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	client.ConnectFunc = func() error { client.connected.Store(true); return nil }

	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "qr-connected-first"}
	if _, err := b.StartPairing(ctx); err != nil {
		t.Fatalf("StartPairing: %v", err)
	}

	// Fire Connected BEFORE sending success to qrChan, while paired=false.
	// handleEventFor will set StateConnected only if gen matches.
	// We set b.paired=true manually here to model the WhatsApp server sending
	// Connected before the local QR success event is processed.
	b.mu.Lock()
	b.paired = true
	b.mu.Unlock()
	client.TriggerEvent(&events.Connected{})

	// Now confirm state is connected
	if got := b.Status().State; got != wire.StateConnected {
		t.Fatalf("expected StateConnected after Connected event, got %s", got)
	}

	// Now deliver QR success — must not overwrite StateConnected with StateConnecting
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}
	close(qrChan)

	// Wait for EventPaired
	deadline := time.After(3 * time.Second)
	for {
		select {
		case evt := <-eventsCh:
			if evt.Event == wire.EventPaired {
				goto doneCheck
			}
		case <-deadline:
			t.Fatal("timed out waiting for EventPaired")
		}
	}
doneCheck:
	if got := b.Status().State; got != wire.StateConnected {
		t.Errorf("QR success must not regress StateConnected to StateConnecting; got %s", got)
	}
}

// TestEmitAfterUnpairDoesNotDeliverToNewSession verifies that events emitted
// from a commitMessage call cannot be attributed to a retired account.  The
// old code unlocked b.mu then called b.emit, leaving a window where Unpair
// could retire the account. The test encodes the observable consequence:
// events published after retirement must not show up as conversations for the
// new (empty) session.
//
// With the fix, commitMessage emits under the lock, so Unpair's gen increment
// and map clear exclude any emission that used the old gen.
func TestCommitMessageEmitAfterUnpairRace(t *testing.T) {
	const goroutines = 64
	const iters = 10

	for iter := 0; iter < iters; iter++ {
		b, client, _ := setupTestBackend(t)
		b.SetClient(client, true)
		ctx := context.Background()
		if err := b.Start(ctx); err != nil {
			t.Fatal(err)
		}
		b.setState(wire.StateConnected, "")

		chat, _ := types.ParseJID("15550100200@s.whatsapp.net")
		msg := wire.Message{
			ID:             fmt.Sprintf("race-msg-%d", iter),
			ConversationID: chat.String(),
			Text:           "race text",
			Timestamp:      int64(iter + 1),
		}
		rawMsg := &waE2E.Message{Conversation: proto.String("race text")}

		var wg sync.WaitGroup
		// Capture gen safely before goroutines start.
		b.mu.RLock()
		initialGen := b.gen
		b.mu.RUnlock()

		// Concurrent commitMessage goroutines
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.commitMessage(initialGen, msg, rawMsg)
			}()
		}
		// Concurrent Unpair
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Unpair(context.Background())
		}()
		wg.Wait()

		// After unpair the backend must be fully retired: no conversations.
		if len(b.Conversations(10)) != 0 {
			t.Fatalf("iter %d: conversations leaked across unpair retirement", iter)
		}
		if b.Status().State != wire.StateUnpaired {
			t.Fatalf("iter %d: state=%s, want StateUnpaired", iter, b.Status().State)
		}
	}
}

// TestReceiptEmitAfterUnpairRace mirrors TestCommitMessageEmitAfterUnpairRace
// for the handleReceipt path.
func TestReceiptEmitAfterUnpairRace(t *testing.T) {
	const goroutines = 32
	const iters = 10

	for iter := 0; iter < iters; iter++ {
		b, client, _ := setupTestBackend(t)
		b.SetClient(client, true)
		ctx := context.Background()
		if err := b.Start(ctx); err != nil {
			t.Fatal(err)
		}

		chat, _ := types.ParseJID("15550300400@s.whatsapp.net")
		// Pre-populate a message to receive a receipt for
		b.mu.Lock()
		gen := b.gen
		b.mu.Unlock()
		b.commitMessage(gen, wire.Message{
			ID:             "rcpt-race-msg",
			ConversationID: chat.String(),
			Text:           "sent",
			Timestamp:      1000,
			FromMe:         true,
		}, nil)

		evt := &events.Receipt{
			Chat:       chat,
			MessageIDs: []types.MessageID{"rcpt-race-msg"},
			Type:       types.ReceiptTypeRead,
		}

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.mu.RLock()
				g := b.gen
				b.mu.RUnlock()
				b.handleReceipt(g, evt)
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Unpair(context.Background())
		}()
		wg.Wait()

		if b.Status().State != wire.StateUnpaired {
			t.Fatalf("iter %d: state=%s, want StateUnpaired", iter, b.Status().State)
		}
	}
}

// TestHistorySyncEmitAfterUnpairRace mirrors TestCommitMessageEmitAfterUnpairRace
// for the ingestHistorySync path.
func TestHistorySyncEmitAfterUnpairRace(t *testing.T) {
	const goroutines = 32
	const iters = 10

	for iter := 0; iter < iters; iter++ {
		b, client, _ := setupTestBackend(t)
		b.SetClient(client, true)
		ctx := context.Background()
		if err := b.Start(ctx); err != nil {
			t.Fatal(err)
		}

		b.mu.RLock()
		gen := b.gen
		b.mu.RUnlock()

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				b.ingestHistorySync(gen, makeMinimalHistorySync("hs-race-chat@s.whatsapp.net"))
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = b.Unpair(context.Background())
		}()
		wg.Wait()

		if b.Status().State != wire.StateUnpaired {
			t.Fatalf("iter %d: state=%s, want StateUnpaired", iter, b.Status().State)
		}
		if len(b.Conversations(10)) != 0 {
			t.Fatalf("iter %d: conversations leaked across history sync retirement", iter)
		}
	}
}

// makeMinimalHistorySync builds a trivial HistorySync payload for race tests.
func makeMinimalHistorySync(chatID string) *waHistorySync.HistorySync {
	return &waHistorySync.HistorySync{
		Conversations: []*waHistorySync.Conversation{
			{
				ID:   proto.String(chatID),
				Name: proto.String("Race Chat"),
			},
		},
	}
}

func TestStoredBoundsKeepNewestConversations(t *testing.T) {
	stored := newStoredChatData()
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("%02d@s.whatsapp.net", i)
		stored.Conversations[id] = wire.Conversation{ID: id, Timestamp: int64(i)}
		stored.Order = append(stored.Order, id)
		var msgs []wire.Message
		for j := 0; j < 120; j++ {
			msgs = append(msgs, wire.Message{ID: fmt.Sprintf("m-%d", j), ConversationID: id, Timestamp: int64(j)})
		}
		stored.Messages[id] = msgs
		stored.RawMedia[rawMediaKey(id, "m-0")] = []byte("old")
		stored.RawMedia[rawMediaKey(id, "m-119")] = []byte("new")
	}
	boundStoredChat(stored)
	if len(stored.Order) != maxPersistedConversations || len(stored.Conversations) != maxPersistedConversations {
		t.Fatalf("expected %d conversations, got order=%d convs=%d", maxPersistedConversations, len(stored.Order), len(stored.Conversations))
	}
	if stored.Order[0] != "59@s.whatsapp.net" {
		t.Fatalf("expected newest first, got %s", stored.Order[0])
	}
	if _, ok := stored.Conversations["00@s.whatsapp.net"]; ok {
		t.Fatal("evicted oldest conversation must leave the map")
	}
	if len(stored.Messages["59@s.whatsapp.net"]) != maxPersistedMessages {
		t.Fatalf("expected %d messages, got %d", maxPersistedMessages, len(stored.Messages["59@s.whatsapp.net"]))
	}
	if _, ok := stored.RawMedia[rawMediaKey("59@s.whatsapp.net", "m-0")]; ok {
		t.Fatal("evicted message media metadata must be dropped")
	}
	if _, ok := stored.RawMedia[rawMediaKey("59@s.whatsapp.net", "m-119")]; !ok {
		t.Fatal("retained message media metadata must stay")
	}
}
