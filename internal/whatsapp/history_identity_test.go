package whatsapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/onelegdave/omachat/internal/wire"
)

const syntheticSavedContactName = "Local Address Book Name"
const syntheticWhatsAppPushName = "WhatsApp Profile Name"

var syntheticPNJID = types.JID{User: "111222333", Server: types.DefaultUserServer}
var syntheticLIDJID = types.JID{User: "444555666", Server: types.HiddenUserServer}

type historyIdentityContactStore struct {
	waStore.ContactStore
	contacts map[types.JID]types.ContactInfo
}

func (s historyIdentityContactStore) GetContact(_ context.Context, jid types.JID) (types.ContactInfo, error) {
	if contact, ok := s.contacts[jid]; ok {
		return contact, nil
	}
	return types.ContactInfo{}, errors.New("synthetic contact not found")
}

type historyIdentityLIDStore struct {
	waStore.LIDStore
	pnByLID map[types.JID]types.JID
	lidByPN map[types.JID]types.JID
}

func (s historyIdentityLIDStore) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := s.pnByLID[lid]; ok {
		return pn, nil
	}
	return types.EmptyJID, errors.New("synthetic PN mapping not found")
}

func (s historyIdentityLIDStore) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	if lid, ok := s.lidByPN[pn]; ok {
		return lid, nil
	}
	return types.EmptyJID, errors.New("synthetic LID mapping not found")
}

func syntheticHistoryDevice() *waStore.Device {
	return &waStore.Device{
		Contacts: historyIdentityContactStore{contacts: map[types.JID]types.ContactInfo{
			syntheticPNJID: {
				FullName: syntheticSavedContactName,
				PushName: syntheticWhatsAppPushName,
			},
		}},
		LIDs: historyIdentityLIDStore{
			pnByLID: map[types.JID]types.JID{syntheticLIDJID: syntheticPNJID},
			lidByPN: map[types.JID]types.JID{syntheticPNJID: syntheticLIDJID},
		},
	}
}

func seedNamedConversation(t *testing.T, backend *Backend, jid types.JID, name string) uint64 {
	t.Helper()
	id := jid.String()
	backend.mu.Lock()
	backend.convs[id] = wire.Conversation{ID: id, Name: name, Initials: initials(name)}
	backend.order = []string{id}
	gen := backend.gen
	backend.mu.Unlock()
	return gen
}

func TestHistorySyncUsesSavedContactNameBeforeHistoryName(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectLIDChat)
	backend.device = syntheticHistoryDevice()
	gen := seedNamedConversation(t, backend, syntheticLIDJID, syntheticWhatsAppPushName)
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID:     proto.String(syntheticDirectLIDChat),
		PnJID:  proto.String(syntheticDirectPNChat),
		LidJID: proto.String(syntheticDirectLIDChat),
		Name:   proto.String(syntheticWhatsAppPushName),
	}}}
	if err := backend.ingestHistorySyncMode(gen, data, false); err != nil {
		t.Fatal(err)
	}
	if got := backend.convs[syntheticDirectLIDChat].Name; got != syntheticSavedContactName {
		t.Fatalf("history name %q overrode saved contact name %q", got, syntheticSavedContactName)
	}
}

func TestRefreshConversationNamesOverridesPushNameWithSavedContactName(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectLIDChat)
	device := syntheticHistoryDevice()
	backend.device = device
	_ = seedNamedConversation(t, backend, syntheticLIDJID, syntheticWhatsAppPushName)

	backend.refreshConversationNames(device)
	if got := backend.convs[syntheticDirectLIDChat].Name; got != syntheticSavedContactName {
		t.Fatalf("startup refresh kept push name %q instead of saved name %q", got, syntheticSavedContactName)
	}
}

func TestContactEventOverridesPushNameWithSavedContactName(t *testing.T) {
	backend, mock, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectLIDChat)
	backend.device = syntheticHistoryDevice()
	_ = seedNamedConversation(t, backend, syntheticLIDJID, syntheticWhatsAppPushName)

	mock.TriggerEvent(&events.Contact{
		JID: syntheticLIDJID,
		Action: &waSyncAction.ContactAction{
			FullName: proto.String(syntheticSavedContactName),
		},
	})
	if got := backend.convs[syntheticDirectLIDChat].Name; got != syntheticSavedContactName {
		t.Fatalf("contact sync kept push name %q instead of saved name %q", got, syntheticSavedContactName)
	}
}

