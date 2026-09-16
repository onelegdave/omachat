package messenger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	appStore "github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
	"go.mau.fi/mautrix-meta/pkg/messagix/httpclient"
	"go.mau.fi/mautrix-meta/pkg/messagix/methods"
	"go.mau.fi/mautrix-meta/pkg/messagix/socket"
	"go.mau.fi/mautrix-meta/pkg/messagix/table"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waConsumerApplication"
	"go.mau.fi/whatsmeow/proto/waMediaTransport"
	"google.golang.org/protobuf/proto"
)

const (
	maxMessengerInboundBytes  int64 = 25 << 20
	maxMessengerOutboundBytes int64 = 25 << 20
	maxMessengerAvatarBytes   int64 = 5 << 20
)

type messengerMedia struct {
	key, remoteURL, name, mime, path string
	size, width, height              int64
	isImage, isGIF, isAudio, isVideo bool
	fbTransport                      *waMediaTransport.WAMediaTransport_Integral
	fbType                           whatsmeow.MediaType
}

func (m *messengerMedia) attachment() wire.Attachment {
	return wire.Attachment{Key: m.key, MediaID: m.key, Name: m.name, MimeType: m.mime, Size: m.size, Width: m.width, Height: m.height, IsImage: m.isImage, IsGif: m.isGIF, IsAudio: m.isAudio, IsVideo: m.isVideo, Path: m.path}
}

func messengerMediaKey(messageID, attachmentID string, index int64) string {
	sum := sha256.Sum256([]byte(messageID + "\x00" + attachmentID + "\x00" + strconv.FormatInt(index, 10)))
	return "fb-" + hex.EncodeToString(sum[:16])
}

func classifyMessengerMedia(m *messengerMedia, typ table.AttachmentType) {
	lower := strings.ToLower(m.mime)
	ext := messengerMediaExtension(m)
	baseName := strings.ToLower(filepath.Base(m.name))
	videoContainer := strings.HasPrefix(lower, "video/") || ext == ".f4v" || ext == ".mp4" || ext == ".m4v" || ext == ".mov" || ext == ".webm" || ext == ".mkv" || ext == ".3gp"
	m.isGIF = typ == table.AttachmentTypeAnimatedImage || strings.Contains(lower, "gif") || ext == ".gif" || strings.HasPrefix(baseName, "gif-") || strings.HasPrefix(baseName, "gif_")
	m.isImage = typ == table.AttachmentTypeImage || typ == table.AttachmentTypeSticker || typ == table.AttachmentTypeSelfieSticker || typ == table.AttachmentTypeThirdPartySticker || (m.isGIF && !videoContainer) || strings.HasPrefix(lower, "image/")
	m.isAudio = typ == table.AttachmentTypeAudio || typ == table.AttachmentTypeSoundBite || strings.HasPrefix(lower, "audio/")
	m.isVideo = typ == table.AttachmentTypeVideo || videoContainer
	if m.isVideo {
		m.isImage = false
	}
}

