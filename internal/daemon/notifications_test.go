package daemon

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"
)

type fakeNotifications struct {
	server            string
	id                uint32
	replace           uint32
	icon, title, body string
	closed            []uint32
	err               error
}

func (f *fakeNotifications) owner(context.Context) (string, error) { return f.server, f.err }
func (f *fakeNotifications) notify(_ context.Context, replace uint32, icon, title, body string) (uint32, error) {
	f.replace, f.icon, f.title, f.body = replace, icon, title, body
	if replace != 0 {
		return replace, f.err
	}
	f.id++
	return f.id, f.err
}
func (f *fakeNotifications) close(_ context.Context, id uint32) error {
	f.closed = append(f.closed, id)
	return nil
}
func (f *fakeNotifications) stop() {}

func TestNotificationPreferencesPersistAndPreserveConfig(t *testing.T) {
	d := freshSelectionDaemon(t)
	if !d.PluginConfig().NotificationsEnabled || d.PluginConfig().NotificationPreviews {
		t.Fatal("incorrect notification defaults")
	}
	if err := d.config.SetTelegramCredentials(123, strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string]any{{"enabled": false}, {"previews": true}} {
		resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetNotifications, Params: values})
		if !resp.OK {
			t.Fatal(resp.Error)
		}
	}
	cfg := store.NewConfigStore(d.paths.ConfigFile()).Get()
	if cfg.NotificationsOn() || !cfg.NotificationPreviews || cfg.TelegramAPIID != 123 || cfg.TelegramAPIHash != strings.Repeat("a", 32) {
		t.Fatal("notification preferences did not survive reload or replaced unrelated config")
	}
	for _, values := range []any{nil, map[string]any{"enabled": "yes"}, map[string]any{"previews": 42}} {
		if d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetNotifications, Params: values}).OK {
			t.Fatalf("accepted invalid preferences: %v", values)
		}
	}
}

func TestNotificationFormattingRespectsPreviewPreference(t *testing.T) {
	p := wire.NotifyMessageParams{ConversationName: "Demo Contact", Message: wire.Message{Text: "<b>hello</b> & \"world\""}}
	title, body := notificationText("telegram", p, false)
	if title != "OmaChat · Telegram" || body != "New message" {
		t.Fatal("disabled previews exposed sender or message content")
	}
	title, body = notificationText("telegram", p, true)
	if title != "Demo Contact · Telegram" || body != "&lt;b&gt;hello&lt;/b&gt; &amp; &#34;world&#34;" {
		t.Fatalf("preview was not escaped: %q %q", title, body)
	}
	p.Message.Text = strings.Repeat("😀", 300)
	_, body = notificationText("telegram", p, true)
	if len([]rune(body)) != 241 {
		t.Fatal("notification preview is not bounded by characters")
	}
	p.Message.Text = ""
	p.Message.Attachments = []wire.Attachment{{IsAudio: true}}
	_, body = notificationText("whatsapp", p, true)
	if body != "Voice message" {
		t.Fatal("attachment-only message has no useful preview")
	}
}

func TestNotificationsReplaceByServiceAndRouteTrustedActions(t *testing.T) {
	var events []wire.Event
	n := newDesktopNotifications(func(event wire.Event) { events = append(events, event) })
	transport := &fakeNotifications{server: ":1.99"}
	n.transport = transport
	first := wire.NotificationTarget{Network: "telegram", ConversationID: "demo"}
	second := wire.NotificationTarget{Network: "whatsapp", ConversationID: "demo"}
	for _, target := range []wire.NotificationTarget{first, first, second} {
		if err := n.send(context.Background(), target, "mail-unread", "OmaChat", "New message"); err != nil {
			t.Fatal(err)
		}
	}
	if transport.id != 2 || len(n.targets) != 2 {
		t.Fatal("notifications are not grouped by service and conversation")
	}
	n.handleSignal(&dbus.Signal{Sender: ":1.spoof", Name: notificationInterface + ".ActionInvoked", Body: []any{uint32(1), "default"}})
	if len(events) != 0 {
		t.Fatal("an unrelated bus sender opened a conversation")
	}
	n.handleSignal(&dbus.Signal{Sender: transport.server, Name: notificationInterface + ".ActionInvoked", Body: []any{uint32(1), "default"}})
	if len(events) != 1 || events[0].Event != wire.EventNotificationOpen || events[0].Data != first {
		t.Fatal("notification action did not route to its conversation")
	}
	n.handleSignal(&dbus.Signal{Sender: transport.server, Name: notificationInterface + ".ActionInvoked", Body: []any{uint32(1), "default"}})
	if len(events) != 1 {
		t.Fatal("notification action fired twice")
	}
	n.dismiss(context.Background(), "whatsapp")
	if len(n.targets) != 0 || len(transport.closed) != 1 {
		t.Fatal("unpairing did not dismiss the service notification")
	}
}

