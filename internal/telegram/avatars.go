package telegram

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/tg"
	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

const (
	telegramAvatarPrefix   = "tg-avatar:"
	maxTelegramAvatarBytes = 5 * 1024 * 1024
)

func addAvatarReference(keys map[int64]string, refs map[string]tg.InputFileLocationClass, id int64, peer tg.InputPeerClass, photoID int64) {
	if peer == nil || photoID == 0 {
		return
	}
	key := fmt.Sprintf("%s%d:%d", telegramAvatarPrefix, id, photoID)
	keys[id] = key
	refs[key] = &tg.InputPeerPhotoFileLocation{Peer: peer, PhotoID: photoID}
}

func cachedAvatar(dir, key string) string {
	path := filepath.Join(dir, safeMediaName(key)+".jpg")
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxTelegramAvatarBytes {
		return path
	}
	return ""
}

type avatarJob struct {
	conversationID string
	key            string
}

func (b *Backend) queueAvatars(cli Client, epoch uint64) {
	media, ok := cli.(MediaClient)
	if !ok || b.paths == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopping || b.epoch != epoch {
		return
	}
	jobs := make(chan avatarJob, len(b.avatarKeys))
	for id, key := range b.avatarKeys {
		conv, exists := b.convs[id]
		if !exists || conv.AvatarPath != "" || b.avatarPending[key] {
			continue
		}
		b.avatarPending[key] = true
		jobs <- avatarJob{conversationID: id, key: key}
	}
	close(jobs)
	workers := min(2, len(jobs))
	account := b.accountCtx
	b.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer b.wg.Done()
			for job := range jobs {
				b.fetchAvatar(account, media, epoch, job)
			}
		}()
	}
}

func (b *Backend) fetchAvatar(account context.Context, media MediaClient, epoch uint64, job avatarJob) {
	ctx, cancel := context.WithTimeout(account, 15*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return
	}
	path, err := media.DownloadMedia(ctx, job.key, b.paths.TelegramMediaDir())
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.epoch != epoch || b.stopping {
		return
	}
	delete(b.avatarPending, job.key)
	if err != nil || ctx.Err() != nil || b.avatarKeys[job.conversationID] != job.key {
		return
	}
	conv, exists := b.convs[job.conversationID]
	if !exists || path == "" {
		return
	}
	conv.AvatarPath = path
	b.convs[conv.ID] = conv
	if err := appStore.PruneMedia(b.paths.TelegramMediaDir(), path); err != nil {
		b.log.Warn().Err(err).Msg("Could not trim Telegram avatar cache")
	}
	if err := b.saveLocked(); err != nil {
		b.log.Warn().Err(err).Msg("Could not save Telegram avatar")
	}
	// Publish under the account lock so unpairing cannot be overtaken by an old photo.
	if b.publish != nil {
		b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkTelegram, Data: conv})
	}
}
