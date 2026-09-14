package telegram

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

type storedData struct {
	Conversations map[string]wire.Conversation `json:"conversations"`
	Order         []string                     `json:"order"`
	Messages      map[string][]wire.Message    `json:"messages"`
}

func emptyStoredData() storedData {
	return storedData{Conversations: map[string]wire.Conversation{}, Order: []string{}, Messages: map[string][]wire.Message{}}
}

func loadStoredData(path string) storedData {
	out := emptyStoredData()
	b, err := os.ReadFile(path)
	if err != nil || errors.Is(err, os.ErrNotExist) {
		return out
	}
	if json.Unmarshal(b, &out) != nil {
		return emptyStoredData()
	}
	if out.Conversations == nil {
		out.Conversations = map[string]wire.Conversation{}
	}
	if out.Order == nil {
		out.Order = []string{}
	}
	if out.Messages == nil {
		out.Messages = map[string][]wire.Message{}
	}
	return out
}

func saveStoredData(path string, data storedData) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return store.WritePrivateJSON(path, b)
}
