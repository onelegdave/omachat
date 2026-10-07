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
