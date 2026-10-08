package whatsapp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/onelegdave/omachat/internal/wire"
)

const (
	sqlitePageBytes                 = 4096
	maxHistoryPageBytes             = 4 * 1024 * 1024
	maxHistoryPageMessages          = 100
	historyPageEntryOverhead        = 512
	historyPageContainerOverhead    = 64 * 1024
	maxHistoryPageAttachments       = 256
	maxHistoryPageReactions         = 256
	maxHistoryIndexFileBytes        = 8 * 1024 * 1024
	historySQLiteSafetyReserveBytes = 1 * 1024 * 1024
)

func historyIndexBudgetBytes(physicalBudget int64) int64 {
	indexBudget := int64(maxHistoryIndexFileBytes)
	if physicalBudget/8 < indexBudget {
		indexBudget = physicalBudget / 8
	}
	return indexBudget
}

func historyDatabaseBudgetBytes(physicalBudget int64) int64 {
	// Reserve three database-sized footprints for SQLite's main database,
	// compaction copy, and rollback journal. The extra fixed reserve covers
	// journal headers, page records, and sector rounding at supported cache sizes.
	remaining := physicalBudget - 2*historyIndexBudgetBytes(physicalBudget) - historySQLiteSafetyReserveBytes
	if remaining <= 0 {
		return 0
	}
	return remaining / 3
}

// historyStore is separate from whatsmeow's credential/device database.
type historyStore struct {
	db       *sql.DB
	maxBytes int64
	budgetMu sync.RWMutex
}

func openHistoryStore(path string, budgetMB int) (*historyStore, error) {
	if budgetMB != 64 && budgetMB != 128 && budgetMB != 256 && budgetMB != 512 {
		budgetMB = 128
	}
	return openHistoryStoreWithBudget(path, int64(budgetMB)*1024*1024)
}

