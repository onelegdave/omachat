package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/onelegdave/omachat/internal/wire"
)

const notificationInterface = "org.freedesktop.Notifications"

var errNotificationsUnavailable = errors.New("Desktop notifications are unavailable. Check your desktop notification service and try again.")

type notificationTransport interface {
	owner(context.Context) (string, error)
	notify(context.Context, uint32, string, string, string) (uint32, error)
	close(context.Context, uint32) error
	stop()
}

type desktopNotifications struct {
	mu        sync.Mutex
	transport notificationTransport
	server    string
	stopped   bool
	targets   map[uint32]wire.NotificationTarget
	replaces  map[string]uint32
	order     []uint32
	publish   func(wire.Event)
}

func newDesktopNotifications(publish func(wire.Event)) *desktopNotifications {
	return &desktopNotifications{targets: make(map[uint32]wire.NotificationTarget), replaces: make(map[string]uint32), publish: publish}
}

func notificationKey(target wire.NotificationTarget) string {
	data, _ := json.Marshal(target)
	return string(data)
}

func notificationText(network string, p wire.NotifyMessageParams, previews bool) (string, string) {
	service := map[string]string{"gmessages": "Google Messages", "whatsapp": "WhatsApp", "telegram": "Telegram", "messenger": "Messenger"}[network]
	title, body := "OmaChat · "+service, "New message"
	if !previews {
		return title, body
	}
	if name := strings.TrimSpace(p.ConversationName); name != "" {
		title = truncateNotification(name, 120) + " · " + service
	} else if name := strings.TrimSpace(p.Message.SenderName); name != "" {
		title = truncateNotification(name, 120) + " · " + service
	}
	if text := strings.TrimSpace(p.Message.Text); text != "" {
		body = truncateNotification(text, 240)
	} else if len(p.Message.Attachments) > 0 {
		attachment := p.Message.Attachments[0]
		switch {
		case attachment.IsAudio:
			body = "Voice message"
		case attachment.IsGif:
			body = "GIF"
		case attachment.IsVideo:
			body = "Video"
		case attachment.IsImage:
			body = "Photo"
		default:
			body = "Attachment"
		}
	}
	return title, html.EscapeString(body)
}

func truncateNotification(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

func (n *desktopNotifications) send(ctx context.Context, target wire.NotificationTarget, icon, title, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return errNotificationsUnavailable
	}
	if n.transport == nil {
		transport, err := connectNotifications(n.handleSignal)
		if err != nil {
			return errNotificationsUnavailable
		}
		n.transport = transport
	}
	owner, err := n.transport.owner(ctx)
	if err != nil {
		return errNotificationsUnavailable
	}
	if owner != n.server {
		n.server = owner
		n.targets = make(map[uint32]wire.NotificationTarget)
		n.replaces = make(map[string]uint32)
		n.order = nil
	}
	key := notificationKey(target)
	id, err := n.transport.notify(ctx, n.replaces[key], icon, title, body)
	if err != nil || id == 0 {
		return errNotificationsUnavailable
	}
	if _, exists := n.targets[id]; !exists {
		n.order = append(n.order, id)
	}
	n.targets[id] = target
	n.replaces[key] = id
	for len(n.order) > 128 {
		n.forgetLocked(n.order[0])
	}
	return nil
}

func (n *desktopNotifications) forgetLocked(id uint32) {
	if target, exists := n.targets[id]; exists {
		key := notificationKey(target)
		if n.replaces[key] == id {
			delete(n.replaces, key)
		}
	}
	delete(n.targets, id)
	for i, existing := range n.order {
		if existing == id {
			n.order = append(n.order[:i], n.order[i+1:]...)
			break
		}
	}
}

func (n *desktopNotifications) handleSignal(signal *dbus.Signal) {
	if signal == nil || len(signal.Body) < 2 {
		return
	}
	id, ok := signal.Body[0].(uint32)
	if !ok {
		return
	}
	n.mu.Lock()
	target, exists := n.targets[id]
	if n.stopped || signal.Sender != n.server || !exists {
		n.mu.Unlock()
		return
	}
	open := signal.Name == notificationInterface+".ActionInvoked" && signal.Body[1] == "default"
	if open || signal.Name == notificationInterface+".NotificationClosed" {
		n.forgetLocked(id)
	}
	n.mu.Unlock()
	if open && n.publish != nil {
		n.publish(wire.Event{Event: wire.EventNotificationOpen, Data: target})
	}
}

func (n *desktopNotifications) dismiss(ctx context.Context, network string) {
	if n == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, target := range n.targets {
		if network != "" && target.Network != network {
			continue
		}
		n.forgetLocked(id)
		if n.transport != nil {
			_ = n.transport.close(ctx, id)
		}
	}
}

func (n *desktopNotifications) stop() {
	if n == nil {
		return
	}
	n.mu.Lock()
	n.stopped = true
	transport := n.transport
	n.mu.Unlock()
	n.dismiss(context.Background(), "")
	if transport != nil {
		transport.stop()
	}
}