func messengerMediaExtension(m *messengerMedia) string {
	for _, value := range []string{m.name, m.path, m.remoteURL} {
		if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
			value = parsed.Path
		}
		if ext := strings.ToLower(filepath.Ext(value)); ext != "" {
			return ext
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (b *Backend) rememberRemoteLocked(messageID, attachmentID string, index int64, m *messengerMedia, typ table.AttachmentType) wire.Attachment {
	m.key = messengerMediaKey(messageID, attachmentID, index)
	classifyMessengerMedia(m, typ)
	b.media[m.key] = m
	return m.attachment()
}

func (b *Backend) tableAttachmentsLocked(tbl *table.LSTable) map[string][]wire.Attachment {
	out := make(map[string][]wire.Attachment)
	for _, a := range tbl.LSInsertAttachment {
		m := &messengerMedia{remoteURL: firstNonEmpty(a.PlayableAudioURL, a.PlayableUrl, a.ImageUrl, a.PreviewUrl), name: a.Filename, mime: firstNonEmpty(a.AttachmentMimeType, a.PlayableUrlMimeType, a.ImageUrlMimeType, a.PreviewUrlMimeType), size: a.Filesize, width: a.PreviewWidth, height: a.PreviewHeight}
		if m.remoteURL != "" {
			out[a.MessageId] = append(out[a.MessageId], b.rememberRemoteLocked(a.MessageId, a.AttachmentFbid, a.AttachmentIndex, m, a.AttachmentType))
		}
	}
	for _, a := range tbl.LSInsertBlobAttachment {
		m := &messengerMedia{remoteURL: firstNonEmpty(a.PlayableUrl, a.PreviewUrl), name: a.Filename, mime: firstNonEmpty(a.AttachmentMimeType, a.PlayableUrlMimeType, a.PreviewUrlMimeType), size: a.Filesize, width: a.PreviewWidth, height: a.PreviewHeight}
		if m.remoteURL != "" {
			out[a.MessageId] = append(out[a.MessageId], b.rememberRemoteLocked(a.MessageId, a.AttachmentFbid, a.AttachmentIndex, m, a.AttachmentType))
		}
	}
	for _, a := range tbl.LSInsertStickerAttachment {
		m := &messengerMedia{remoteURL: firstNonEmpty(a.PlayableUrl, a.ImageUrl, a.PreviewUrl), name: "Sticker", mime: firstNonEmpty(a.PlayableUrlMimeType, a.ImageUrlMimeType, a.PreviewUrlMimeType), width: a.PreviewWidth, height: a.PreviewHeight}
		if m.remoteURL != "" {
			out[a.MessageId] = append(out[a.MessageId], b.rememberRemoteLocked(a.MessageId, a.AttachmentFbid, a.AttachmentIndex, m, table.AttachmentTypeSticker))
		}
	}
	for _, a := range tbl.LSInsertXmaAttachment {
		m := &messengerMedia{remoteURL: firstNonEmpty(a.PlayableUrl, a.ImageUrl, a.PreviewUrl), name: a.Filename, mime: firstNonEmpty(a.PlayableUrlMimeType, a.PreviewUrlMimeType), size: a.Filesize, width: a.PreviewWidth, height: a.PreviewHeight}
		if m.remoteURL != "" {
			out[a.MessageId] = append(out[a.MessageId], b.rememberRemoteLocked(a.MessageId, a.AttachmentFbid, a.AttachmentIndex, m, a.AttachmentType))
		}
	}
	return out
}

func mediaFromFB(t *waMediaTransport.WAMediaTransport, mt whatsmeow.MediaType, image, gif, audio, video bool, name string) *messengerMedia {
	if t == nil || t.GetIntegral() == nil {
		return nil
	}
	m := &messengerMedia{name: name, mime: t.GetAncillary().GetMimetype(), size: int64(t.GetAncillary().GetFileLength()), isImage: image, isGIF: gif, isAudio: audio, isVideo: video, fbTransport: t.GetIntegral(), fbType: mt}
	if gif {
		m.isImage = true
	}
	return m
}

func mediaExtension(mimeType, name string) string {
	if ext := strings.ToLower(filepath.Ext(name)); len(ext) > 1 && len(ext) <= 10 {
		return ext
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		return exts[0]
	}
	switch {
	case strings.Contains(mimeType, "jpeg"):
		return ".jpg"
	case strings.Contains(mimeType, "png"):
		return ".png"
	case strings.Contains(mimeType, "gif"):
		return ".gif"
	case strings.Contains(mimeType, "webp"):
		return ".webp"
	case strings.HasPrefix(mimeType, "audio/"):
		return ".audio"
	case strings.HasPrefix(mimeType, "video/"):
		return ".video"
	}
	return ".bin"
}

func (b *Backend) Media(ctx context.Context, p wire.MediaParams) (wire.MediaResult, error) {
	key := p.Key
	if key == "" {
		key = p.MediaID
	}
	if key == "" {
		return wire.MediaResult{}, errors.New("empty media key")
	}
	b.mu.RLock()
	stored := b.media[key]
	e2ee := b.e2eeClient
	b.mu.RUnlock()
	if stored == nil {
		return wire.MediaResult{}, errors.New("unauthorized or unknown Messenger media key")
	}
	record := *stored
	if record.path != "" {
		if fi, err := os.Lstat(record.path); err == nil && fi.Mode().IsRegular() && fi.Mode()&os.ModeSymlink == 0 {
			return wire.MediaResult{Key: key, Path: record.path}, nil
		}
	}
	if record.size > maxMessengerInboundBytes {
		return wire.MediaResult{}, errors.New("Messenger attachment exceeds the 25 MB download limit")
	}
	dir := b.paths.MessengerMediaDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return wire.MediaResult{}, err
	}
	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return wire.MediaResult{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limited := &limitedFile{File: tmp, remaining: maxMessengerInboundBytes}
	if record.fbTransport != nil {
		if e2ee == nil {
			_ = tmp.Close()
			return wire.MediaResult{}, errors.New("Messenger encrypted transport is not connected")
		}
		err = e2ee.DownloadFBToFile(ctx, record.fbTransport, record.fbType, limited)
	} else {
		err = downloadMessengerURL(ctx, record.remoteURL, limited, maxMessengerInboundBytes)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return wire.MediaResult{}, err
	}
	path := filepath.Join(dir, key+mediaExtension(record.mime, record.name))
	if err = os.Rename(tmpName, path); err != nil {
		return wire.MediaResult{}, err
	}
	_ = os.Chmod(path, 0o600)
	b.mu.Lock()
	if current := b.media[key]; current == stored {
		current.path = path
	}
	b.mu.Unlock()
	if err := appStore.PruneMedia(dir, path); err != nil {
		b.log.Warn().Err(err).Msg("Could not trim Messenger media")
	}
	return wire.MediaResult{Key: key, Path: path}, nil
}

type limitedFile struct {
	*os.File
	remaining int64
}

func (f *limitedFile) Write(p []byte) (int, error) {
	if int64(len(p)) > f.remaining {
		return 0, errors.New("Messenger attachment exceeds the 25 MB download limit")
	}
	n, err := f.File.Write(p)
	f.remaining -= int64(n)
	return n, err
}

func downloadMessengerURL(ctx context.Context, raw string, dst io.Writer, max int64) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("invalid Messenger media URL")
	}
	if u.Port() != "" && u.Port() != "443" {
		return errors.New("invalid Messenger media URL port")
	}
	transport := &http.Transport{DialContext: dialPublicHTTPS}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return errors.New("unsafe Messenger media redirect")
		}
		if req.URL.Port() != "" && req.URL.Port() != "443" {
			return errors.New("unsafe Messenger media redirect port")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Messenger media download returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > max {
		return errors.New("Messenger attachment exceeds the download limit")
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return errors.New("Messenger attachment exceeds the download limit")
	}
	return nil
}

