package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

var dummyWebPBytes = []byte("RIFF\x1a\x00\x00\x00WEBPVP8 \x0e\x00\x00\x00\x30\x01\x00\x9d\x01\x2a\x01\x00\x01\x00\x02\x00\x34\x25")

func TestConvertEventMessageSticker(t *testing.T) {
	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")
	senderJID, _ := types.ParseJID("15551234567@s.whatsapp.net")
	msgID := "sticker-evt-1"

	evt := &events.Message{
		Info: types.MessageInfo{
			ID:        msgID,
			Chat:      chatJID,
			Sender:    senderJID,
			Timestamp: time.Unix(1700000000, 0),
			PushName:  "Sticker Sender",
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				URL:           proto.String("https://mmg.whatsapp.net/d/f/sticker1.enc"),
				DirectPath:    proto.String("/v/t62.7118-24/sticker1"),
				Mimetype:      proto.String("image/webp"),
				FileSHA256:    []byte("01234567890123456789012345678901"),
				FileEncSHA256: []byte("abcdefabcdefabcdefabcdefabcdefab"),
				MediaKey:      []byte("11112222333344445555666677778888"),
				FileLength:    proto.Uint64(34567),
				Width:         proto.Uint32(512),
				Height:        proto.Uint32(512),
			},
		},
	}

	msg, displayable := convertEventMessage(evt)
	if !displayable {
		t.Fatal("expected sticker message to be displayable")
	}

	if msg.ID != msgID {
		t.Errorf("expected ID %q, got %q", msgID, msg.ID)
	}
	if msg.ConversationID != chatJID.String() {
		t.Errorf("expected ConversationID %q, got %q", chatJID.String(), msg.ConversationID)
	}
	if msg.Text != "" {
		t.Errorf("expected empty text for sticker, got %q", msg.Text)
	}
	if msg.FromMe {
		t.Error("expected FromMe to be false for incoming sticker")
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}

	att := msg.Attachments[0]
	expectedKey := rawMediaKey(chatJID.String(), msgID)
	if att.Key != expectedKey {
		t.Errorf("expected attachment Key %q, got %q", expectedKey, att.Key)
	}
	if att.MediaID != msgID {
		t.Errorf("expected attachment MediaID %q, got %q", msgID, att.MediaID)
	}
	if att.MimeType != "image/webp" {
		t.Errorf("expected MimeType %q, got %q", "image/webp", att.MimeType)
	}
	if att.Size != 34567 {
		t.Errorf("expected Size 34567, got %d", att.Size)
	}
	if att.Width != 512 {
		t.Errorf("expected Width 512, got %d", att.Width)
	}
	if att.Height != 512 {
		t.Errorf("expected Height 512, got %d", att.Height)
	}
	if !att.IsImage {
		t.Error("expected IsImage to be true")
	}
	if att.IsAudio || att.IsVideo || att.IsGif {
		t.Errorf("expected audio/video/gif to be false, got audio=%v video=%v gif=%v", att.IsAudio, att.IsVideo, att.IsGif)
	}
}

func TestConvertEventMessageStickerDefaultMime(t *testing.T) {
	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")
	evt := &events.Message{
		Info: types.MessageInfo{
			ID:        "sticker-evt-nomime",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				FileLength: proto.Uint64(12345),
			},
		},
	}

	msg, displayable := convertEventMessage(evt)
	if !displayable {
		t.Fatal("expected sticker message to be displayable")
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].MimeType != "image/webp" {
		t.Errorf("expected default MimeType image/webp, got %q", msg.Attachments[0].MimeType)
	}
}

