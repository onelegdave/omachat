package messenger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	waTypes "go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func threadSnapshot(b *Backend, activity, read int64, preview string) {
	b.handleTable(&table.LSTable{LSUpdateOrInsertThread: []*table.LSUpdateOrInsertThread{{
		ThreadKey: 7, ThreadName: "Synthetic thread", Snippet: preview,
		LastActivityTimestampMs: activity, LastReadWatermarkTimestampMs: read,
	}}})
}

func TestMessengerReadAcknowledgmentPreservesNewerActivityAndFailures(t *testing.T) {
	paths := setupMessengerPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	threadSnapshot(b, 3000, 0, "unread before request")
	before, err := os.ReadFile(paths.MessengerStoreFile())
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("synthetic transport failure")
	err = b.markReadThrough(context.Background(), 7, 3000, func(_ context.Context, task *socket.ThreadMarkReadTask) error {
		if task.ThreadId != 7 || task.LastReadWatermarkTs != 3000 {
			t.Fatal("incorrect read request")
		}
		return failure
	})
	if !errors.Is(err, failure) || !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
		t.Fatal("failed acknowledgment changed read state")
	}
	after, err := os.ReadFile(paths.MessengerStoreFile())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed acknowledgment changed cache")
	}
	err = b.markReadThrough(context.Background(), 7, 3000, func(_ context.Context, _ *socket.ThreadMarkReadTask) error {
		threadSnapshot(b, 4000, 0, "arrived while acknowledgment was pending")
		return nil
	})
	if err != nil || !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
		t.Fatal("acknowledgment cleared activity newer than its watermark")
	}
	if err := b.markReadThrough(context.Background(), 7, 4000, func(context.Context, *socket.ThreadMarkReadTask) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if b.Conversations(1)[0].Unread || b.Status().Unread != 0 {
		t.Fatal("covering acknowledgment did not mark thread read")
	}
}

func encryptedReadFixture(b *Backend, timestamp int64, fromMe bool, id string) {
	event := &events.FBMessage{}
	event.Info.Chat = waTypes.NewJID("7", waTypes.MessengerServer)
	event.Info.Sender = waTypes.NewJID("42", waTypes.MessengerServer)
	event.Info.ID = id
	event.Info.Timestamp = time.UnixMilli(timestamp)
	event.Info.IsFromMe = fromMe
	// A missing supported body is intentionally represented by the existing
	// unsupported-message placeholder; this fixture concerns activity/read state.
	b.handleE2EEEvent(event)
}

func TestMessengerEncryptedReplaysDoNotResurfaceReadOrHideUnread(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	threadSnapshot(b, 3000, 3000, "latest read activity")
	encryptedReadFixture(b, 1000, false, "old-encrypted")
	if conv := b.Conversations(1)[0]; conv.Unread || conv.Timestamp != messengerTimestamp(3000) || conv.Preview != "latest read activity" {
		t.Fatal("encrypted history rolled back or resurfaced read activity")
	}
	encryptedReadFixture(b, 4000, false, "new-encrypted")
	if !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
		t.Fatal("new encrypted activity stayed read")
	}
	encryptedReadFixture(b, 3000, true, "old-outgoing-sync")
	if !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
		t.Fatal("stale outgoing phone sync hid newer unread incoming activity")
	}
	b.handleTable(&table.LSTable{LSMarkThreadRead: []*table.LSMarkThreadRead{{ThreadKey: 7, LastReadWatermarkTimestampMs: 4000}}})
	encryptedReadFixture(b, 4000, false, "new-encrypted")
	if b.Conversations(1)[0].Unread || b.Status().Unread != 0 {
		t.Fatal("encrypted duplicate resurfaced after acknowledgment")
	}
	other := New(zerolog.Nop(), nil, nil)
	encryptedReadFixture(other, 1000, true, "first-outgoing")
	if other.Conversations(1)[0].Unread {
		t.Fatal("first outgoing encrypted message is unread")
	}
}

func TestMessengerOlderSnapshotsDoNotRollbackReadActivity(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	threadSnapshot(b, 3000, 3000, "latest")
	threadSnapshot(b, 1000, 0, "old snapshot")
	if conv := b.Conversations(1)[0]; conv.Timestamp != messengerTimestamp(3000) || conv.Preview != "latest" {
		t.Fatalf("older snapshot rolled back activity: %+v", conv)
	}
	threadSnapshot(b, 2000, 0, "older replay")
	if conv := b.Conversations(1)[0]; conv.Unread || b.Status().Unread != 0 {
		t.Fatal("older replay resurfaced read activity")
	}
	threadSnapshot(b, 4000, 0, "genuinely new")
	if conv := b.Conversations(1)[0]; !conv.Unread || b.Status().Unread != 1 {
		t.Fatal("new activity was hidden")
	}
}