func openHistoryStoreWithBudget(path string, physicalBudget int64) (*historyStore, error) {
	if physicalBudget <= 0 {
		return nil, errors.New("WhatsApp history cache budget must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	indexPath := filepath.Join(filepath.Dir(path), "whatsapp_store.json")
	if err := cleanupHistoryIndexTemps(indexPath); err != nil {
		return nil, fmt.Errorf("clean interrupted WhatsApp history index writes: %w", err)
	}
	maxBytes := historyDatabaseBudgetBytes(physicalBudget)
	if maxBytes < sqlitePageBytes {
		return nil, errors.New("WhatsApp history cache budget is too small for SQLite overhead")
	}
	existingBytes, err := historyDatabaseArtifactBytes(path)
	if err != nil {
		return nil, err
	}
	if existingBytes > 2*maxBytes {
		return nil, fmt.Errorf("existing WhatsApp history database artifacts use %d bytes, above the bounded journal allowance", existingBytes)
	}
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open WhatsApp history store: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*historyStore, error) { _ = db.Close(); return nil, err }
	var pageSize int64
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return fail(fmt.Errorf("read history page size: %w", err))
	}
	if pageSize != sqlitePageBytes {
		return fail(fmt.Errorf("unsupported existing WhatsApp history page size %d", pageSize))
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fail(fmt.Errorf("restrict WhatsApp history database permissions: %w", err))
	}
	pageLimit := maxBytes / pageSize
	if pageLimit < 1 {
		return fail(errors.New("WhatsApp history cache budget is smaller than one SQLite page"))
	}
	var actual int64
	if err := db.QueryRow(`PRAGMA max_page_count=` + fmt.Sprint(pageLimit)).Scan(&actual); err != nil {
		return fail(fmt.Errorf("limit history pages before initialization: %w", err))
	}
	if actual*pageSize > maxBytes {
		return fail(fmt.Errorf("existing WhatsApp history database exceeds its %d-byte database allocation", maxBytes))
	}
	if _, err := db.Exec(`PRAGMA temp_store=MEMORY`); err != nil {
		return fail(fmt.Errorf("configure in-memory SQLite temporary storage: %w", err))
	}
	var tempStore int
	if err := db.QueryRow(`PRAGMA temp_store`).Scan(&tempStore); err != nil {
		return fail(fmt.Errorf("verify SQLite temporary storage mode: %w", err))
	}
	if tempStore != 2 {
		return fail(errors.New("SQLite build does not support in-memory temporary tables"))
	}
	var journalMode string
	if err := db.QueryRow(`PRAGMA journal_mode=DELETE`).Scan(&journalMode); err != nil {
		return fail(fmt.Errorf("set history journal mode: %w", err))
	}
	if strings.ToLower(journalMode) != "delete" {
		return fail(fmt.Errorf("unexpected WhatsApp history journal mode %q", journalMode))
	}
	if _, err := db.Exec(`PRAGMA synchronous=FULL`); err != nil {
		return fail(fmt.Errorf("set history synchronous mode: %w", err))
	}
	var autoVacuum int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		return fail(fmt.Errorf("read history vacuum mode: %w", err))
	}
	if autoVacuum != 1 {
		if autoVacuum == 0 && existingBytes > maxBytes {
			return fail(errors.New("existing WhatsApp history database is too large to convert safely"))
		}
		if _, err := db.Exec(`PRAGMA auto_vacuum=FULL`); err != nil {
			return fail(fmt.Errorf("enable full history vacuum: %w", err))
		}
		if autoVacuum == 0 && existingBytes > 0 {
			if _, err := db.Exec(`VACUUM`); err != nil {
				return fail(fmt.Errorf("initialize full history vacuum: %w", err))
			}
		}
	}
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum); err != nil {
		return fail(fmt.Errorf("verify history vacuum mode: %w", err))
	}
	if autoVacuum != 1 {
		return fail(fmt.Errorf("unexpected WhatsApp history vacuum mode %d", autoVacuum))
	}
	if _, err := db.Exec(`PRAGMA page_size=4096`); err != nil {
		return fail(fmt.Errorf("set history page size: %w", err))
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS history_messages (
		chat_id TEXT NOT NULL,
		message_id TEXT NOT NULL,
		ts INTEGER NOT NULL,
		payload BLOB NOT NULL,
		raw_payload BLOB,
		PRIMARY KEY(chat_id, message_id)
	)`); err != nil {
		return fail(fmt.Errorf("create history table: %w", err))
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS history_messages_cursor ON history_messages(chat_id, ts, message_id)`); err != nil {
		return fail(fmt.Errorf("create history cursor index: %w", err))
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS history_chat_state (
		chat_id TEXT PRIMARY KEY,
		last_viewed INTEGER NOT NULL DEFAULT 0,
		last_evicted INTEGER NOT NULL DEFAULT 0,
		evicted_before INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return fail(fmt.Errorf("create history chat state: %w", err))
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO history_chat_state(chat_id) SELECT DISTINCT chat_id FROM history_messages`); err != nil {
		return fail(fmt.Errorf("migrate history chat state: %w", err))
	}
	if err := db.QueryRow(`PRAGMA max_page_count=` + fmt.Sprint(pageLimit)).Scan(&actual); err != nil {
		return fail(fmt.Errorf("limit history pages: %w", err))
	}
	if actual*sqlitePageBytes > maxBytes {
		return fail(fmt.Errorf("initialized WhatsApp history database exceeds its %d-byte database allocation", maxBytes))
	}
	store := &historyStore{db: db, maxBytes: maxBytes}
	return store, nil
}

func (s *historyStore) maxBytesLimit() int64 {
	s.budgetMu.RLock()
	defer s.budgetMu.RUnlock()
	return s.maxBytes
}
func (s *historyStore) close() error { return s.db.Close() }

func (s *historyStore) put(ctx context.Context, messages []wire.Message, rawByMessage ...map[string][]byte) error {
	if len(messages) == 0 {
		return nil
	}
	perRowLimit := s.maxBytesLimit() / 2
	if pageSafeLimit := int64(maxHistoryPageBytes - historyPageEntryOverhead); perRowLimit > pageSafeLimit {
		perRowLimit = pageSafeLimit
	}
	batchTarget := s.maxBytesLimit() / 4
	if batchTarget > maxHistoryPageBytes {
		batchTarget = maxHistoryPageBytes
	}
	if batchTarget < sqlitePageBytes {
		batchTarget = sqlitePageBytes
	}
	batch := make([]wire.Message, 0, 50)
	var batchBytes int64
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := s.putBatch(ctx, batch, rawByMessage...); err != nil {
			return err
		}
		batch = batch[:0]
		batchBytes = 0
		return nil
	}
	for _, message := range messages {
		if message.ID == "" || strings.TrimSpace(message.ConversationID) == "" {
			continue
		}
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		var raw []byte
		if len(rawByMessage) > 0 {
			raw = rawByMessage[0][message.ID]
			if raw == nil {
				raw = rawByMessage[0][rawMediaKey(message.ConversationID, message.ID)]
			}
		}
		rowBytes := int64(len(payload) + len(raw))
		if rowBytes > perRowLimit {
			return fmt.Errorf("WhatsApp message %q exceeds the safe per-entry history cache budget", message.ID)
		}
		if len(batch) > 0 && (batchBytes+rowBytes > batchTarget || len(batch) >= 50) {
			if err := flush(); err != nil {
				return err
			}
		}
		batch = append(batch, message)
		batchBytes += rowBytes
	}
	return flush()
}

func (s *historyStore) putBatch(ctx context.Context, messages []wire.Message, rawByMessage ...map[string][]byte) error {
	for attempt := 0; attempt < 256; attempt++ {
		err := s.putOnce(ctx, messages, rawByMessage...)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "full") {
			return err
		}
		evicted, evictErr := s.evictOne(ctx)
		if evictErr != nil {
			return fmt.Errorf("history cache is full; eviction failed: %w", evictErr)
		}
		if !evicted {
			return fmt.Errorf("history page exceeds the available cache budget: %w", err)
		}
	}
	return errors.New("history cache remains full after bounded eviction")
}

func (s *historyStore) putOnce(ctx context.Context, messages []wire.Message, rawByMessage ...map[string][]byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO history_messages(chat_id,message_id,ts,payload,raw_payload) VALUES(?,?,?,?,?) ON CONFLICT(chat_id,message_id) DO UPDATE SET ts=excluded.ts,payload=excluded.payload,raw_payload=COALESCE(excluded.raw_payload,history_messages.raw_payload)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, message := range messages {
		if message.ID == "" || strings.TrimSpace(message.ConversationID) == "" {
			continue
		}
		if len(message.Attachments) > maxHistoryPageAttachments || len(message.Reactions) > maxHistoryPageReactions {
			return errors.New("WhatsApp history message exceeds the bounded attachment or reaction count")
		}
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		var raw []byte
		if len(rawByMessage) > 0 {
			raw = rawByMessage[0][message.ID]
			if raw == nil {
				raw = rawByMessage[0][rawMediaKey(message.ConversationID, message.ID)]
			}
		}
		if _, err := stmt.ExecContext(ctx, message.ConversationID, message.ID, message.Timestamp, payload, raw); err != nil {
			return fmt.Errorf("persist WhatsApp history message: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO history_chat_state(chat_id) VALUES(?)`, message.ConversationID); err != nil {
			return fmt.Errorf("persist WhatsApp history metadata: %w", err)
		}
	}
	return tx.Commit()
}