func TestConvertEventMessageStickerViewOnceFiltered(t *testing.T) {
	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")

	// Case 1: evt.IsViewOnce flag set
	evt1 := &events.Message{
		Info: types.MessageInfo{
			ID:        "sticker-vo-1",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(5000),
			},
		},
		IsViewOnce: true,
	}

	msg1, displayable1 := convertEventMessage(evt1)
	if !displayable1 {
		t.Fatal("expected view-once placeholder message to be displayable")
	}
	if msg1.Text != lifetimePlaceholder() {
		t.Errorf("expected lifetime placeholder, got %q", msg1.Text)
	}
	if len(msg1.Attachments) != 0 {
		t.Fatalf("view-once sticker must not expose attachments, got %d", len(msg1.Attachments))
	}

	// Case 2: wrapped in ViewOnceMessage envelope
	evt2 := &events.Message{
		Info: types.MessageInfo{
			ID:        "sticker-vo-2",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			ViewOnceMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					StickerMessage: &waE2E.StickerMessage{
						Mimetype:   proto.String("image/webp"),
						FileLength: proto.Uint64(5000),
					},
				},
			},
		},
	}

	msg2, displayable2 := convertEventMessage(evt2)
	if !displayable2 {
		t.Fatal("expected view-once placeholder message to be displayable")
	}
	if msg2.Text != lifetimePlaceholder() {
		t.Errorf("expected lifetime placeholder, got %q", msg2.Text)
	}
	if len(msg2.Attachments) != 0 {
		t.Fatalf("view-once sticker must not expose attachments, got %d", len(msg2.Attachments))
	}
}

func TestConvertEventMessageStickerEphemeralFiltered(t *testing.T) {
	chatJID, _ := types.ParseJID("15551234567@s.whatsapp.net")

	// Case 1: evt.IsEphemeral flag set
	evt1 := &events.Message{
		Info: types.MessageInfo{
			ID:        "sticker-eph-1",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(5000),
			},
		},
		IsEphemeral: true,
	}

	msg1, displayable1 := convertEventMessage(evt1)
	if !displayable1 {
		t.Fatal("expected ephemeral placeholder message to be displayable")
	}
	if msg1.Text != lifetimePlaceholder() {
		t.Errorf("expected lifetime placeholder, got %q", msg1.Text)
	}
	if len(msg1.Attachments) != 0 {
		t.Fatalf("ephemeral sticker must not expose attachments, got %d", len(msg1.Attachments))
	}

	// Case 2: wrapped in EphemeralMessage envelope
	evt2 := &events.Message{
		Info: types.MessageInfo{
			ID:        "sticker-eph-2",
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			EphemeralMessage: &waE2E.FutureProofMessage{
				Message: &waE2E.Message{
					StickerMessage: &waE2E.StickerMessage{
						Mimetype:   proto.String("image/webp"),
						FileLength: proto.Uint64(5000),
					},
				},
			},
		},
	}

	msg2, displayable2 := convertEventMessage(evt2)
	if !displayable2 {
		t.Fatal("expected ephemeral placeholder message to be displayable")
	}
	if msg2.Text != lifetimePlaceholder() {
		t.Errorf("expected lifetime placeholder, got %q", msg2.Text)
	}
	if len(msg2.Attachments) != 0 {
		t.Fatalf("ephemeral sticker must not expose attachments, got %d", len(msg2.Attachments))
	}
}

