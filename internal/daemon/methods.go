package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/wire"
)

var errNotConnected = errors.New("not connected to Google Messages")

func (d *Daemon) requireClient() (*libgm.Client, error) {
	d.mu.RLock()
	c := d.client
	state := d.status.State
	d.mu.RUnlock()
	if c == nil {
		return nil, errNotConnected
	}
	if state != wire.StateConnected {
		return nil, fmt.Errorf("%w (state: %s)", errNotConnected, state)
	}
	return c, nil
}

// Status returns the current cached status.
func (d *Daemon) Status() wire.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status.Unread = d.unreadCountLocked()
	return d.status
}

// Conversations returns the cached conversation list, newest first.
func (d *Daemon) Conversations(limit int) []wire.Conversation {
	d.mu.RLock()
	defer d.mu.RUnlock()
	list := d.listLocked()
	if limit > 0 && limit < len(list) {
		list = list[:limit]
	}
	return list
}

// Messages fetches one page of history for a conversation. The cursor lets the
// panel page backwards as the user scrolls up.
func (d *Daemon) Messages(ctx context.Context, p wire.MessagesParams) (*wire.MessagesResult, error) {
	c, err := d.requireClient()
	if err != nil {
		return nil, err
	}
	if p.Count <= 0 {
		p.Count = 40
	}
	var cursor *gmproto.Cursor
	if p.CursorID != "" {
		cursor = &gmproto.Cursor{LastItemID: p.CursorID, LastItemTimestamp: p.CursorTime}
	}
	resp, err := withAuthRetry(ctx, d, func() (*gmproto.ListMessagesResponse, error) {
		return c.FetchMessages(ctx, p.ConversationID, p.Count, cursor)
	})
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}

	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	msgs := resp.GetMessages()
	out := &wire.MessagesResult{
		ConversationID: p.ConversationID,
		Messages:       make([]wire.Message, 0, len(msgs)),
	}
	// The API returns newest-first; the view renders oldest-first.
	for i := len(msgs) - 1; i >= 0; i-- {
		d.media.record(msgs[i])
		d.recordReactions(msgs[i])
		converted := convertMessage(msgs[i], d.senderName(msgs[i]))
		d.markMyReactions(p.ConversationID, &converted)
		out.Messages = append(out.Messages, converted)
	}
	if cur := resp.GetCursor(); cur != nil {
		out.CursorID = cur.GetLastItemID()
		out.CursorTime = cur.GetLastItemTimestamp()
		out.HasMore = out.CursorID != ""
	}
	return out, nil
}

// Send delivers a text message and optimistically marks the thread read.
func (d *Daemon) Send(ctx context.Context, p wire.SendParams) (*wire.Message, error) {
	c, err := d.requireClient()
	if err != nil {
		return nil, err
	}
	if p.Text == "" {
		return nil, errors.New("empty message")
	}

	d.mu.RLock()
	conv, ok := d.convs[p.ConversationID]
	d.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown conversation %q", p.ConversationID)
	}
	if conv.ReadOnly {
		return nil, errors.New("conversation is read-only")
	}

	tmpID, err := messageTransactionID(p.TmpID)
	if err != nil {
		return nil, err
	}
	req := &gmproto.SendMessageRequest{
		ConversationID: p.ConversationID,
		TmpID:          tmpID,
		MessagePayload: &gmproto.MessagePayload{
			TmpID:          tmpID,
			TmpID2:         tmpID,
			ConversationID: p.ConversationID,
			ParticipantID:  conv.OutgoingID,
			MessageInfo: []*gmproto.MessageInfo{{
				Data: &gmproto.MessageInfo_MessageContent{
					MessageContent: &gmproto.MessageContent{Content: p.Text},
				},
			}},
		},
	}
	if p.ReplyToID != "" {
		req.Reply = &gmproto.ReplyPayload{MessageID: p.ReplyToID}
	}

	resp, err := withAuthRetry(ctx, d, func() (*gmproto.SendMessageResponse, error) {
		return c.SendMessage(ctx, req)
	})
	if err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	if status := resp.GetStatus(); status != gmproto.SendMessageResponse_SUCCESS {
		return nil, fmt.Errorf("send rejected: %s", status)
	}

	return &wire.Message{
		ID:             tmpID,
		TmpID:          tmpID,
		Provisional:    true,
		Timestamp:      time.Now().UnixMicro(),
		ConversationID: p.ConversationID,
		Text:           p.Text,
		FromMe:         true,
		Pending:        true,
		ReplyToID:      p.ReplyToID,
	}, nil
}