func (d *Daemon) handleNotificationRequest(ctx context.Context, req wire.Request) wire.Response {
	fail := func(err error) wire.Response { return wire.Response{ID: req.ID, Error: err.Error()} }
	if req.Method == wire.MethodSetNotifications {
		p, err := decodeParams[wire.SetNotificationsParams](req.Params)
		if err != nil {
			return fail(err)
		}
		if err := d.config.SetNotifications(p.Enabled, p.Previews); err != nil {
			return fail(err)
		}
		if (p.Enabled != nil && !*p.Enabled) || (p.Previews != nil && !*p.Previews) {
			d.notifications.dismiss(ctx, "")
		}
		return wire.Response{ID: req.ID, OK: true, Result: d.PluginConfig()}
	}
	cfg := d.config.Get()
	if !cfg.NotificationsOn() {
		return wire.Response{ID: req.ID, OK: true, Result: false}
	}
	if req.Method == wire.MethodTestNotification {
		if err := d.notifications.send(ctx, wire.NotificationTarget{}, "mail-unread", "OmaChat", "Desktop notifications are working. Click to open notification settings."); err != nil {
			return fail(err)
		}
		return wire.Response{ID: req.ID, OK: true, Result: true}
	}
	network := req.Network
	if network == "" {
		network = wire.NetworkGMessages
	}
	if err := d.serviceRequestError(network); err != nil {
		return fail(err)
	}
	ready := false
	switch network {
	case wire.NetworkGMessages:
		ready = d.Status().State == wire.StateConnected
	case wire.NetworkWhatsApp:
		ready = d.wa != nil && d.wa.Status().State == wire.StateConnected
	case wire.NetworkTelegram:
		ready = d.tg != nil && d.tg.Status().State == wire.StateConnected
	case wire.NetworkMessenger:
		ready = d.fb != nil && d.fb.Status().State == wire.StateConnected
	}
	if !ready {
		return wire.Response{ID: req.ID, OK: true, Result: false}
	}
	p, err := decodeParams[wire.NotifyMessageParams](req.Params)
	if err != nil {
		return fail(err)
	}
	msg := p.Message
	if msg.ID == "" || msg.ConversationID == "" || len(msg.ID) > 1024 || len(msg.ConversationID) > 1024 {
		return fail(errors.New("invalid notification message"))
	}
	if msg.FromMe || msg.Deleted || msg.Pending || msg.Failed || msg.Provisional || (strings.TrimSpace(msg.Text) == "" && len(msg.Attachments) == 0) {
		return wire.Response{ID: req.ID, OK: true, Result: false}
	}
	title, body := notificationText(network, p, cfg.NotificationPreviews)
	icon := d.notificationIcon(network, msg.ConversationID)
	if err := d.notifications.send(ctx, wire.NotificationTarget{Network: network, ConversationID: msg.ConversationID}, icon, title, body); err != nil {
		return fail(err)
	}
	return wire.Response{ID: req.ID, OK: true, Result: true}
}

func (d *Daemon) notificationIcon(network, conversationID string) string {
	var conversations []wire.Conversation
	var directory string
	switch network {
	case wire.NetworkGMessages:
		d.mu.RLock()
		conversation := d.convs[conversationID]
		d.mu.RUnlock()
		conversations = []wire.Conversation{conversation}
		directory = d.paths.MediaDir()
	case wire.NetworkWhatsApp:
		if d.wa != nil {
			conversations = d.wa.Conversations(0)
		}
		directory = d.paths.WhatsAppMediaDir()
	case wire.NetworkTelegram:
		if d.tg != nil {
			conversations = d.tg.Conversations(0)
		}
		directory = d.paths.TelegramMediaDir()
	case wire.NetworkMessenger:
		if d.fb != nil {
			conversations = d.fb.Conversations(0)
		}
		directory = d.paths.MessengerMediaDir()
	}
	for _, conversation := range conversations {
		if conversation.ID != conversationID || conversation.AvatarPath == "" {
			continue
		}
		path := filepath.Clean(conversation.AvatarPath)
		relative, err := filepath.Rel(directory, path)
		if err != nil || !filepath.IsAbs(path) || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			break
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			break
		}
		return (&url.URL{Scheme: "file", Path: path}).String()
	}
	return "mail-unread"
}

type dbusNotifications struct {
	conn *dbus.Conn
	wg   sync.WaitGroup
}

func connectNotifications(handle func(*dbus.Signal)) (*dbusNotifications, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	if err := conn.AddMatchSignal(dbus.WithMatchSender(notificationInterface), dbus.WithMatchInterface(notificationInterface), dbus.WithMatchObjectPath("/org/freedesktop/Notifications")); err != nil {
		conn.Close()
		return nil, err
	}
	n := &dbusNotifications{conn: conn}
	signals := make(chan *dbus.Signal, 32)
	conn.Signal(signals)
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for signal := range signals {
			handle(signal)
		}
	}()
	return n, nil
}

func (n *dbusNotifications) owner(ctx context.Context) (string, error) {
	var owner string
	err := n.conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, notificationInterface).Store(&owner)
	return owner, err
}

func (n *dbusNotifications) notify(ctx context.Context, replaces uint32, icon, title, body string) (uint32, error) {
	var id uint32
	hints := map[string]dbus.Variant{"urgency": dbus.MakeVariant(byte(1)), "category": dbus.MakeVariant("im.received")}
	err := n.conn.Object(notificationInterface, "/org/freedesktop/Notifications").CallWithContext(ctx, notificationInterface+".Notify", 0,
		"OmaChat", replaces, icon, title, body, []string{"default", "Open"}, hints, int32(8000)).Store(&id)
	return id, err
}

func (n *dbusNotifications) close(ctx context.Context, id uint32) error {
	return n.conn.Object(notificationInterface, "/org/freedesktop/Notifications").CallWithContext(ctx, notificationInterface+".CloseNotification", 0, id).Err
}

func (n *dbusNotifications) stop() {
	n.conn.Close()
	n.wg.Wait()
}