func TestNotificationServerRestartRetiresOldIDs(t *testing.T) {
	n := newDesktopNotifications(nil)
	transport := &fakeNotifications{server: ":1.99"}
	n.transport = transport
	first := wire.NotificationTarget{Network: "telegram", ConversationID: "demo-a"}
	second := wire.NotificationTarget{Network: "telegram", ConversationID: "demo-b"}
	if err := n.send(context.Background(), first, "mail-unread", "OmaChat", "New message"); err != nil {
		t.Fatal(err)
	}
	transport.server, transport.id = ":1.100", 0
	if err := n.send(context.Background(), second, "mail-unread", "OmaChat", "New message"); err != nil {
		t.Fatal(err)
	}
	if transport.replace != 0 || len(n.targets) != 1 || n.targets[1] != second {
		t.Fatal("new notification server reused stale notification IDs")
	}
	n.handleSignal(&dbus.Signal{Sender: ":1.99", Name: notificationInterface + ".NotificationClosed", Body: []any{uint32(1), uint32(2)}})
	if len(n.targets) != 1 {
		t.Fatal("old server signal removed a new notification")
	}
	transport.err = errors.New("unavailable")
	if err := n.send(context.Background(), second, "mail-unread", "OmaChat", "New message"); err != errNotificationsUnavailable {
		t.Fatal("notification error was not surfaced safely")
	}
}