func TestCommitFromMeDoesNotUseOwnerPushNameAsConversationName(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectPNChat)
	backend.device = syntheticHistoryDevice()
	backend.commitMessage(backend.gen, wire.Message{
		ID: "sent-from-phone", ConversationID: syntheticDirectPNChat,
		FromMe: true, SenderName: "Owner WhatsApp Name", Timestamp: 1,
	}, nil)
	if got := backend.convs[syntheticDirectPNChat].Name; got != syntheticPNJID.User {
		t.Fatalf("outgoing message named conversation %q; want contact number %q", got, syntheticPNJID.User)
	}
}

func TestCommitMessageUsesPhoneNumberInsteadOfPushNameWithoutSavedContact(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, "777888999@s.whatsapp.net")
	jid, _ := types.ParseJID("777888999@s.whatsapp.net")
	backend.commitMessage(backend.gen, wire.Message{
		ID: "incoming-no-contact", ConversationID: jid.String(), SenderID: jid.String(),
		SenderName: "WhatsApp Profile Name", Timestamp: 1,
	}, nil)
	if got := backend.convs[jid.String()].Name; got != jid.User {
		t.Fatalf("conversation name = %q; want phone number %q", got, jid.User)
	}
}

func TestHistorySyncUsesPhoneNumberInsteadOfWhatsAppProfileNameWithoutSavedContact(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, "777888999@s.whatsapp.net")
	data := &waHistorySync.HistorySync{Conversations: []*waHistorySync.Conversation{{
		ID: proto.String("777888999@s.whatsapp.net"), Name: proto.String("WhatsApp Profile Name"),
	}}}
	if err := backend.ingestHistorySyncMode(backend.gen, data, false); err != nil {
		t.Fatal(err)
	}
	if got := backend.convs["777888999@s.whatsapp.net"].Name; got != "777888999" {
		t.Fatalf("history conversation name = %q; want phone number", got)
	}
}

func TestCommitMessageReusesExistingPNLIDConversation(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectLIDChat)
	backend.device = syntheticHistoryDevice()
	_ = seedNamedConversation(t, backend, syntheticLIDJID, syntheticSavedContactName)
	backend.commitMessage(backend.gen, wire.Message{
		ID: "sent-from-phone-alias", ConversationID: syntheticDirectPNChat,
		FromMe: true, Timestamp: 2,
	}, nil)
	if got := backend.Conversations(0); len(got) != 1 || got[0].ID != syntheticDirectLIDChat {
		t.Fatalf("PN/LID messages created duplicate conversation: %#v", got)
	}
	if got := backend.messages[syntheticDirectLIDChat]; len(got) != 1 || got[0].ConversationID != syntheticDirectLIDChat {
		t.Fatalf("alias message not stored under existing conversation: %#v", got)
	}
}

func TestReconcileConversationAliasesMergesPersistedRowsAndHistory(t *testing.T) {
	backend, _, _ := historyBackendWithAnchorChat(t, time.Second, syntheticDirectLIDChat)
	backend.device = syntheticHistoryDevice()
	oldID, newID := syntheticDirectLIDChat, syntheticDirectPNChat
	backend.mu.Lock()
	backend.convs[oldID] = wire.Conversation{ID: oldID, Name: syntheticSavedContactName, Timestamp: 2, Preview: "newer"}
	backend.convs[newID] = wire.Conversation{ID: newID, Name: syntheticSavedContactName, Timestamp: 1, Preview: "older"}
	backend.order = []string{oldID, newID}
	gen := backend.gen
	backend.mu.Unlock()
	if err := backend.historyStore.put(context.Background(), []wire.Message{
		{ID: "lid-message", ConversationID: oldID, Text: "from LID", Timestamp: 2},
		{ID: "pn-message", ConversationID: newID, Text: "from PN", Timestamp: 1},
	}); err != nil {
		t.Fatal(err)
	}
	backend.reconcileConversationAliases(gen, backend.device)
	if got := backend.Conversations(0); len(got) != 1 || got[0].ID != newID || got[0].Preview != "newer" {
		t.Fatalf("merged conversations = %#v", got)
	}
	page, err := backend.historyStore.page(context.Background(), newID, 10, "", 0)
	if err != nil || len(page.Messages) != 3 {
		t.Fatalf("merged history = %+v, err=%v", page, err)
	}
	if _, exists := backend.convs[oldID]; exists {
		t.Fatal("old alias conversation remains")
	}
}