func TestMessengerReadWatermarkFollowsStandInThreadMapping(t *testing.T) {
	paths := setupMessengerPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	jid := waTypes.NewJID("42", waTypes.MessengerServer)
	b.setMappingLocked(42, 42, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	b.convs["42"] = wire.Conversation{ID: "42", Timestamp: messengerTimestamp(3000)}
	b.messages["42"] = []wire.Message{{ID: "stand-in-message", ConversationID: "42", Timestamp: messengerTimestamp(3000)}}
	b.advanceReadWatermarkLocked(42, 5000)
	b.setMappingLocked(7, 42, table.ENCRYPTED_OVER_WA_ONE_TO_ONE)
	if b.readWatermarks[7] != 5000 {
		t.Fatal("read boundary was lost when the stand-in was folded")
	}
	if _, exists := b.readWatermarks[42]; exists {
		t.Fatal("retired stand-in retained its read boundary")
	}
	if b.threadToJID[7] != jid {
		t.Fatal("real thread mapping was not retained")
	}
	threadSnapshot(b, 4000, 0, "activity already covered by the read boundary")
	if b.Conversations(1)[0].Unread {
		t.Fatal("folded read activity resurfaced as unread")
	}
	restored := New(zerolog.Nop(), paths, nil)
	restored.mergeStoredMessengerDataLocked(loadStoredMessengerData(paths.MessengerStoreFile()))
	threadSnapshot(restored, 4500, 0, "covered activity after restart")
	if restored.Conversations(1)[0].Unread {
		t.Fatal("folded read boundary was not persisted")
	}
	threadSnapshot(restored, 6000, 0, "genuinely newer activity")
	if !restored.Conversations(1)[0].Unread {
		t.Fatal("folded read boundary hid newer activity")
	}
}

func TestMessengerStaleReadEventsCannotClearNewerActivity(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		b := New(zerolog.Nop(), nil, nil)
		threadSnapshot(b, 3000, 2000, "unread")
		read := func(watermark int64) {
			if v2 {
				b.handleTable(&table.LSTable{LSMarkThreadReadV2: []*table.LSMarkThreadReadV2{{ThreadKey: 7, LastReadWatermarkTimestampMs: watermark}}})
			} else {
				b.handleTable(&table.LSTable{LSMarkThreadRead: []*table.LSMarkThreadRead{{ThreadKey: 7, LastReadWatermarkTimestampMs: watermark}}})
			}
		}
		read(2000)
		if !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
			t.Fatalf("V2=%v: old read event hid newer activity", v2)
		}
		read(3000)
		if b.Conversations(1)[0].Unread || b.Status().Unread != 0 {
			t.Fatalf("V2=%v: covering read event did not clear activity", v2)
		}
		// A stale refresh must not hide newer incoming activity either.
		threadSnapshot(b, 4000, 0, "new unread")
		threadSnapshot(b, 1000, 1000, "old read snapshot")
		if !b.Conversations(1)[0].Unread || b.Status().Unread != 1 {
			t.Fatalf("V2=%v: old read snapshot hid newer activity", v2)
		}
	}
}

func TestMessengerReadWatermarkCoversLaterArrivalAndSurvivesRestart(t *testing.T) {
	paths := setupMessengerPaths(t)
	b := New(zerolog.Nop(), paths, nil)
	threadSnapshot(b, 2000, 5000, "already read")
	restarted := New(zerolog.Nop(), paths, nil)
	restarted.mu.Lock()
	restarted.mergeStoredMessengerDataLocked(loadStoredMessengerData(paths.MessengerStoreFile()))
	restarted.mu.Unlock()
	threadSnapshot(restarted, 4000, 0, "late arrival already covered by read")
	if restarted.Conversations(1)[0].Unread || restarted.Status().Unread != 0 {
		t.Fatal("restart lost read watermark and resurfaced covered activity")
	}
	threadSnapshot(restarted, 6000, 0, "new unread after watermark")
	if !restarted.Conversations(1)[0].Unread || restarted.Status().Unread != 1 {
		t.Fatal("persisted watermark hid genuinely new activity")
	}
}

func TestMessengerReadBeforeThreadAndCountBasedSnapshots(t *testing.T) {
	b := New(zerolog.Nop(), nil, nil)
	b.handleTable(&table.LSTable{LSMarkThreadRead: []*table.LSMarkThreadRead{{ThreadKey: 7, LastReadWatermarkTimestampMs: 3000}}})
	snapshot := func(activity int64, count int64, preview string) {
		b.handleTable(&table.LSTable{LSDeleteThenInsertThread: []*table.LSDeleteThenInsertThread{{ThreadKey: 7,
			LastActivityTimestampMs: activity, UnreadMessageCount: count, Snippet: preview}}})
	}
	snapshot(2000, 1, "covered arrival")
	if b.Conversations(1)[0].Unread {
		t.Fatal("read event preceding the thread was lost")
	}
	snapshot(4000, 1, "new unread")
	snapshot(1000, 0, "older count-based row")
	if conv := b.Conversations(1)[0]; !conv.Unread || conv.Preview != "new unread" || conv.Timestamp != messengerTimestamp(4000) {
		t.Fatal("older count-based snapshot hid or rolled back unread activity")
	}
}