func (s *historyStore) evictOne(ctx context.Context) (bool, error) {
	var chatID string
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT m.chat_id,COUNT(*) FROM history_messages AS m LEFT JOIN history_chat_state AS c ON c.chat_id=m.chat_id GROUP BY m.chat_id ORDER BY COALESCE(MAX(c.last_viewed),0) ASC,COALESCE(MAX(c.last_evicted),0) DESC,MIN(m.ts) ASC,m.chat_id ASC LIMIT 1`).Scan(&chatID, &count)
	if errors.Is(err, sql.ErrNoRows) || count == 0 {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	remove := count / 4
	if remove < 1 {
		remove = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	var boundary int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ts),0) FROM (SELECT ts FROM history_messages WHERE chat_id=? ORDER BY ts ASC,message_id ASC LIMIT ?)`, chatID, remove).Scan(&boundary); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE history_chat_state SET evicted_before=MAX(evicted_before,?) WHERE chat_id=?`, boundary, chatID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE history_chat_state SET last_evicted=? WHERE chat_id=?`, time.Now().UnixMicro(), chatID); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM history_messages WHERE rowid IN (SELECT rowid FROM history_messages WHERE chat_id=? ORDER BY ts ASC,message_id ASC LIMIT ?)`, chatID, remove); err != nil {
		_ = tx.Rollback()
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *historyStore) setBudgetMB(ctx context.Context, sizeMB int) error {
	if sizeMB != 64 && sizeMB != 128 && sizeMB != 256 && sizeMB != 512 {
		return fmt.Errorf("unsupported WhatsApp history cache size %d MB", sizeMB)
	}
	return s.setBudgetBytes(ctx, int64(sizeMB)*1024*1024)
}

func (s *historyStore) setBudgetBytes(ctx context.Context, physicalBudget int64) error {
	maxBytes := historyDatabaseBudgetBytes(physicalBudget)
	if maxBytes < sqlitePageBytes {
		return errors.New("WhatsApp history cache budget is too small for SQLite overhead")
	}
	pageLimit := maxBytes / sqlitePageBytes
	for attempt := 0; attempt < 1024; attempt++ {
		var pages int64
		if err := s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages); err != nil {
			return err
		}
		if pages <= pageLimit {
			if _, err := s.db.ExecContext(ctx, `PRAGMA max_page_count=`+fmt.Sprint(pageLimit)); err != nil {
				return err
			}
			s.budgetMu.Lock()
			s.maxBytes = maxBytes
			s.budgetMu.Unlock()
			return nil
		}
		evicted, err := s.evictOne(ctx)
		if err != nil {
			return fmt.Errorf("shrink WhatsApp history cache: %w", err)
		}
		if !evicted {
			return fmt.Errorf("could not shrink WhatsApp history cache to the selected budget: %d pages remain, limit is %d", pages, pageLimit)
		}
	}
	return errors.New("history cache shrink exceeded its bounded eviction limit")
}

func (s *historyStore) markViewed(ctx context.Context, chatID string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO history_chat_state(chat_id,last_viewed) VALUES(?,?) ON CONFLICT(chat_id) DO UPDATE SET last_viewed=excluded.last_viewed`, chatID, at.UnixMicro())
	return err
}

