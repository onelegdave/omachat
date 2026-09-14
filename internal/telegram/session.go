package telegram

import (
	"context"
	"os"

	"github.com/gotd/td/session"

	appStore "github.com/onelegdave/omachat/internal/store"
)

var _ session.Storage = (*FileSessionStorage)(nil)

// FileSessionStorage implements session.Storage using OmaChat's atomic private file helpers.
type FileSessionStorage struct {
	path string
}

// NewFileSessionStorage creates a session.Storage backed by path.
func NewFileSessionStorage(path string) *FileSessionStorage {
	return &FileSessionStorage{path: path}
}

// LoadSession loads session data from disk. If the file does not exist or is empty,
// it returns session.ErrNotFound.
func (s *FileSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
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
	return appStore.WritePrivateFile(s.path, data)
}
