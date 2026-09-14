package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/wire"
)

// maxUploadBytes guards against sending something the carrier will reject
// outright. MMS limits are far lower than this, but RCS tolerates more, and
// the phone gives a clearer error than a truncated upload would.
const maxUploadBytes = 25 << 20

// SendMedia uploads a local file and sends it to a conversation, optionally
// with a caption.
func (d *Daemon) SendMedia(ctx context.Context, p wire.SendMediaParams) (*wire.SendMediaResult, error) {
	c, err := d.requireClient()
	if err != nil {
		return nil, err
	}
	if p.Path == "" {
		return nil, errors.New("no file given")
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
	info, err := os.Stat(p.Path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("that is a directory, not a file")
	}
	if info.Size() > maxUploadBytes {
		return nil, fmt.Errorf("file is too large (%d MB); the limit is %d MB",
			info.Size()>>20, maxUploadBytes>>20)
	}

	// Read through a bounded reader rather than trusting the Stat above: the
	// size check and the read are two separate observations of the file, and
	// the limit is what decides how much memory this allocates.
	f, err := os.Open(p.Path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxUploadBytes+1))
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(data) > maxUploadBytes {
		return nil, fmt.Errorf("file is too large; the limit is %d MB", maxUploadBytes>>20)
	}
	if len(data) == 0 {
		return nil, errors.New("file is empty")
	}

	fileName := filepath.Base(p.Path)
	mime := detectMime(data, fileName)

	media, err := withAuthRetry(ctx, d, func() (*gmproto.MediaContent, error) {
		return c.UploadMedia(data, fileName, mime)
	})
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}

	result, err := sendMediaMessages(ctx, p, conv.OutgoingID, tmpID, media, func(ctx context.Context, req *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error) {
		return withAuthRetry(ctx, d, func() (*gmproto.SendMessageResponse, error) {
			return c.SendMessage(ctx, req)
		})
	})
	if err != nil {
		return nil, err
	}

	// A capture that has been sent has done its job. Without this every voice
	// note and webcam photo ever sent stays in the cache for good -- the panel
	// only deletes the ones the user cancels.
	if err := d.DiscardCapture(p.Path); err != nil {
		d.log.Debug().Err(err).Msg("Sent file was not one of ours to delete")
	}

	d.log.Info().
		Str("conversation", p.ConversationID).
		Str("mime", mime).
		Int("bytes", len(data)).
		Msg("Submitted media")
	return result, nil
}

// The phone accepts a media+text MessageInfo list but can silently discard
// the media and deliver only the text. Match Messages for web by submitting
// one part per request. Submit the attachment first so its failure cannot
// leave an orphan caption, and report any later caption failure separately.
func sendMediaMessages(ctx context.Context, p wire.SendMediaParams, participantID, tmpID string, media *gmproto.MediaContent,
	send func(context.Context, *gmproto.SendMessageRequest) (*gmproto.SendMessageResponse, error),
) (*wire.SendMediaResult, error) {
	submit := func(id string, part *gmproto.MessageInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, err := send(ctx, &gmproto.SendMessageRequest{
			ConversationID: p.ConversationID,
			TmpID:          id,
			MessagePayload: &gmproto.MessagePayload{
				TmpID: id, TmpID2: id, ConversationID: p.ConversationID,
				ParticipantID: participantID, MessageInfo: []*gmproto.MessageInfo{part},
			},
		})
		if err != nil {
			return fmt.Errorf("send: %w", err)
		}
		if status := resp.GetStatus(); status != gmproto.SendMessageResponse_SUCCESS {
			return fmt.Errorf("send rejected: %s", status)
		}
		return nil
	}
	if err := submit(tmpID, &gmproto.MessageInfo{
		Data: &gmproto.MessageInfo_MediaContent{MediaContent: media},
	}); err != nil {
		return nil, err
	}
	message := &wire.Message{
		ID:             tmpID,
		TmpID:          tmpID,
		Provisional:    true,
		Timestamp:      time.Now().UnixMicro(),
		ConversationID: p.ConversationID,
		FromMe:         true,
		Pending:        true,
		Attachments: []wire.Attachment{{
			Key:      media.GetMediaID(),
			MediaID:  media.GetMediaID(),
			Name:     media.GetMediaName(),
			MimeType: media.GetMimeType(),
			Size:     media.GetSize(),
			IsImage:  isImageMime(media.GetMimeType()),
			IsGif:    isGifMime(media.GetMimeType(), media.GetMediaName()),
			IsAudio:  isAudioMime(media.GetMimeType()),
			IsVideo:  isVideoMime(media.GetMimeType()),
		}},
	}
	result := &wire.SendMediaResult{Message: message}
	caption := strings.TrimSpace(p.Caption)
	if caption == "" {
		return result, nil
	}
	// Keep retries of one attachment tied to one independent caption identity.
	captionID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("omachat-caption:"+tmpID)).String()
	if err := submit(captionID, &gmproto.MessageInfo{
		Data: &gmproto.MessageInfo_MessageContent{MessageContent: &gmproto.MessageContent{Content: caption}},
	}); err != nil {
		result.CaptionError = err.Error()
		return result, nil
	}
	result.CaptionMessage = &wire.Message{
		ID: captionID, TmpID: captionID, ConversationID: p.ConversationID,
		Text: caption, Timestamp: time.Now().UnixMicro(),
		FromMe: true, Pending: true, Provisional: true,
	}
	return result, nil
}

// audioMimeByExt covers the container formats libgm knows how to type. The
// extension decides these, not the sniffer: an .m4a is an MP4 container, so
// content sniffing calls it video/mp4 (or fails to identify it at all), and
// uploading a voice note as video would show the recipient a video message.
var audioMimeByExt = map[string]string{
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	".amr":  "audio/amr",
	".3ga":  "audio/3gpp",
}

// detectMime prefers sniffing the content, since a wrong extension would make
// the phone reject the upload. Audio is the exception -- see audioMimeByExt.
func detectMime(data []byte, fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if mime, ok := audioMimeByExt[ext]; ok {
		return mime
	}
	if mime := http.DetectContentType(data); mime != "" && mime != "application/octet-stream" {
		// DetectContentType appends charset for text types.
		return strings.TrimSpace(strings.Split(mime, ";")[0])
	}
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	default:
		return "application/octet-stream"
	}
}
