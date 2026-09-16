package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

func setupTestBackend(t *testing.T) (*Backend, *MockClient, chan wire.Event) {
	t.Helper()
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()

	paths := &appStore.Paths{
		Data:    dataDir,
		Cache:   cacheDir,
		Runtime: runtimeDir,
	}
	_ = os.MkdirAll(paths.WhatsAppMediaDir(), 0o700)

	eventsCh := make(chan wire.Event, 100)
	publish := func(evt wire.Event) {
		select {
		case eventsCh <- evt:
		default:
		}
	}

	logger := zerolog.Nop()
	backend := New(logger, paths, publish)
	mock := NewMockClient()
	return backend, mock, eventsCh
}

func TestWhatsAppStartUnpaired(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := backend.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	st := backend.Status()
	if st.Network != wire.NetworkWhatsApp {
		t.Errorf("expected network %q, got %q", wire.NetworkWhatsApp, st.Network)
	}
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}
	if mock.IsConnected() {
		t.Errorf("unpaired backend must not connect automatically")
	}
}

func TestWhatsAppStartPairedReconnects(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	connectedCh := make(chan struct{}, 1)
	mock.ConnectFunc = func() error {
		mock.connected.Store(true)
		connectedCh <- struct{}{}
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := backend.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-connectedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for automatic reconnect")
	}

	mock.TriggerEvent(&events.Connected{})
	st := backend.Status()
	if st.State != wire.StateConnected {
		t.Errorf("expected state %q, got %q", wire.StateConnected, st.State)
	}
}