func TestNotificationIconUsesMatchingServiceAvatar(t *testing.T) {
	d := freshSelectionDaemon(t)
	if err := os.MkdirAll(d.paths.MediaDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(d.paths.TelegramMediaDir(), 0700); err != nil {
		t.Fatal(err)
	}
	google := filepath.Join(d.paths.MediaDir(), "google.png")
	telegram := filepath.Join(d.paths.TelegramMediaDir(), "contact #1 photo.jpg")
	for _, path := range []string{google, telegram} {
		if err := os.WriteFile(path, []byte("synthetic avatar"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d.convs["same-id"] = wire.Conversation{ID: "same-id", AvatarPath: google}
	d.tg.AddTestConversation(wire.Conversation{ID: "same-id", AvatarPath: telegram})
	for network, path := range map[string]string{"gmessages": google, "telegram": telegram} {
		if icon := d.notificationIcon(network, "same-id"); icon != (&url.URL{Scheme: "file", Path: path}).String() {
			t.Fatalf("wrong avatar for %s: %s", network, icon)
		}
	}
	if err := os.Remove(telegram); err != nil {
		t.Fatal(err)
	}
	if d.notificationIcon("telegram", "same-id") != "mail-unread" || d.notificationIcon("telegram", "missing") != "mail-unread" {
		t.Fatal("missing avatar did not use the generic fallback")
	}
	d.tg.AddTestConversation(wire.Conversation{ID: "wrong-service", AvatarPath: google})
	if d.notificationIcon("telegram", "wrong-service") != "mail-unread" {
		t.Fatal("another service's image was accepted")
	}
	link := filepath.Join(d.paths.TelegramMediaDir(), "linked.jpg")
	if err := os.Symlink(google, link); err != nil {
		t.Fatal(err)
	}
	d.tg.AddTestConversation(wire.Conversation{ID: "linked", AvatarPath: link})
	if d.notificationIcon("telegram", "linked") != "mail-unread" {
		t.Fatal("symlink avatar was accepted")
	}
}

func TestNotificationRPCUsesAvatarWithoutEnablingTextPreviews(t *testing.T) {
	d := freshSelectionDaemon(t)
	d.activeServices["telegram"] = true
	d.tg.SetState(wire.StateConnected, "")
	if err := os.MkdirAll(d.paths.TelegramMediaDir(), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.paths.TelegramMediaDir(), "fixture.jpg")
	if err := os.WriteFile(path, []byte("synthetic avatar"), 0600); err != nil {
		t.Fatal(err)
	}
	d.tg.AddTestConversation(wire.Conversation{ID: "tg:42", AvatarPath: path})
	transport := &fakeNotifications{server: ":1.99"}
	d.notifications.transport = transport
	p := wire.NotifyMessageParams{ConversationName: "Demo Contact", Message: wire.Message{ID: "fixture", ConversationID: "tg:42", Text: "Synthetic message"}}
	resp := d.dispatch(context.Background(), wire.Request{Network: "telegram", Method: wire.MethodNotifyMessage, Params: p})
	if !resp.OK || transport.icon != (&url.URL{Scheme: "file", Path: path}).String() || transport.title != "OmaChat · Telegram" || transport.body != "New message" {
		t.Fatal("contact avatar was not used independently of text previews")
	}
	if resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodTestNotification}); !resp.OK || transport.icon != "mail-unread" {
		t.Fatal("test notification should keep its generic icon")
	}
}

func TestNotificationRPCRejectsOutgoingAndDisabledNotifications(t *testing.T) {
	d := freshSelectionDaemon(t)
	d.activeServices["gmessages"] = true
	d.status.State = wire.StateConnected
	transport := &fakeNotifications{server: ":1.99"}
	d.notifications.transport = transport
	p := wire.NotifyMessageParams{Message: wire.Message{ID: "demo-message", ConversationID: "demo-chat", Text: "Hello", Timestamp: time.Now().UnixMicro(), FromMe: true}}
	request := wire.Request{Method: wire.MethodNotifyMessage, Params: p}
	if resp := d.dispatch(context.Background(), request); !resp.OK || transport.id != 0 {
		t.Fatal("outgoing message created a notification")
	}
	p.Message.FromMe = false
	request.Params = p
	if resp := d.dispatch(context.Background(), request); !resp.OK || transport.id != 1 {
		t.Fatal("incoming message did not create a notification")
	}
	resp := d.dispatch(context.Background(), wire.Request{Method: wire.MethodSetNotifications, Params: map[string]any{"enabled": false}})
	if !resp.OK || len(transport.closed) != 1 {
		t.Fatal("disabling notifications did not dismiss existing alerts")
	}
	if resp := d.dispatch(context.Background(), request); !resp.OK || transport.id != 1 {
		t.Fatal("disabled notifications still created an alert")
	}
}

func TestGoogleMessageReadUpdatesDoNotRequestNotifications(t *testing.T) {
	d := freshSelectionDaemon(t)
	events, unsubscribe := d.Subscribe()
	defer unsubscribe()
	for _, status := range []gmproto.MessageStatusType{gmproto.MessageStatusType_INCOMING_COMPLETE, gmproto.MessageStatusType_INCOMING_DISPLAYED, gmproto.MessageStatusType_OUTGOING_DELIVERED} {
		d.handleMessage(&gmproto.Message{MessageID: "fixture", ConversationID: "demo", MessageStatus: &gmproto.MessageStatus{Status: status}})
		event := <-events
		if event.Event != wire.EventMessage || event.Notify != (status == gmproto.MessageStatusType_INCOMING_COMPLETE) {
			t.Fatalf("incorrect notification marker for %s", status)
		}
	}
}