func TestStickerDownloadAndCache(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	downloadCalled := 0
	mock.DownloadToFileFunc = func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		downloadCalled++
		_, ok := msg.(*waE2E.StickerMessage)
		if !ok {
			t.Errorf("expected *waE2E.StickerMessage, got %T", msg)
		}
		_, err := file.Write(dummyWebPBytes)
		return err
	}

	chatJID, _ := types.ParseJID("15559876543@s.whatsapp.net")
	msgID := "sticker-dl-1"
	mock.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID:        msgID,
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(uint64(len(dummyWebPBytes))),
			},
		},
	})

	// Verify conversation preview is "Attachment"
	convs := backend.Conversations(10)
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}
	if convs[0].Preview != "Attachment" {
		t.Errorf("expected preview %q, got %q", "Attachment", convs[0].Preview)
	}

	// First call downloads media
	key := rawMediaKey(chatJID.String(), msgID)
	res, err := backend.Media(ctx, wire.MediaParams{Key: key})
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	if res.Key != key {
		t.Errorf("expected key %q, got %q", key, res.Key)
	}
	if res.Path == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(res.Path, ".webp") {
		t.Errorf("expected .webp extension, got %s", res.Path)
	}
	if filepath.Dir(res.Path) != backend.paths.WhatsAppMediaDir() {
		t.Errorf("expected path in WhatsAppMediaDir, got %s", res.Path)
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("cached file does not exist: %v", err)
	}
	if downloadCalled != 1 {
		t.Errorf("expected 1 download call, got %d", downloadCalled)
	}

	// Second call uses cached file immediately without re-downloading
	res2, err := backend.Media(ctx, wire.MediaParams{Key: key})
	if err != nil {
		t.Fatalf("Media (cached): %v", err)
	}
	if res2.Path != res.Path {
		t.Errorf("expected same path %q, got %q", res.Path, res2.Path)
	}
	if downloadCalled != 1 {
		t.Errorf("download should not be called again when cached; called %d times", downloadCalled)
	}
}

func TestStickerDownloadFallbackExtension(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Write un-sniffable bytes (not RIFF/WEBP, not PNG/JPEG)
	mock.DownloadToFileFunc = func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		_, err := file.Write([]byte("arbitrary-binary-sticker-data-without-riff-header"))
		return err
	}

	chatJID, _ := types.ParseJID("15559876543@s.whatsapp.net")
	msgID := "sticker-fallback-ext"
	mock.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID:        msgID,
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(100),
			},
		},
	})

	key := rawMediaKey(chatJID.String(), msgID)
	res, err := backend.Media(ctx, wire.MediaParams{Key: key})
	if err != nil {
		t.Fatalf("Media: %v", err)
	}
	if !strings.HasSuffix(res.Path, ".webp") {
		t.Errorf("expected .webp extension from declared mimetype fallback, got %s", res.Path)
	}
}

func TestStickerSurvivesRestartAndDownloads(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)
	ctx := context.Background()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15553334444@s.whatsapp.net")
	msgID := "sticker-persist-1"

	mock.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID:        msgID,
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(uint64(len(dummyWebPBytes))),
				Width:      proto.Uint32(512),
				Height:     proto.Uint32(512),
			},
		},
	})

	// Stop the first backend instance without downloading media yet
	backend.Stop()

	// Start a second backend instance using the same paths
	backend2 := New(backend.log, backend.paths, backend.publish)
	mock2 := NewMockClient()
	mock2.DownloadToFileFunc = func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		_, ok := msg.(*waE2E.StickerMessage)
		if !ok {
			t.Errorf("expected *waE2E.StickerMessage, got %T", msg)
		}
		_, err := file.Write(dummyWebPBytes)
		return err
	}
	backend2.SetClient(mock2, true)
	if err := backend2.Start(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify messages survived restart
	msgs, err := backend2.Messages(ctx, wire.MessagesParams{ConversationID: chatJID.String(), Count: 10})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs.Messages) != 1 {
		t.Fatalf("expected 1 message after restart, got %d", len(msgs.Messages))
	}
	m := msgs.Messages[0]
	if m.ID != msgID {
		t.Errorf("expected message ID %q, got %q", msgID, m.ID)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("expected 1 attachment after restart, got %d", len(m.Attachments))
	}
	if !m.Attachments[0].IsImage {
		t.Error("expected attachment IsImage to be true after restart")
	}
	if m.Attachments[0].MimeType != "image/webp" {
		t.Errorf("expected MimeType image/webp after restart, got %q", m.Attachments[0].MimeType)
	}

	// Download media through restarted backend (proves raw payload was persisted and restored)
	res, err := backend2.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatJID.String(), msgID)})
	if err != nil {
		t.Fatalf("Media after restart: %v", err)
	}
	if res.Path == "" {
		t.Fatal("expected non-empty path after restart")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("downloaded file does not exist after restart: %v", err)
	}
}