type historyPage struct {
	Messages []wire.Message
	HasMore  bool
	Raw      map[string][]byte
	Gap      bool
}

func (s *historyStore) get(ctx context.Context, chatID, messageID string) (wire.Message, bool, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM history_messages WHERE chat_id=? AND message_id=?`, chatID, messageID).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return wire.Message{}, false, nil
	}
	if err != nil {
		return wire.Message{}, false, err
	}
	if err := validateHistoryMessagePayload(payload); err != nil {
		return wire.Message{}, false, err
	}
	var message wire.Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return wire.Message{}, false, err
	}
	if retainedMessageBytes(message)+historyPageEntryOverhead > maxHistoryPageBytes {
		return wire.Message{}, false, errors.New("decoded WhatsApp history row exceeds the page memory ceiling")
	}
	return message, true, nil
}

func validateHistoryMessagePayload(payload []byte) error {
	if len(payload) > maxHistoryPageBytes {
		return errors.New("stored WhatsApp history row exceeds the page byte ceiling")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	for fieldName, limit := range map[string]int{
		"attachments": maxHistoryPageAttachments,
		"reactions":   maxHistoryPageReactions,
	} {
		for key, value := range fields {
			if !strings.EqualFold(key, fieldName) {
				continue
			}
			decoder := json.NewDecoder(bytes.NewReader(value))
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			if token == nil {
				continue
			}
			if token != json.Delim('[') {
				continue // The typed decoder below reports the field type mismatch.
			}
			count := 0
			for decoder.More() {
				if count == limit {
					return fmt.Errorf("stored WhatsApp history %s exceeds the bounded count of %d", fieldName, limit)
				}
				var item json.RawMessage
				if err := decoder.Decode(&item); err != nil {
					return err
				}
				count++
			}
		}
	}
	return nil
}

func (s *historyStore) getRaw(ctx context.Context, chatID, messageID string) ([]byte, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT raw_payload FROM history_messages WHERE chat_id=? AND message_id=?`, chatID, messageID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}