func dialPublicHTTPS(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, errors.New("invalid Messenger media address")
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, candidate := range resolved {
		ip := candidate.IP
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return nil, errors.New("Messenger media host did not resolve to a public address")
}

func openMessengerUpload(path string) ([]byte, os.FileInfo, string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, nil, "", errors.New("attachment must be a regular file")
	}
	if info.Size() > maxMessengerOutboundBytes {
		return nil, nil, "", errors.New("Messenger attachment exceeds the 25 MB upload limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxMessengerOutboundBytes+1))
	if err != nil {
		return nil, nil, "", err
	}
	if int64(len(data)) > maxMessengerOutboundBytes {
		return nil, nil, "", errors.New("Messenger attachment exceeds the 25 MB upload limit")
	}
	mimeType := http.DetectContentType(data[:min(len(data), 512)])
	return data, info, mimeType, nil
}

func messengerOutboundAudio(path, mimeType string) bool {
	return strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(strings.ToLower(filepath.Base(path)), "voice-")
}

func (b *Backend) SendMedia(ctx context.Context, p wire.SendMediaParams) (wire.SendMediaResult, error) {
	thread, err := strconv.ParseInt(p.ConversationID, 10, 64)
	if err != nil {
		return wire.SendMediaResult{}, errors.New("invalid Messenger conversation ID")
	}
	data, info, mimeType, err := openMessengerUpload(p.Path)
	if err != nil {
		return wire.SendMediaResult{}, err
	}
	b.mu.RLock()
	cli, e2ee, jid, self := b.client, b.e2eeClient, b.threadToJID[thread], b.selfID
	b.mu.RUnlock()
	if cli == nil {
		return wire.SendMediaResult{}, ErrNotConfigured
	}
	id := strconv.FormatInt(methods.GenerateEpochID(), 10)
	ts := time.Now()
	caption := strings.TrimSpace(p.Caption)
	mainText := caption
	var captionMessage *wire.Message
	if !jid.IsEmpty() {
		if e2ee == nil {
			return wire.SendMediaResult{}, errors.New("Messenger encrypted transport is not connected")
		}
		mt := whatsmeow.MediaDocument
		isImage := strings.HasPrefix(mimeType, "image/")
		isAudio := messengerOutboundAudio(p.Path, mimeType)
		if isImage {
			mt = whatsmeow.MediaImage
		} else if isAudio {
			mt = whatsmeow.MediaAudio
		}
		upload, e := e2ee.Upload(ctx, data, mt)
		if e != nil {
			return wire.SendMediaResult{}, e
		}
		transport := &waMediaTransport.WAMediaTransport{Integral: &waMediaTransport.WAMediaTransport_Integral{FileSHA256: upload.FileSHA256, MediaKey: upload.MediaKey, FileEncSHA256: upload.FileEncSHA256, DirectPath: proto.String(upload.DirectPath)}, Ancillary: &waMediaTransport.WAMediaTransport_Ancillary{FileLength: proto.Uint64(upload.FileLength), Mimetype: proto.String(mimeType), ObjectID: proto.String(upload.ObjectID)}}
		var content waConsumerApplication.ConsumerApplication_Content_Content
		if isImage {
			im := &waConsumerApplication.ConsumerApplication_ImageMessage{Caption: &waCommon.MessageText{Text: proto.String(caption)}}
			if e = im.Set(&waMediaTransport.ImageTransport{Integral: &waMediaTransport.ImageTransport_Integral{Transport: transport}}); e != nil {
				return wire.SendMediaResult{}, e
			}
			content = &waConsumerApplication.ConsumerApplication_Content_ImageMessage{ImageMessage: im}
		} else if isAudio {
			mainText = ""
			am := &waConsumerApplication.ConsumerApplication_AudioMessage{PTT: proto.Bool(true)}
			if e = am.Set(&waMediaTransport.AudioTransport{Integral: &waMediaTransport.AudioTransport_Integral{Transport: transport}}); e != nil {
				return wire.SendMediaResult{}, e
			}
			content = &waConsumerApplication.ConsumerApplication_Content_AudioMessage{AudioMessage: am}
		} else {
			mainText = ""
			dm := &waConsumerApplication.ConsumerApplication_DocumentMessage{FileName: proto.String(filepath.Base(p.Path))}
			if e = dm.Set(&waMediaTransport.DocumentTransport{Integral: &waMediaTransport.DocumentTransport_Integral{Transport: transport}}); e != nil {
				return wire.SendMediaResult{}, e
			}
			content = &waConsumerApplication.ConsumerApplication_Content_DocumentMessage{DocumentMessage: dm}
		}
		payload := &waConsumerApplication.ConsumerApplication{Payload: &waConsumerApplication.ConsumerApplication_Payload{Payload: &waConsumerApplication.ConsumerApplication_Payload_Content{Content: &waConsumerApplication.ConsumerApplication_Content{Content: content}}}}
		resp, e := e2ee.SendFBMessage(ctx, jid, payload, nil, whatsmeow.SendRequestExtra{ID: id})
		if e != nil {
			return wire.SendMediaResult{}, e
		}
		ts = resp.Timestamp
		if !isImage && caption != "" {
			captionMessage, e = b.Send(ctx, wire.SendParams{ConversationID: p.ConversationID, Text: caption})
			if e != nil {
				captionMessage = nil
			}
		}
	} else {
		isAudio := messengerOutboundAudio(p.Path, mimeType)
		upload, e := cli.GetHTTP().SendMercuryUploadRequest(ctx, thread, &httpclient.MercuryUploadMedia{Filename: filepath.Base(p.Path), MimeType: mimeType, MediaData: data, IsVoiceClip: isAudio})
		if e != nil {
			return wire.SendMediaResult{}, e
		}
		if upload.Payload.RealMetadata == nil || upload.Payload.RealMetadata.GetFbId() == 0 {
			return wire.SendMediaResult{}, errors.New("Messenger upload returned no attachment ID")
		}
		resp, e := cli.ExecuteTasks(ctx, &socket.SendMessageTask{ThreadId: thread, Otid: methods.GenerateEpochID(), Source: table.MESSENGER_INBOX, SendType: table.MEDIA, SyncGroup: 1, AttachmentFBIds: []int64{upload.Payload.RealMetadata.GetFbId()}, Text: caption, InitiatingSource: table.FACEBOOK_INBOX})
		if e != nil {
			return wire.SendMediaResult{}, e
		}
		for _, r := range resp.LSReplaceOptimsiticMessage {
			if r.MessageId != "" {
				id = r.MessageId
				break
			}
		}
	}
	key := messengerMediaKey(id, "outgoing", 0)
	isAudio := messengerOutboundAudio(p.Path, mimeType)
	att := wire.Attachment{Key: key, MediaID: key, Name: filepath.Base(p.Path), MimeType: mimeType, Size: info.Size(), IsImage: strings.HasPrefix(mimeType, "image/"), IsGif: strings.Contains(mimeType, "gif"), IsAudio: isAudio, Path: p.Path}
	msg := wire.Message{TmpID: p.TmpID, ID: id, ConversationID: p.ConversationID, Text: mainText, Timestamp: messengerTimeTimestamp(ts), FromMe: true, SenderID: strconv.FormatInt(self, 10), Delivery: wire.DeliverySent, Attachments: []wire.Attachment{att}}
	b.mu.Lock()
	b.media[key] = &messengerMedia{key: key, name: att.Name, mime: mimeType, size: info.Size(), isImage: att.IsImage, isGIF: att.IsGif, isAudio: att.IsAudio, path: p.Path}
	b.addMessageWithAttachmentsLocked(thread, id, mainText, ts.UnixMilli(), self, false, "", msg.Attachments)
	if c, ok := b.convs[p.ConversationID]; ok {
		c.Preview = mainText
		if captionMessage != nil {
			c.Preview = captionMessage.Text
			c.Timestamp = captionMessage.Timestamp
		} else if c.Preview == "" {
			c.Preview = "Attachment"
		}
		c.PreviewMine = true
		if captionMessage == nil {
			c.Timestamp = msg.Timestamp
		}
		b.convs[p.ConversationID] = c
	}
	b.saveStoredMessengerDataLocked()
	b.mu.Unlock()
	b.publishSnapshots()
	result := wire.SendMediaResult{Message: &msg, CaptionMessage: captionMessage}
	if mainText == "" && caption != "" && captionMessage == nil {
		result.CaptionError = "Messenger accepted the file but could not send its caption"
	}
	return result, nil
}

type avatarJob struct{ conversationID, rawURL string }

func (b *Backend) avatarURLLocked(id string, c wire.Conversation) string {
	raw := b.threadAvatars[parseUser(id)]
	if raw == "" && !c.IsGroup {
		contact := parseUser(id)
		if jid := b.threadToJID[contact]; !jid.IsEmpty() {
			contact = parseUser(jid.User)
		}
		raw = b.contactAvatars[contact]
	}
	return raw
}

func (b *Backend) avatarJobsLocked() []avatarJob {
	var jobs []avatarJob
	for id, c := range b.convs {
		raw := b.avatarURLLocked(id, c)
		if raw == "" {
			continue
		}
		if path := b.avatarCache[raw]; path != "" {
			c.AvatarPath = path
			b.convs[id] = c
			continue
		}
		if !b.avatarPending[raw] {
			b.avatarPending[raw] = true
			jobs = append(jobs, avatarJob{id, raw})
		}
	}
	return jobs
}
func (b *Backend) fetchAvatars(jobs []avatarJob) {
	if b.paths == nil {
		return
	}
	b.mu.RLock()
	parent := b.runCtx
	b.mu.RUnlock()
	if parent == nil {
		parent = context.Background()
	}
	for _, job := range jobs {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		dir := b.paths.MessengerMediaDir()
		sum := sha256.Sum256([]byte(job.rawURL))
		tmp, err := os.CreateTemp(dir, ".avatar-*")
		if err == nil {
			err = downloadMessengerURL(ctx, job.rawURL, tmp, maxMessengerAvatarBytes)
			_ = tmp.Close()
		}
		cancel()
		path := ""
		if err == nil {
			path = filepath.Join(dir, "avatar-"+hex.EncodeToString(sum[:12])+".img")
			err = os.Rename(tmp.Name(), path)
			if err == nil {
				_ = os.Chmod(path, 0o600)
			}
		}
		if tmp != nil {
			_ = os.Remove(tmp.Name())
		}
		var updated []wire.Conversation
		b.mu.Lock()
		delete(b.avatarPending, job.rawURL)
		if err == nil {
			b.avatarCache[job.rawURL] = path
			for id, c := range b.convs {
				if b.avatarURLLocked(id, c) == job.rawURL {
					c.AvatarPath = path
					b.convs[id] = c
					updated = append(updated, c)
				}
			}
		}
		b.mu.Unlock()
		if err == nil && b.publish != nil {
			for _, conversation := range updated {
				b.publish(wire.Event{Event: wire.EventConversation, Network: wire.NetworkMessenger, Data: conversation})
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}
