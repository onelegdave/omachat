package whatsapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	maxWhatsAppAvatarBytes     = 4 << 20
	maxWhatsAppAvatarDimension = 2048
)

type avatarFetchJob struct {
	gen          uint64
	conversation string
	epoch        uint64
	ctx          context.Context
}

var errInvalidWhatsAppAvatar = errors.New("invalid WhatsApp avatar image")

func trustedWhatsAppAvatarURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("WhatsApp avatar URL must be an HTTPS URL")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	trusted := host == "whatsapp.net" || strings.HasSuffix(host, ".whatsapp.net")
	if !trusted {
		return nil, errors.New("WhatsApp avatar URL host is not trusted")
	}
	return u, nil
}

func fetchWhatsAppAvatarImage(ctx context.Context, raw string) ([]byte, error) {
	if _, err := trustedWhatsAppAvatarURL(raw); err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many WhatsApp avatar redirects")
			}
			_, err := trustedWhatsAppAvatarURL(req.URL.String())
			return err
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch WhatsApp avatar: %w", err)
	}
	defer resp.Body.Close()
	return readWhatsAppAvatarResponse(resp)
}

func readWhatsAppAvatarResponse(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.StatusCode != http.StatusOK {
		return nil, errors.New("WhatsApp avatar request failed")
	}
	if resp.ContentLength > maxWhatsAppAvatarBytes {
		return nil, errInboundMediaTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWhatsAppAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read WhatsApp avatar: %w", err)
	}
	if len(data) == 0 || len(data) > maxWhatsAppAvatarBytes {
		return nil, errInboundMediaTooLarge
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxWhatsAppAvatarDimension || cfg.Height > maxWhatsAppAvatarDimension {
		return nil, errInvalidWhatsAppAvatar
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		return nil, errInvalidWhatsAppAvatar
	}
	return data, nil
}

// whatsappAvatarTarget accepts only ordinary direct chats and standard groups.
// Keep the original canonical JID: LID and group IDs must not be guessed from
// display names or rewritten to a different identity.
func whatsappAvatarTarget(conversationID string) (types.JID, bool) {
	jid, err := types.ParseJID(conversationID)
	if err != nil || jid.IsEmpty() {
		return types.EmptyJID, false
	}
	switch jid.Server {
	case types.DefaultUserServer, "lid", types.GroupServer:
		return jid, true
	default:
		return types.EmptyJID, false
	}
}

func whatsappAvatarPath(dir, conversationID string, imageData []byte) string {
	contentHash := sha256.Sum256(imageData)
	key := conversationID + "\x00" + hex.EncodeToString(contentHash[:])
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(dir, "avatar-"+hex.EncodeToString(sum[:12])+".img")
}

func isWhatsAppAvatarPath(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && !filepath.IsAbs(rel) && rel != "." && filepath.Dir(rel) == "." && strings.HasPrefix(filepath.Base(rel), "avatar-") && filepath.Ext(rel) == ".img"
}

func (b *Backend) queueKnownAvatarFetches(gen uint64) {
	b.mu.RLock()
	ids := append([]string(nil), b.order...)
	b.mu.RUnlock()
	b.queueAvatarFetches(gen, ids)
}

func (b *Backend) queueAvatarFetches(gen uint64, ids []string) {
	b.queueAvatarFetchesMode(gen, ids, false)
}

func (b *Backend) queueAvatarFetchesMode(gen uint64, ids []string, force bool) {
	b.mu.Lock()
	if b.gen != gen || b.client == nil || !b.paired {
		b.mu.Unlock()
		return
	}
	ctx := b.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, id := range ids {
		if len(b.avatarQueue) >= maxConversations {
			break
		}
		conversation, exists := b.convs[id]
		if !exists {
			continue
		}
		if !force && conversation.AvatarPath != "" {
			if _, err := os.Stat(conversation.AvatarPath); err == nil {
				continue
			}
		}
		if _, ok := whatsappAvatarTarget(id); !ok || b.avatarPending[id] == gen || b.avatarAttempted[id] {
			continue
		}
		b.avatarPending[id] = gen
		b.avatarQueue = append(b.avatarQueue, avatarFetchJob{gen: gen, conversation: id, epoch: b.avatarEpoch[id], ctx: ctx})
	}
	if !b.avatarWorker && len(b.avatarQueue) > 0 {
		b.avatarWorker = true
		go b.runAvatarQueue()
	}
	b.mu.Unlock()
}

func (b *Backend) handlePictureEvent(gen uint64, evt *events.Picture) {
	if evt == nil || evt.JID.IsEmpty() {
		return
	}
	ids := []string{evt.JID.ToNonAD().String()}
	b.mu.RLock()
	device, ctx := b.device, b.ctx
	b.mu.RUnlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if device != nil && device.LIDs != nil {
		if alt, err := device.GetAltJID(ctx, evt.JID); err == nil && !alt.IsEmpty() {
			altID := alt.ToNonAD().String()
			if altID != ids[0] {
				ids = append(ids, altID)
			}
		}
	}

	var updated []wire.Conversation
	b.mu.Lock()
	if b.gen != gen {
		b.mu.Unlock()
		return
	}
	for _, id := range ids {
		conversation, exists := b.convs[id]
		if !exists {
			continue
		}
		if evt.Remove {
			b.avatarEpoch[id]++
			delete(b.avatarRefresh, id)
			if isWhatsAppAvatarPath(b.paths.WhatsAppMediaDir(), conversation.AvatarPath) {
				_ = os.Remove(conversation.AvatarPath)
			}
			conversation.AvatarPath = ""
			b.convs[id] = conversation
			delete(b.avatarAttempted, id)
			updated = append(updated, conversation)
		} else {
			b.avatarEpoch[id]++
			delete(b.avatarAttempted, id)
			if b.avatarPending[id] == gen {
				b.avatarRefresh[id] = true
			}
		}
	}
	if len(updated) > 0 {
		b.saveStoreLocked()
		for _, conversation := range updated {
			b.emitLocked(wire.EventConversation, conversation)
		}
	}
	b.mu.Unlock()
	if !evt.Remove {
		b.queueAvatarFetchesMode(gen, ids, true)
	}
}

func (b *Backend) runAvatarQueue() {
	for {
		b.mu.Lock()
		if len(b.avatarQueue) == 0 {
			b.avatarWorker = false
			b.mu.Unlock()
			return
		}
		job := b.avatarQueue[0]
		b.avatarQueue = b.avatarQueue[1:]
		client := b.client
		b.mu.Unlock()
		target, valid := whatsappAvatarTarget(job.conversation)
		var info *types.ProfilePictureInfo
		var err error
		if job.ctx.Err() == nil && valid && client != nil {
			info, err = client.GetProfilePictureInfo(job.ctx, target, &whatsmeow.GetProfilePictureParams{Preview: true})
		}
		var imageData []byte
		if err == nil && info != nil && info.URL != "" && b.downloadAvatarURL != nil {
			imageData, err = b.downloadAvatarURL(job.ctx, info.URL)
		}

		b.mu.Lock()
		if b.avatarPending[job.conversation] == job.gen {
			delete(b.avatarPending, job.conversation)
		}
		refreshAgain := false
		if b.gen == job.gen {
			b.avatarAttempted[job.conversation] = true
			current, currentExists := b.convs[job.conversation]
			if b.avatarEpoch[job.conversation] == job.epoch && err == nil && info != nil && len(imageData) > 0 && currentExists && job.ctx.Err() == nil {
				avatarPath := whatsappAvatarPath(b.paths.WhatsAppMediaDir(), job.conversation, imageData)
				path := avatarPath
				if writeErr := appStore.WritePrivateFile(path, imageData); writeErr == nil {
					if isWhatsAppAvatarPath(b.paths.WhatsAppMediaDir(), current.AvatarPath) && current.AvatarPath != path {
						_ = os.Remove(current.AvatarPath)
					}
					if pruneErr := appStore.PruneMedia(b.paths.WhatsAppMediaDir(), path); pruneErr != nil {
						b.log.Warn().Err(pruneErr).Msg("Could not trim WhatsApp media cache")
					}
					current.AvatarPath = path
					b.convs[job.conversation] = current
					b.saveStoreLocked()
					b.emitLocked(wire.EventConversation, current)
				}
			}
			if b.avatarRefresh[job.conversation] {
				delete(b.avatarRefresh, job.conversation)
				delete(b.avatarAttempted, job.conversation)
				refreshAgain = true
			}
		}
		b.mu.Unlock()
		if refreshAgain {
			b.queueAvatarFetchesMode(job.gen, []string{job.conversation}, true)
		}

		select {
		case <-job.ctx.Done():
			continue
		case <-time.After(100 * time.Millisecond):
		}
	}
}
