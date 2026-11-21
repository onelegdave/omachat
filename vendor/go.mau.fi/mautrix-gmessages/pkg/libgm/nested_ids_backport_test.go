package libgm

// OmaChat: regression tests for the upstream b580a1a (#166) nested-ID backport.

import (
	"testing"

	"github.com/rs/zerolog"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/crypto"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
	"google.golang.org/protobuf/proto"
)

func nestedConversation(id, latest string) *gmproto.Conversation {
	return &gmproto.Conversation{
		Name:                  "Conversation " + id,
		SomeKindOfGroupID:     &gmproto.SomeKindOfGroupID{ConversationID: id},
		LatestMessageAnoherID: &gmproto.AnotherMessageID{MessageID: latest},
	}
}

func nestedMessage(id, conv string) *gmproto.Message {
	return &gmproto.Message{
		AnotherMessageID:  &gmproto.AnotherMessageID{MessageID: id},
		SomeKindOfGroupID: &gmproto.SomeKindOfGroupID{ConversationID: conv},
	}
}

// decryptDataEvent runs a response through the real decrypt path, as the
// long-poll handler does, and returns the decoded response.
func decryptDataEvent(t *testing.T, action gmproto.ActionType, response proto.Message) proto.Message {
	t.Helper()
	cli := &Client{Logger: zerolog.Nop(), AuthData: &AuthData{RequestCrypto: crypto.NewAESCTRHelper()}}
	plaintext, err := proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cli.AuthData.RequestCrypto.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	messageData, err := proto.Marshal(&gmproto.RPCMessageData{Action: action, EncryptedData: encrypted})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := cli.decryptInternalMessage(&gmproto.IncomingRPCMessage{
		BugleRoute:  gmproto.BugleRoute_DataEvent,
		MessageData: messageData,
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.DecryptedMessage == nil {
		t.Fatalf("action %s decoded no response", action)
	}
	return msg.DecryptedMessage
}

func TestNestedConversationIDsFillListConversations(t *testing.T) {
	out := decryptDataEvent(t, gmproto.ActionType_LIST_CONVERSATIONS, &gmproto.ListConversationsResponse{
		Conversations: []*gmproto.Conversation{
			nestedConversation("conv-1", "msg-1"),
			nestedConversation("conv-2", "msg-2"),
		},
	}).(*gmproto.ListConversationsResponse)
	convs := out.GetConversations()
	if len(convs) != 2 {
		t.Fatalf("got %d conversations, want 2", len(convs))
	}
	for i, want := range []string{"conv-1", "conv-2"} {
		if got := convs[i].GetConversationID(); got != want {
			t.Errorf("conversation %d ID = %q, want %q", i, got, want)
		}
	}
	if got := convs[1].GetLatestMessageID(); got != "msg-2" {
		t.Errorf("latest message ID = %q, want msg-2", got)
	}
}

func TestNestedIDsFillUpdatesAndSingleConversations(t *testing.T) {
	updates := decryptDataEvent(t, gmproto.ActionType_GET_UPDATES, &gmproto.UpdateEvents{
		Event: &gmproto.UpdateEvents_ConversationEvent{ConversationEvent: &gmproto.ConversationEvent{
			Data: []*gmproto.Conversation{nestedConversation("conv-3", "msg-3")},
		}},
	}).(*gmproto.UpdateEvents)
	if got := updates.GetConversationEvent().GetData()[0].GetConversationID(); got != "conv-3" {
		t.Errorf("conversation update ID = %q, want conv-3", got)
	}

	messageUpdates := decryptDataEvent(t, gmproto.ActionType_GET_UPDATES, &gmproto.UpdateEvents{
		Event: &gmproto.UpdateEvents_MessageEvent{MessageEvent: &gmproto.MessageEvent{
			Data: []*gmproto.Message{nestedMessage("msg-4", "conv-4")},
		}},
	}).(*gmproto.UpdateEvents)
	updated := messageUpdates.GetMessageEvent().GetData()[0]
	if updated.GetMessageID() != "msg-4" || updated.GetConversationID() != "conv-4" {
		t.Errorf("message update IDs = %q/%q, want msg-4/conv-4", updated.GetMessageID(), updated.GetConversationID())
	}

	single := decryptDataEvent(t, gmproto.ActionType_GET_CONVERSATION, &gmproto.GetConversationResponse{
		Conversation: nestedConversation("conv-5", "msg-5"),
	}).(*gmproto.GetConversationResponse)
	if got := single.GetConversation().GetConversationID(); got != "conv-5" {
		t.Errorf("single conversation ID = %q, want conv-5", got)
	}

	created := decryptDataEvent(t, gmproto.ActionType_GET_OR_CREATE_CONVERSATION, &gmproto.GetOrCreateConversationResponse{
		Conversation: nestedConversation("conv-6", "msg-6"),
	}).(*gmproto.GetOrCreateConversationResponse)
	if got := created.GetConversation().GetConversationID(); got != "conv-6" {
		t.Errorf("created conversation ID = %q, want conv-6", got)
	}
}

func TestNestedMessageIDsFillListMessages(t *testing.T) {
	out := decryptDataEvent(t, gmproto.ActionType_LIST_MESSAGES, &gmproto.ListMessagesResponse{
		Messages: []*gmproto.Message{nestedMessage("msg-7", "conv-7")},
	}).(*gmproto.ListMessagesResponse)
	msg := out.GetMessages()[0]
	if msg.GetMessageID() != "msg-7" || msg.GetConversationID() != "conv-7" {
		t.Errorf("message IDs = %q/%q, want msg-7/conv-7", msg.GetMessageID(), msg.GetConversationID())
	}
}

func TestNestedIDsNeverReplacePresentIDs(t *testing.T) {
	conv := nestedConversation("nested-conv", "nested-latest")
	conv.ConversationID = "top-conv"
	conv.LatestMessageID = "top-latest"
	msg := nestedMessage("nested-msg", "nested-conv")
	msg.MessageID = "top-msg"
	msg.ConversationID = "top-conv"
	fillMissingIDs(&gmproto.ListConversationsResponse{Conversations: []*gmproto.Conversation{conv, nil}})
	fillMissingIDs(&gmproto.ListMessagesResponse{Messages: []*gmproto.Message{msg, nil}})
	if conv.ConversationID != "top-conv" || conv.LatestMessageID != "top-latest" {
		t.Errorf("conversation IDs changed to %q/%q", conv.ConversationID, conv.LatestMessageID)
	}
	if msg.MessageID != "top-msg" || msg.ConversationID != "top-conv" {
		t.Errorf("message IDs changed to %q/%q", msg.MessageID, msg.ConversationID)
	}

	// Without a nested ID the field stays empty instead of inventing one.
	bare := &gmproto.Conversation{}
	fillMissingConversationIDs(bare)
	fillMissingMessageIDs(nil)
	if bare.ConversationID != "" || bare.LatestMessageID != "" {
		t.Errorf("bare conversation got IDs %q/%q", bare.ConversationID, bare.LatestMessageID)
	}
}