func TestWhatsAppStartPairingFlow(t *testing.T) {
	backend, mock, eventsCh := setupTestBackend(t)
	backend.SetClient(mock, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	qrChan := make(chan whatsmeow.QRChannelItem, 4)
	mock.GetQRChannelFunc = func(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
		return qrChan, nil
	}
	mock.ConnectFunc = func() error {
		mock.connected.Store(true)
		return nil
	}

	// Send initial QR code
	qrChan <- whatsmeow.QRChannelItem{Event: whatsmeow.QRChannelEventCode, Code: "whatsapp-qr-123"}

	code, err := backend.StartPairing(ctx)
	if err != nil {
		t.Fatalf("StartPairing: %v", err)
	}
	if code != "whatsapp-qr-123" {
		t.Errorf("got code %q, want %q", code, "whatsapp-qr-123")
	}

	st := backend.Status()
	if st.State != wire.StatePairing {
		t.Errorf("expected state %q, got %q", wire.StatePairing, st.State)
	}
	if st.QRURL != "whatsapp-qr-123" {
		t.Errorf("expected QRURL %q, got %q", "whatsapp-qr-123", st.QRURL)
	}

	// Simulate phone scanning and successful pair
	qrChan <- whatsmeow.QRChannelItem{Event: "success"}

	// Wait for paired event
	paired := false
	deadline := time.After(2 * time.Second)
	for !paired {
		select {
		case evt := <-eventsCh:
			if evt.Event == wire.EventPaired && evt.Network == wire.NetworkWhatsApp {
				paired = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for EventPaired")
		}
	}

	st = backend.Status()
	if st.State != wire.StateConnecting {
		t.Errorf("expected state %q after pair, got %q", wire.StateConnecting, st.State)
	}
}

func TestWhatsAppIncomingMessageAndConversationTouch(t *testing.T) {
	backend, mock, eventsCh := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")
	senderJID, _ := types.ParseJID("15551234567@s.whatsapp.net")

	evt := &events.Message{
		Info: types.MessageInfo{
			ID:        "msg-alpha-1",
			Chat:      chatJID,
			Sender:    senderJID,
			Timestamp: time.Now(),
			PushName:  "Alice",
		},
		Message: &waE2E.Message{
			Conversation: proto.String("Hello from Alice!"),
		},
	}

	mock.TriggerEvent(evt)

	// Verify events
	var gotMsg wire.Message
	var gotConv wire.Conversation
	gotMsgEvent := false
	gotConvEvent := false

	deadline := time.After(2 * time.Second)
	for !gotMsgEvent || !gotConvEvent {
		select {
		case e := <-eventsCh:
			if e.Network != wire.NetworkWhatsApp {
				continue
			}
			if e.Event == wire.EventMessage {
				gotMsg = e.Data.(wire.Message)
				gotMsgEvent = true
			}
			if e.Event == wire.EventConversation {
				gotConv = e.Data.(wire.Conversation)
				gotConvEvent = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for message/conversation events")
		}
	}

	if gotMsg.ID != "msg-alpha-1" || gotMsg.Text != "Hello from Alice!" {
		t.Errorf("unexpected message: %+v", gotMsg)
	}
	if gotConv.ID != chatJID.String() || gotConv.Preview != "Hello from Alice!" || !gotConv.Unread {
		t.Errorf("unexpected conversation: %+v", gotConv)
	}

	// Verify Conversations() list
	convs := backend.Conversations(10)
	if len(convs) != 1 || convs[0].ID != chatJID.String() {
		t.Errorf("unexpected conv list: %+v", convs)
	}

	// Verify Messages()
	res, err := backend.Messages(ctx, wire.MessagesParams{ConversationID: chatJID.String(), Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 || res.Messages[0].ID != "msg-alpha-1" {
		t.Errorf("unexpected messages result: %+v", res)
	}

	// Test MarkRead
	if err := backend.MarkRead(ctx, wire.MarkReadParams{ConversationID: chatJID.String(), MessageID: "msg-alpha-1"}); err != nil {
		t.Fatal(err)
	}
	st := backend.Status()
	if st.Unread != 0 {
		t.Errorf("expected 0 unread, got %d", st.Unread)
	}
}

func TestWhatsAppSendTextMessage(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend.setState(wire.StateConnected, "")

	chatJID, _ := types.ParseJID("15559876543@s.whatsapp.net")
	sentText := ""
	mock.SendMessageFunc = func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
		sentText = message.GetConversation()
		return whatsmeow.SendResponse{
			ID:        "server-msg-id-888",
			Timestamp: time.Now(),
		}, nil
	}

	msg, err := backend.Send(ctx, wire.SendParams{
		TmpID:          "tmp-tx-1",
		ConversationID: chatJID.String(),
		Text:           "Can you hear me now?",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sentText != "Can you hear me now?" {
		t.Errorf("unexpected sent text: %q", sentText)
	}
	if msg.ID != "server-msg-id-888" || msg.TmpID != "tmp-tx-1" || !msg.FromMe {
		t.Errorf("unexpected message result: %+v", msg)
	}

	convs := backend.Conversations(5)
	if len(convs) != 1 || convs[0].Preview != "Can you hear me now?" || !convs[0].PreviewMine {
		t.Errorf("conversation preview not updated: %+v", convs)
	}
}

func TestWhatsAppSendMediaImage(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend.setState(wire.StateConnected, "")

	// Write dummy image to disk
	tmpImg := filepath.Join(t.TempDir(), "test.png")
	pngBytes := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(tmpImg, pngBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	uploadedBytes := 0
	mock.UploadFunc = func(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
		uploadedBytes = len(plaintext)
		return whatsmeow.UploadResponse{
			URL:        "https://mock.whatsapp.net/1",
			DirectPath: "/direct/1",
		}, nil
	}

	chatJID, _ := types.ParseJID("15559876543@s.whatsapp.net")
	res, err := backend.SendMedia(ctx, wire.SendMediaParams{
		TmpID:          "tmp-img-1",
		ConversationID: chatJID.String(),
		Path:           tmpImg,
		Caption:        "Check out this picture",
	})
	if err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if uploadedBytes != len(pngBytes) {
		t.Errorf("expected %d uploaded bytes, got %d", len(pngBytes), uploadedBytes)
	}
	if res.Message == nil || res.Message.Text != "Check out this picture" || len(res.Message.Attachments) != 1 {
		t.Errorf("unexpected SendMediaResult: %+v", res)
	}
}

func TestWhatsAppSendMediaGIFAsPlaybackVideo(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend.setState(wire.StateConnected, "")

	gifPath := filepath.Join(t.TempDir(), "test.gif")
	gifBytes := []byte{
		0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00,
		0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0x21,
		0xf9, 0x04, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00,
		0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44,
		0x01, 0x00, 0x3b,
	}
	if err := os.WriteFile(gifPath, gifBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	mp4Bytes := []byte("synthetic-mp4")
	backend.convertGIF = func(context.Context, string) ([]byte, error) {
		return mp4Bytes, nil
	}

	var uploadedType whatsmeow.MediaType
	mock.UploadFunc = func(ctx context.Context, plaintext []byte, appInfo whatsmeow.MediaType) (whatsmeow.UploadResponse, error) {
		uploadedType = appInfo
		if string(plaintext) != string(mp4Bytes) {
			t.Errorf("uploaded original GIF instead of converted MP4: %q", plaintext)
		}
		return whatsmeow.UploadResponse{URL: "https://mock.whatsapp.net/gif", DirectPath: "/direct/gif"}, nil
	}
	var sent *waE2E.Message
	mock.SendMessageFunc = func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
		sent = message
		return whatsmeow.SendResponse{ID: "server-gif-id", Timestamp: time.Now()}, nil
	}

	chatJID, _ := types.ParseJID("15559876543@s.whatsapp.net")
	res, err := backend.SendMedia(ctx, wire.SendMediaParams{
		TmpID: "tmp-gif-1", ConversationID: chatJID.String(), Path: gifPath,
	})
	if err != nil {
		t.Fatalf("SendMedia GIF: %v", err)
	}
	if uploadedType != whatsmeow.MediaVideo {
		t.Errorf("uploaded type = %v, want MediaVideo", uploadedType)
	}
	if sent == nil || sent.GetImageMessage() != nil || sent.GetVideoMessage() == nil {
		t.Fatalf("expected a video message, got %+v", sent)
	}
	if !sent.GetVideoMessage().GetGifPlayback() || sent.GetVideoMessage().GetMimetype() != "video/mp4" {
		t.Errorf("expected GIF-playback MP4, got %+v", sent.GetVideoMessage())
	}
	if res.Message == nil || len(res.Message.Attachments) != 1 {
		t.Fatalf("unexpected GIF result: %+v", res)
	}
	att := res.Message.Attachments[0]
	if !att.IsGif || !att.IsVideo || att.IsImage || att.MimeType != "video/mp4" {
		t.Errorf("unexpected GIF attachment flags: %+v", att)
	}
}

func TestWhatsAppIncomingGIFPlaybackStaysInline(t *testing.T) {
	msg := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Mimetype: proto.String("video/mp4"), GifPlayback: proto.Bool(true),
		Width: proto.Uint32(320), Height: proto.Uint32(180), FileLength: proto.Uint64(1234),
	}}
	atts := extractAttachments(msg, "chat", "gif-message")
	if len(atts) != 1 {
		t.Fatalf("expected one attachment, got %+v", atts)
	}
	if !atts[0].IsGif || !atts[0].IsVideo || atts[0].IsImage {
		t.Errorf("incoming GIF playback lost its flags: %+v", atts[0])
	}
	if atts[0].Width != 320 || atts[0].Height != 180 {
		t.Errorf("incoming GIF dimensions lost: %+v", atts[0])
	}
}

func TestWhatsAppMediaDownload(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15551112222@s.whatsapp.net")
	rawImgMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption: proto.String("Attached image"),
		},
	}

	mock.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID:        "media-msg-99",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: rawImgMsg,
	})

	res, err := backend.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatJID.String(), "media-msg-99")})
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	if res.Path == "" {
		t.Errorf("expected non-empty path, got empty")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("cached file does not exist: %v", err)
	}
}

