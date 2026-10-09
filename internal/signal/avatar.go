package signal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	maxSignalAvatarBytes     = 4 << 20
	maxSignalAvatarDimension = 2048
)

func (b *Backend) fetchAvatar(ctx context.Context, conversationID string) error {
	b.mu.RLock()
	account, client, conversation := b.account, b.client, b.convs[conversationID]
	_, exists := b.convs[conversationID]
	b.mu.RUnlock()
	if account == "" || !exists {
		return errors.New("Signal avatar target is unavailable")
	}
	if b.paths == nil {
		return errors.New("Signal avatar storage is unavailable")
	}
	params := map[string]any{"account": account}
	switch {
	case strings.HasPrefix(conversationID, directPrefix):
		params["profile"] = strings.TrimPrefix(conversationID, directPrefix)
	case strings.HasPrefix(conversationID, groupPrefix):
		params["groupId"] = strings.TrimPrefix(conversationID, groupPrefix)
	default:
		return errors.New("invalid Signal avatar target")
	}
	var result json.RawMessage
	if err := client.Call(ctx, "getAvatar", params, &result); err != nil {
		return err
	}
	encoded, err := signalAvatarPayload(result)
	if err != nil {
		return err
	}
	if encoded == "" {
		b.clearAvatarPath(conversationID, account)
		return nil
	}
	imageData, err := decodeSignalAvatar(encoded)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(imageData)
	path := filepath.Join(b.paths.SignalMediaDir(), "avatar-"+hex.EncodeToString(sum[:12])+".img")
	if err := writeSignalAvatar(path, imageData); err != nil {
		return err
	}

	b.mu.Lock()
	current, stillExists := b.convs[conversationID]
	currentAccount := b.account == account
	if stillExists && currentAccount {
		current.AvatarPath = path
		b.convs[conversationID] = current
		_ = b.saveLocked()
		conversation = current
	}
	b.mu.Unlock()
	if !stillExists || !currentAccount {
		return nil
	}
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conversation})
	}
	if err := appStore.PruneMedia(b.paths.SignalMediaDir(), path); err != nil {
		b.log.Warn().Err(err).Msg("Could not trim Signal media")
	}
	return nil
}

func (b *Backend) clearAvatarPath(conversationID, account string) {
	b.mu.Lock()
	conversation, exists := b.convs[conversationID]
	if !exists || b.account != account || conversation.AvatarPath == "" {
		b.mu.Unlock()
		return
	}
	conversation.AvatarPath = ""
	b.convs[conversationID] = conversation
	_ = b.saveLocked()
	b.mu.Unlock()
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkSignal, Data: conversation})
	}
}

func signalAvatarPayload(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var encoded string
	if raw[0] == '"' {
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return "", errors.New("invalid Signal avatar response")
		}
		return encoded, nil
	}
	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", errors.New("invalid Signal avatar response")
	}
	return result.Data, nil
}

func (b *Backend) queueAvatarFetches() {
	b.mu.Lock()
	ctx := b.avatarCtx
	if ctx == nil || ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	for _, id := range b.order {
		if !b.avatarPending[id] {
			b.avatarPending[id] = true
			b.avatarQueue = append(b.avatarQueue, id)
		}
	}
	if !b.avatarWorker && len(b.avatarQueue) > 0 {
		b.avatarWorker = true
		go b.runAvatarQueue()
	}
	b.mu.Unlock()
}

func (b *Backend) runAvatarQueue() {
	for {
		b.mu.Lock()
		ctx := b.avatarCtx
		if ctx == nil || ctx.Err() != nil {
			for _, id := range b.avatarQueue {
				delete(b.avatarPending, id)
			}
			b.avatarQueue = nil
			b.avatarWorker = false
			b.mu.Unlock()
			return
		}
		if len(b.avatarQueue) == 0 {
			b.avatarWorker = false
			b.mu.Unlock()
			return
		}
		id := b.avatarQueue[0]
		b.avatarQueue = b.avatarQueue[1:]
		b.mu.Unlock()

		requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		if err := b.fetchAvatar(requestCtx, id); err != nil && ctx.Err() == nil {
			b.log.Warn().Msg("Could not retrieve Signal avatar")
		}
		cancel()
		b.mu.Lock()
		delete(b.avatarPending, id)
		b.mu.Unlock()
	}
}

func decodeSignalAvatar(encoded string) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(maxSignalAvatarBytes) {
		return nil, errors.New("Signal avatar exceeds size limit")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data) > maxSignalAvatarBytes {
		return nil, errors.New("invalid Signal avatar data")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "gif") || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxSignalAvatarDimension || cfg.Height > maxSignalAvatarDimension {
		return nil, errors.New("invalid Signal avatar image")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return nil, errors.New("invalid Signal avatar image")
	}
	return data, nil
}

func writeSignalAvatar(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".signal-avatar-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write Signal avatar: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
