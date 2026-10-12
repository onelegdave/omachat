// Package boundedfile limits the physical extent of a temporary download file.
package boundedfile

import (
	"errors"
	"io"
	"os"
	"sync"
)

var ErrLimit = errors.New("inbound media exceeds maximum allowed size")

// File intentionally does not embed os.File. Promoted ReadFrom and WriteString
// methods would let io.Copy and source WriterTo implementations bypass Write.
// The caller owns and closes the fresh, non-append temporary file passed to New.
type File struct {
	mu   sync.Mutex
	file *os.File
	max  int64
}

func New(file *os.File, max int64) *File { return &File{file: file, max: max} }

func (f *File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Read(p)
}

func (f *File) ReadAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.ReadAt(p, off)
}

func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pos, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if !f.fits(pos, int64(len(p))) {
		return 0, ErrLimit
	}
	return f.file.Write(p)
}

func (f *File) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.fits(off, int64(len(p))) {
		return 0, ErrLimit
	}
	return f.file.WriteAt(p, off)
}

func (f *File) fits(off, length int64) bool {
	// Subtraction after checking the offset avoids signed overflow in off+len.
	return off >= 0 && off <= f.max && length <= f.max-off
}

func (f *File) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var base int64
	var err error
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base, err = f.file.Seek(0, io.SeekCurrent)
	case io.SeekEnd:
		var stat os.FileInfo
		stat, err = f.file.Stat()
		if err == nil {
			base = stat.Size()
		}
	default:
		return 0, errors.New("invalid seek origin")
	}
	if err != nil {
		return 0, err
	}
	if base < 0 || base > f.max || offset < -base || offset > f.max-base {
		return 0, ErrLimit
	}
	return f.file.Seek(base+offset, io.SeekStart)
}

func (f *File) Truncate(size int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if size < 0 || size > f.max {
		return ErrLimit
	}
	return f.file.Truncate(size)
}

func (f *File) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.file.Stat()
}