func TestViewOnceStickerNotPersistedAcrossRestart(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)
	ctx := context.Background()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	chatJID, _ := types.ParseJID("15553334444@s.whatsapp.net")
	msgID := "vo-sticker-1"

	mock.TriggerEvent(&events.Message{
		Info: types.MessageInfo{
			ID:        msgID,
			Chat:      chatJID,
			Sender:    chatJID,
			Timestamp: time.Now(),
		},
		Message: &waE2E.Message{
			StickerMessage: &waE2E.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				FileLength: proto.Uint64(1234),
			},
		},
		IsViewOnce: true,
	})

	backend.Stop()

	backend2 := New(backend.log, backend.paths, backend.publish)
	mock2 := NewMockClient()
	backend2.SetClient(mock2, true)
	if err := backend2.Start(ctx); err != nil {
		t.Fatal(err)
	}

	msgs, err := backend2.Messages(ctx, wire.MessagesParams{ConversationID: chatJID.String(), Count: 10})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs.Messages))
	}
	if msgs.Messages[0].Text != lifetimePlaceholder() {
		t.Errorf("expected lifetime placeholder, got %q", msgs.Messages[0].Text)
	}
	if len(msgs.Messages[0].Attachments) != 0 {
		t.Fatalf("expected 0 attachments for view-once sticker, got %d", len(msgs.Messages[0].Attachments))
	}

	_, err = backend2.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatJID.String(), msgID)})
	if err == nil || (!strings.Contains(err.Error(), "unauthorized") && !strings.Contains(err.Error(), "view-once")) {
		t.Fatalf("view-once sticker must not be downloadable after restart, got err: %v", err)
	}
}

func TestStickerHistorySync(t *testing.T) {
	backend, mock, _ := setupTestBackend(t)
	backend.SetClient(mock, true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := backend.Start(ctx); err != nil {
		t.Fatal(err)
	}

	mock.DownloadToFileFunc = func(ctx context.Context, msg whatsmeow.DownloadableMessage, file whatsmeow.File) error {
		_, err := file.Write(dummyWebPBytes)
		return err
	}

	chatStr := "15556667777@s.whatsapp.net"
	chatName := "Sticker History Friend"
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
									ID: proto.String("hist-sticker-1"),
								},
								MessageTimestamp: proto.Uint64(1700000000),
								Message: &waE2E.Message{
									StickerMessage: &waE2E.StickerMessage{
										Mimetype:   proto.String("image/webp"),
										FileLength: proto.Uint64(uint64(len(dummyWebPBytes))),
										Width:      proto.Uint32(512),
										Height:     proto.Uint32(512),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	mock.TriggerEvent(syncEvt)

	convs := backend.Conversations(10)
	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation from history sync, got %d", len(convs))
	}
	if convs[0].Preview != "Attachment" {
		t.Errorf("expected preview %q, got %q", "Attachment", convs[0].Preview)
	}

	msgs, err := backend.Messages(ctx, wire.MessagesParams{ConversationID: chatStr, Count: 10})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(msgs.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs.Messages))
	}
	if len(msgs.Messages[0].Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msgs.Messages[0].Attachments))
	}
	att := msgs.Messages[0].Attachments[0]
	if !att.IsImage || att.MimeType != "image/webp" {
		t.Errorf("unexpected attachment properties: %+v", att)
	}

	// Verify the sticker payload from history sync can be downloaded
	res, err := backend.Media(ctx, wire.MediaParams{Key: rawMediaKey(chatStr, "hist-sticker-1")})
	if err != nil {
		t.Fatalf("Media for history sync sticker: %v", err)
	}
	if res.Path == "" {
		t.Fatal("expected non-empty path for history sync sticker")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("cached file does not exist: %v", err)
	}
}
