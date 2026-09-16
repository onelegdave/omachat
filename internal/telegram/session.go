package telegram

import (
	"context"
	"os"
	"sync"

	"github.com/gotd/td/session"

	appStore "github.com/onelegdave/omachat/internal/store"
)

var _ session.Storage = (*FileSessionStorage)(nil)

// FileSessionStorage implements session.Storage using OmaChat's atomic private file helpers.
type FileSessionStorage struct {
	path   string
	mu     sync.Mutex
	closed bool
}

// NewFileSessionStorage creates a session.Storage backed by path.
func NewFileSessionStorage(path string) *FileSessionStorage {
	return &FileSessionStorage{path: path}
}

// LoadSession loads session data from disk. If the file does not exist or is empty,
// it returns session.ErrNotFound.
func (s *FileSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, session.ErrNotFound
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, session.ErrNotFound
	}
	return data, nil
}

// StoreSession stores session data atomically at mode 0600.
func (s *FileSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return appStore.WritePrivateFile(s.path, data)
}

// close retires this account writer before the backend deletes credentials.
func (s *FileSessionStorage) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}