func TestWhatsAppHistorySync(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatStr := "15557778888@s.whatsapp.net"
	chatName := "History Sync Friend"
	syncEvt := &events.HistorySync{
		Data: &waHistorySync.HistorySync{
			Conversations: []*waHistorySync.Conversation{
				{
					ID:   proto.String(chatStr),
					Name: proto.String(chatName),
					Messages: []*waHistorySync.HistorySyncMsg{
						{
							Message: &waWeb.WebMessageInfo{
								Key: &waCommon.MessageKey{
									ID:     proto.String("hist-msg-1"),
									FromMe: proto.Bool(false),
								},
								MessageTimestamp: proto.Uint64(1700000000),
								Message: &waE2E.Message{
									Conversation: proto.String("Synced historical text"),
								},
							},
						},
					},
				},
				{
					ID:   proto.String("192148934783072@lid"),
					Name: proto.String("Empty Companion Contact"),
				},
			},
		},
	}

	mock.TriggerEvent(syncEvt)

	convs := backend.Conversations(5)
	if len(convs) != 1 || convs[0].ID != chatStr || convs[0].Name != chatName {
		t.Fatalf("unexpected conversations after history sync: %+v", convs)
	}
	if convs[0].Preview != "Synced historical text" {
		t.Errorf("unexpected preview: %q", convs[0].Preview)
	}

	res, err := backend.Messages(ctx, wire.MessagesParams{ConversationID: chatStr, Count: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Messages) != 1 || res.Messages[0].Text != "Synced historical text" {
		t.Errorf("unexpected messages: %+v", res.Messages)
	}
}

func TestWhatsAppUnpairTruthfulErrorAndCleanup(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend.setState(wire.StateConnected, "")

	mock.LogoutFunc = func(ctx context.Context) error {
		return errors.New("network unreachable")
	}

	// Unpair should attempt logout, clean up local state, and truthfully return the error
	err := backend.Unpair(ctx)
	if err == nil {
		t.Fatal("expected error from unpair when remote logout fails, got nil")
	}
	if !errors.Is(err, err) && err.Error() == "" {
		t.Error("error string must be non-empty")
	}

	st := backend.Status()
	if st.State != wire.StateUnpaired {
		t.Errorf("expected state %q, got %q", wire.StateUnpaired, st.State)
	}

	// Verify local state was cleaned up
	if len(backend.Conversations(10)) != 0 {
		t.Error("conversations must be empty after unpair")
	}
}

func TestWhatsAppRemoteLoggedOut(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend.setState(wire.StateConnected, "")

	mock.TriggerEvent(&events.LoggedOut{})
	waitState(t, backend, wire.StateUnpaired)
	st := backend.Status()
	if st.Error == "" {
		t.Error("expected explanation message on remote unpair")
	}
}

func TestChatStorePersistence(t *testing.T) {
	dataDir := t.TempDir()
	cacheDir := t.TempDir()
	runtimeDir := t.TempDir()
	paths := &appStore.Paths{Data: dataDir, Cache: cacheDir, Runtime: runtimeDir}
	_ = os.MkdirAll(paths.WhatsAppMediaDir(), 0o700)

	log := zerolog.Nop()
	backend1 := New(log, paths, nil)
	mock1 := NewMockClient()
	backend1.SetClient(mock1, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend1.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")
	backend1.appendMessage(wire.Message{
		ID:             "msg-persist-1",
		ConversationID: chatJID.String(),
		Text:           "Persisted WhatsApp chat",
		Timestamp:      1000,
		FromMe:         false,
	}, nil)

	// Stop backend1
	backend1.Stop()

	// Verify whatsapp_store.json was created with 0600 permissions
	fi, err := os.Stat(paths.WhatsAppStoreFile())
	if err != nil {
		t.Fatalf("expected store file to exist: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("expected permissions 0600, got %v", fi.Mode().Perm())
	}

	// Start backend2 with same paths
	backend2 := New(log, paths, nil)
	mock2 := NewMockClient()
	backend2.SetClient(mock2, true)
	if err := backend2.Start(ctx); err != nil {
		t.Fatal(err)
	}

	convs := backend2.Conversations(10)
	if len(convs) != 1 || convs[0].Preview != "Persisted WhatsApp chat" {
		t.Fatalf("expected restored conversation, got %+v", convs)
	}

	res, err := backend2.Messages(ctx, wire.MessagesParams{ConversationID: chatJID.String()})
	if err != nil || len(res.Messages) != 1 || res.Messages[0].Text != "Persisted WhatsApp chat" {
		t.Fatalf("expected restored message, got %+v (err: %v)", res, err)
	}
}

func TestMediaSecurityAndOpaquePaths(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)
	ctx := context.Background()

	// 1. Unauthorized key must fail
	_, err := backend.Media(ctx, wire.MediaParams{Key: "../escaped/path"})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("expected unauthorized media error, got %v", err)
	}

	// 2. Authorized media download saves to opaque hash path
	mediaID := "media-sec-id-1"
	rawMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Mimetype: proto.String("image/png"),
		},
	}
	backend.mu.Lock()
	backend.rawMsgs[mediaID] = rawMsg
	backend.mu.Unlock()

	mock.DownloadAnyFunc = func(ctx context.Context, msg *waE2E.Message) ([]byte, error) {
		// Valid 1x1 PNG bytes
		return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15c4\x00\x00\x00\rIDATx\x9cc\xf8\xff\xff?\x00\x05\xfe\x02\xfe\r\xef\x8c\x83\x00\x00\x00\x00IEND\xaeB`\x82"), nil
	}

	res, err := backend.Media(ctx, wire.MediaParams{Key: mediaID})
	if err != nil {
		t.Fatalf("Media failed: %v", err)
	}
	expectedName := mediaKeyToOpaque(mediaID) + ".png"
	if filepath.Base(res.Path) != expectedName {
		t.Errorf("expected opaque filename %q, got %q", expectedName, filepath.Base(res.Path))
	}

	// 3. View-once media must be refused for caching
	voID := "vo-media-id"
	voMsg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			ViewOnce: proto.Bool(true),
			Mimetype: proto.String("image/jpeg"),
		},
	}
	backend.mu.Lock()
	backend.rawMsgs[voID] = voMsg
	backend.mu.Unlock()

	_, err = backend.Media(ctx, wire.MediaParams{Key: voID})
	if err == nil || !strings.Contains(err.Error(), "view-once") {
		t.Fatalf("expected view-once refusal error, got %v", err)
	}
}

func TestMessagesCursorTieBreaker(t *testing.T) {
	backend, _, _ := setupTestBackend(t)
	ctx := context.Background()
	chatID := "test-cursor-chat@s.whatsapp.net"

	// Append 3 messages with the exact same timestamp
	ts := int64(5000)
	backend.appendMessage(wire.Message{ID: "msg-a", ConversationID: chatID, Timestamp: ts, Text: "A"}, nil)
	backend.appendMessage(wire.Message{ID: "msg-b", ConversationID: chatID, Timestamp: ts, Text: "B"}, nil)
	backend.appendMessage(wire.Message{ID: "msg-c", ConversationID: chatID, Timestamp: ts, Text: "C"}, nil)

	// Fetch without cursor
	all, err := backend.Messages(ctx, wire.MessagesParams{ConversationID: chatID, Count: 10})
	if err != nil || len(all.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d (err: %v)", len(all.Messages), err)
	}

	// Fetch with cursor at msg-c
	page, err := backend.Messages(ctx, wire.MessagesParams{
		ConversationID: chatID,
		Count:          10,
		CursorTime:     ts,
		CursorID:       "msg-c",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("expected 2 messages before msg-c, got %d", len(page.Messages))
	}
	if page.Messages[0].ID != "msg-a" || page.Messages[1].ID != "msg-b" {
		t.Errorf("unexpected messages: %+v", page.Messages)
	}
}

func TestReplyToIDAndJIDValidation(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	ctx := context.Background()
	backend.SetClient(mock, true)
	_ = backend.Start(ctx)
	backend.setState(wire.StateConnected, "")

	chatJID := "15551112222@s.whatsapp.net"

	// 1. Invalid recipient server must error
	_, err := backend.Send(ctx, wire.SendParams{
		ConversationID: "status@broadcast",
		Text:           "test",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported recipient server") {
		t.Fatalf("expected unsupported server error, got %v", err)
	}

	// 2. Non-existent ReplyToID must return descriptive error
	_, err = backend.Send(ctx, wire.SendParams{
		ConversationID: chatJID,
		Text:           "test reply",
		ReplyToID:      "nonexistent-msg",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected reply not found error, got %v", err)
	}

	// 3. Valid ReplyToID populates ExtendedTextMessage with ContextInfo
	quotedRaw := &waE2E.Message{Conversation: proto.String("Original question")}
	backend.appendMessage(wire.Message{
		ID:             "orig-id-1",
		ConversationID: chatJID,
		SenderID:       "15551112222@s.whatsapp.net",
		Text:           "Original question",
		Timestamp:      1000,
	}, quotedRaw)

	var sentWaMsg *waE2E.Message
	mock.SendMessageFunc = func(ctx context.Context, to types.JID, message *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
		sentWaMsg = message
		return whatsmeow.SendResponse{ID: "reply-resp-1", Timestamp: time.Now()}, nil
	}

	msg, err := backend.Send(ctx, wire.SendParams{
		ConversationID: chatJID,
		Text:           "Answering your question",
		ReplyToID:      "orig-id-1",
	})
	if err != nil {
		t.Fatalf("Send reply failed: %v", err)
	}
	if msg.ReplyToID != "orig-id-1" {
		t.Errorf("expected ReplyToID orig-id-1, got %q", msg.ReplyToID)
	}
	if sentWaMsg == nil || sentWaMsg.ExtendedTextMessage == nil || sentWaMsg.ExtendedTextMessage.ContextInfo == nil {
		t.Fatalf("expected ExtendedTextMessage ContextInfo, got %+v", sentWaMsg)
	}
	if sentWaMsg.ExtendedTextMessage.ContextInfo.GetStanzaID() != "orig-id-1" {
		t.Errorf("expected StanzaID orig-id-1, got %q", sentWaMsg.ExtendedTextMessage.ContextInfo.GetStanzaID())
	}
}

func TestGroupReadReceiptSenderJID(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	ctx := context.Background()
	backend.SetClient(mock, true)
	mock.SetConnected(true)
	_ = backend.Start(ctx)
	backend.setState(wire.StateConnected, "")

	groupJID := "12345678-group@g.us"
	memberJID := "15559990000@s.whatsapp.net"

	backend.appendMessage(wire.Message{
		ID:             "grp-msg-1",
		ConversationID: groupJID,
		SenderID:       memberJID,
		Text:           "Hello group",
		Timestamp:      2000,
	}, nil)

	var calledChat, calledSender types.JID
	mock.MarkReadFunc = func(ctx context.Context, ids []types.MessageID, timestamp time.Time, chat, sender types.JID, receiptTypeExtra ...types.ReceiptType) error {
		calledChat = chat
		calledSender = sender
		return nil
	}

	err := backend.MarkRead(ctx, wire.MarkReadParams{
		ConversationID: groupJID,
		MessageID:      "grp-msg-1",
	})
	if err != nil {
		t.Fatalf("MarkRead failed: %v", err)
	}
	if calledChat.String() != groupJID {
		t.Errorf("expected chat JID %q, got %q", groupJID, calledChat.String())
	}
	if calledSender.String() != memberJID {
		t.Errorf("expected sender JID %q, got %q", memberJID, calledSender.String())
	}
}

func TestIgnoreUnsupportedControlMessages(t *testing.T) {
	backend, _, _ := setupTestBackend(t)

	// Control message with ProtocolMessage should be ignored
	evt := &events.Message{
		Info: types.MessageInfo{
			ID:   "ctrl-1",
			Chat: types.NewJID("15551234567", types.DefaultUserServer),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Key: &waCommon.MessageKey{ID: proto.String("revoked-id")},
			},
		},
	}

	backend.handleEvent(evt)

	convs := backend.Conversations(10)
	if len(convs) != 0 {
		t.Errorf("control message must not create conversation bubbles, got %+v", convs)
	}
}

func TestEnsureSQLiteFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	if err := ensureSQLiteFile(path); err != nil {
		t.Fatalf("ensureSQLiteFile: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("expected 0600 permissions, got %v", fi.Mode().Perm())
	}
}