func (s *historyStore) page(ctx context.Context, chatID string, limit int, cursorID string, cursorTime int64) (historyPage, error) {
	if limit <= 0 {
		limit = 60
	}
	if limit > maxHistoryPageMessages {
		limit = maxHistoryPageMessages
	}
	query := `SELECT payload,raw_payload FROM history_messages WHERE chat_id=? ORDER BY ts DESC,message_id DESC LIMIT ?`
	args := []any{chatID, limit + 1}
	if cursorID != "" {
		query = `SELECT payload,raw_payload FROM history_messages WHERE chat_id=? AND (ts < ? OR (ts=? AND message_id < ?)) ORDER BY ts DESC,message_id DESC LIMIT ?`
		args = []any{chatID, cursorTime, cursorTime, cursorID, limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return historyPage{}, err
	}
	defer rows.Close()
	result := historyPage{Messages: make([]wire.Message, 0, limit), Raw: make(map[string][]byte)}
	encodedBytes := int64(0)
	retainedBytes := int64(historyPageContainerOverhead)
	for rows.Next() {
		var payload, raw []byte
		if err := rows.Scan(&payload, &raw); err != nil {
			return historyPage{}, err
		}
		if len(result.Messages) == limit {
			result.HasMore = true
			break
		}
		encodedRowBytes := int64(len(payload)) + int64(len(raw)) + historyPageEntryOverhead
		if encodedRowBytes > maxHistoryPageBytes || int64(len(payload)) > maxHistoryPageBytes {
			return historyPage{}, errors.New("stored WhatsApp history row exceeds the page byte ceiling")
		}
		if encodedBytes > maxHistoryPageBytes-encodedRowBytes {
			result.HasMore = true
			break
		}
		if err := validateHistoryMessagePayload(payload); err != nil {
			return historyPage{}, err
		}
		var message wire.Message
		if err := json.Unmarshal(payload, &message); err != nil {
			return historyPage{}, err
		}
		retainedRowBytes := retainedMessageBytes(message) + int64(len(raw)) + historyPageEntryOverhead
		if retainedRowBytes > maxHistoryPageBytes || retainedBytes > maxHistoryPageBytes-retainedRowBytes {
			if len(result.Messages) == 0 {
				return historyPage{}, errors.New("decoded WhatsApp history row exceeds the page memory ceiling")
			}
			result.HasMore = true
			break
		}
		result.Messages = append(result.Messages, message)
		encodedBytes += encodedRowBytes
		retainedBytes += retainedRowBytes
		if len(raw) > 0 {
			result.Raw[rawMediaKey(message.ConversationID, message.ID)] = raw
		}
	}
	if err := rows.Err(); err != nil {
		return historyPage{}, err
	}
	if err := rows.Close(); err != nil {
		return historyPage{}, err
	}
	if cursorID != "" && len(result.Messages) == 0 {
		var evictedBefore int64
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(evicted_before,0) FROM history_chat_state WHERE chat_id=?`, chatID).Scan(&evictedBefore); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return historyPage{}, err
		}
		result.Gap = evictedBefore > 0
	}
	if len(result.Messages) > 0 {
		if err := s.markViewed(ctx, chatID, time.Now()); err != nil {
			return historyPage{}, err
		}
	}
	for left, right := 0, len(result.Messages)-1; left < right; left, right = left+1, right-1 {
		result.Messages[left], result.Messages[right] = result.Messages[right], result.Messages[left]
	}
	return result, nil
}