// MarkRead clears the unread flag both upstream and in the local cache.
func (d *Daemon) MarkRead(ctx context.Context, p wire.MarkReadParams) error {
	c, err := d.requireClient()
	if err != nil {
		return err
	}
	if err := c.MarkRead(ctx, p.ConversationID, p.MessageID); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	if conv, ok := d.convs[p.ConversationID]; ok && conv.Unread {
		conv.Unread = false
		d.convs[p.ConversationID] = conv
	}
	out, ok := d.convs[p.ConversationID]
	d.mu.Unlock()
	if ok {
		d.publish(wire.EventConversation, out)
	}
	d.publishStatus()
	return nil
}

// Refresh re-pulls the conversation list from the phone.
func (d *Daemon) Refresh(ctx context.Context) error {
	// Sync is permitted while connecting. Sending still requires connected.
	d.sessionMu.Lock()
	if ctx.Err() != nil {
		d.sessionMu.Unlock()
		return ctx.Err()
	}
	if !d.canSyncLocked() {
		d.sessionMu.Unlock()
		return errNotConnected
	}
	d.mu.RLock()
	c := d.client
	session := d.sessionCtx
	d.mu.RUnlock()
	d.sessionMu.Unlock()
	ctx = sessionBoundContext{Context: ctx, session: session}
	resp, err := withAuthRetry(ctx, d, func() (*gmproto.ListConversationsResponse, error) {
		return c.ListConversations(ctx, convListLimit, gmproto.ListConversationsRequest_INBOX)
	})
	if err != nil {
		return fmt.Errorf("list conversations: %w", err)
	}
	return d.acceptConversationSync(ctx, c, resp)
}

func (d *Daemon) acceptConversationSync(ctx context.Context, c *libgm.Client, resp *gmproto.ListConversationsResponse) error {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !d.canSyncLocked() || d.client != c {
		return errNotConnected
	}
	d.replaceConversations(resp.GetConversations())
	d.mu.Lock()
	d.status.PhoneOK = true
	d.status.LastSyncSec = time.Now().Unix()
	d.mu.Unlock()
	d.setState(wire.StateConnected, "")
	return nil
}

// qrExpiry is how long Google honours a pairing code. It is short enough that
// a one-shot QR is unusable in practice — by the time you have unlocked your
// phone and opened the scanner it has already lapsed — so StartPairing runs a
// refresh loop for as long as the pairing screen is up.
const qrExpiry = 30 * time.Second

// qrMaxRefreshes bounds the loop so an abandoned pairing screen does not poll
// Google forever. 20 rounds is ten minutes.
const qrMaxRefreshes = 20

// StartPairing registers a new browser relay and returns a QR payload for the
// plugin to render. Scanning it in Messages > Device pairing completes it, and
// the PairSuccessful event persists the session.
func (d *Daemon) StartPairing() (string, error) {
	parent := d.sessionContext()
	d.sessionMu.Lock()
	if parent.Err() != nil || d.gaiaActive {
		d.sessionMu.Unlock()
		return "", errors.New("pairing is not available")
	}
	d.stopPairRefresh()
	d.mu.Lock()
	d.paired = false
	d.mu.Unlock()
	d.sessionMu.Unlock()
	d.mu.RLock()
	c := d.client
	d.mu.RUnlock()
	if c == nil {
		return "", errors.New("client not initialized")
	}
	qr, err := c.StartLogin()
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	if parent.Err() != nil {
		return "", parent.Err()
	}
	if err != nil {
		d.setState(wire.StateError, err.Error())
		return "", fmt.Errorf("start pairing: %w", err)
	}
	d.mu.Lock()
	d.status.State = wire.StatePairing
	d.status.QRURL = qr
	d.status.Error = ""
	st := d.status
	d.mu.Unlock()
	d.publish(wire.EventStatus, st)
	d.publish(wire.EventQR, map[string]string{"url": qr})

	d.startPairRefresh()
	return qr, nil
}

