package whatsapp

import (
	"github.com/onelegdave/omachat/internal/boundedfile"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

var errInboundMediaTooLarge = boundedfile.ErrLimit

func downloadableMedia(msg *waE2E.Message) (whatsmeow.DownloadableMessage, uint64) {
	inner, _ := unwrapMessage(msg)
	if inner == nil {
		return nil, 0
	}
	if im := inner.GetImageMessage(); im != nil {
		return im, im.GetFileLength()
	}
	if vm := inner.GetVideoMessage(); vm != nil {
		return vm, vm.GetFileLength()
	}
	if am := inner.GetAudioMessage(); am != nil {
		return am, am.GetFileLength()
	}
	if dm := inner.GetDocumentMessage(); dm != nil {
		return dm, dm.GetFileLength()
	}
	if sm := inner.GetStickerMessage(); sm != nil {
		return sm, sm.GetFileLength()
	}
	return nil, 0
}

var _ whatsmeow.File = (*boundedfile.File)(nil)
