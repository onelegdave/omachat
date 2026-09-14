package whatsapp

import (
	"errors"
	"io"
	"os"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

var errInboundMediaTooLarge = errors.New("inbound media exceeds maximum allowed size")

type cappedFile struct {
	*os.File
	max int64
	n   int64
}

func (f *cappedFile) Write(p []byte) (int, error) {
	if f.n+int64(len(p)) > f.max {
		return 0, errInboundMediaTooLarge
	}
	n, err := f.File.Write(p)
	f.n += int64(n)
	return n, err
}

func (f *cappedFile) WriteAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if end > f.max {
		return 0, errInboundMediaTooLarge
	}
	n, err := f.File.WriteAt(p, off)
	if off+int64(n) > f.n {
		f.n = off + int64(n)
	}
	return n, err
}

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
	return nil, 0
}

var _ whatsmeow.File = (*cappedFile)(nil)
var _ io.Writer = (*cappedFile)(nil)