// startPairRefresh replaces the QR every qrExpiry until pairing completes,
// is cancelled, or the attempt budget runs out.
func (d *Daemon) startPairRefresh() {
	d.stopPairRefresh()

	ctx, cancel := context.WithCancel(d.sessionContext())
	d.mu.Lock()
	d.pairCancel = cancel
	c := d.client
	d.mu.Unlock()

	go func() {
		defer cancel()
		ticker := time.NewTicker(qrExpiry)
		defer ticker.Stop()

		for i := 0; i < qrMaxRefreshes; i++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			qr, err := c.RefreshPhoneRelay()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				d.log.Warn().Err(err).Msg("Failed to refresh pairing QR")
				d.inSession(ctx, func() { d.setState(wire.StateError, "pairing code expired: "+err.Error()) })
				return
			}

			d.sessionMu.Lock()
			if ctx.Err() != nil {
				d.sessionMu.Unlock()
				return
			}
			d.mu.Lock()
			// Another state change (paired, unpaired, cancelled) won the race.
			if d.status.State != wire.StatePairing {
				d.mu.Unlock()
				d.sessionMu.Unlock()
				return
			}
			d.status.QRURL = qr
			st := d.status
			d.mu.Unlock()

			d.log.Debug().Int("round", i+1).Msg("Refreshed pairing QR")
			d.publish(wire.EventStatus, st)
			d.publish(wire.EventQR, map[string]string{"url": qr})
			d.sessionMu.Unlock()
		}

		d.inSession(ctx, func() { d.setState(wire.StateUnpaired, "pairing timed out") })
	}()
}

func (d *Daemon) stopPairRefresh() {
	d.mu.Lock()
	cancel := d.pairCancel
	d.pairCancel = nil
	d.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Unpair clears local credentials even when the remote revocation fails.
func (d *Daemon) Unpair(ctx context.Context) error {
	return d.unpair(ctx, func(ctx context.Context, c *libgm.Client) error {
		return c.Unpair(ctx)
	})
}

func (d *Daemon) unpair(ctx context.Context, revoke func(context.Context, *libgm.Client) error) error {
	d.sessionMu.Lock()
	old, localErr := d.resetSessionLocked()
	replacement := d.sessionContext()
	d.sessionMu.Unlock()

	var remoteErr error
	if old != nil {
		remoteErr = revoke(ctx, old)
		old.Disconnect()
	}
	if localErr == nil && remoteErr == nil {
		return nil
	}
	if localErr != nil {
		d.log.Error().Err(localErr).Msg("Unpair local cleanup incomplete")
	}
	if remoteErr != nil {
		d.log.Warn().Err(remoteErr).Msg("Google device revocation could not be confirmed")
	}
	err := &unpairFailure{localErr: localErr, remoteErr: remoteErr}
	// Unpair cancels its old session, and a new pairing can start while the
	// revocation request is pending. Report only into this reset's session.
	d.inSession(replacement, func() {
		// QR pairing can reuse this session context, so check its state too.
		if d.Status().State == wire.StateUnpaired {
			d.setState(wire.StateUnpaired, err.Error())
		}
	})
	return err
}

// Keep transport details in logs and wrapped causes, with actionable UI copy.
type unpairFailure struct {
	localErr, remoteErr error
}

func (e *unpairFailure) Error() string {
	message := "Local credentials and cached files were cleared."
	if e.localErr != nil {
		message = "Local cleanup was incomplete. Check permissions on OmaChat's local data and cache folders."
	}
	if e.remoteErr != nil {
		message += " Google device revocation could not be confirmed. Remove OmaChat in Google Messages on your phone under Device pairing."
	}
	return message
}

func (e *unpairFailure) Unwrap() []error {
	var causes []error
	if e.localErr != nil {
		causes = append(causes, e.localErr)
	}
	if e.remoteErr != nil {
		causes = append(causes, e.remoteErr)
	}
	return causes
}

func messageTransactionID(value string) (string, error) {
	if value == "" {
		return uuid.NewString(), nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return "", errors.New("invalid message transaction ID")
	}
	return id.String(), nil
}
