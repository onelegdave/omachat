package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelegramConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cs := NewConfigStore(path)
	if cs.Get().TelegramAPIID != 0 || cs.Get().TelegramAPIHash != "" {
		t.Fatal("expected empty default Telegram credentials")
	}

	const testID = 1234567
	const testHash = "0123456789abcdef0123456789abcdef"

	if err := cs.SetTelegramCredentials(testID, testHash); err != nil {
		t.Fatalf("SetTelegramCredentials failed: %v", err)
	}

	got := cs.Get()
	if got.TelegramAPIID != testID {
		t.Errorf("TelegramAPIID = %d, want %d", got.TelegramAPIID, testID)
	}
	if got.TelegramAPIHash != testHash {
		t.Errorf("TelegramAPIHash = %q, want %q", got.TelegramAPIHash, testHash)
	}

	// Verify atomic persistence by reading back fresh
	reopened := NewConfigStore(path)
	rgot := reopened.Get()
	if rgot.TelegramAPIID != testID || rgot.TelegramAPIHash != testHash {
		t.Errorf("reopened credentials mismatch: %+v", rgot)
	}

	// Verify TelegramCredentials helper on ConfigStore
	creds, err := reopened.TelegramCredentials()
	if err != nil {
		t.Fatalf("TelegramCredentials returned unexpected error: %v", err)
	}
	if creds.APIID != testID || creds.APIHash != testHash {
		t.Errorf("creds mismatch: %+v", creds)
	}

	// Test individual field setters
	const updatedID = 7654321
	const updatedHash = "abcdef0123456789abcdef0123456789"
	if err := reopened.SetTelegramAPIID(updatedID); err != nil {
		t.Fatalf("SetTelegramAPIID failed: %v", err)
	}
	if err := reopened.SetTelegramAPIHash(updatedHash); err != nil {
		t.Fatalf("SetTelegramAPIHash failed: %v", err)
	}

	reopened2 := NewConfigStore(path)
	if reopened2.Get().TelegramAPIID != updatedID || reopened2.Get().TelegramAPIHash != updatedHash {
		t.Errorf("reopened2 credentials mismatch: %+v", reopened2.Get())
	}

	// Verify JSON unmarshaling supports both camelCase and snake_case
	rawCamel := []byte(`{"telegramApiID":123,"telegramApiHash":"0123456789abcdef0123456789abcdef"}`)
	var cfgCamel Config
	if err := json.Unmarshal(rawCamel, &cfgCamel); err != nil {
		t.Fatalf("Unmarshal camelCase failed: %v", err)
	}
	if cfgCamel.TelegramAPIID != 123 || cfgCamel.TelegramAPIHash != "0123456789abcdef0123456789abcdef" {
		t.Errorf("cfgCamel mismatch: %+v", cfgCamel)
	}

	rawSnake := []byte(`{"telegram_api_id":456,"telegram_api_hash":"fedcba9876543210fedcba9876543210"}`)
	var cfgSnake Config
	if err := json.Unmarshal(rawSnake, &cfgSnake); err != nil {
		t.Fatalf("Unmarshal snake_case failed: %v", err)
	}
	if cfgSnake.TelegramAPIID != 456 || cfgSnake.TelegramAPIHash != "fedcba9876543210fedcba9876543210" {
		t.Errorf("cfgSnake mismatch: %+v", cfgSnake)
	}

	// Verify marshaling outputs camelCase
	marshaled, err := json.Marshal(cfgCamel)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if !strings.Contains(string(marshaled), "telegramApiID") || !strings.Contains(string(marshaled), "telegramApiHash") {
		t.Errorf("expected camelCase tags in marshaled output, got: %s", string(marshaled))
	}
}

func TestTelegramCredentialsPrecedence(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "9999999")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	const cfgID = 1111111
	const cfgHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// 1. Config takes precedence when both config and env are set
	cfgBoth := Config{
		TelegramAPIID:   cfgID,
		TelegramAPIHash: cfgHash,
	}
	creds, err := cfgBoth.TelegramCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.APIID != cfgID {
		t.Errorf("APIID = %d, want config value %d", creds.APIID, cfgID)
	}
	if creds.APIHash != cfgHash {
		t.Errorf("APIHash = %s, want config value %s", creds.APIHash, cfgHash)
	}

	// 2. Fallback to environment variables when config is unset
	cfgEmpty := Config{}
	credsEnv, err := cfgEmpty.TelegramCredentials()
	if err != nil {
		t.Fatalf("unexpected error on env fallback: %v", err)
	}
	if credsEnv.APIID != 9999999 {
		t.Errorf("APIID = %d, want env value 9999999", credsEnv.APIID)
	}
	if credsEnv.APIHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("APIHash = %s, want env value aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", credsEnv.APIHash)
	}

	// 3. Mixed fallback: config has ID, env has Hash
	cfgIDOnly := Config{TelegramAPIID: cfgID}
	credsMixed1, err := cfgIDOnly.TelegramCredentials()
	if err != nil {
		t.Fatalf("unexpected error on mixed 1: %v", err)
	}
	if credsMixed1.APIID != cfgID || credsMixed1.APIHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("unexpected mixed 1 creds: %+v", credsMixed1)
	}

	// 4. Mixed fallback: config has Hash, env has ID
	cfgHashOnly := Config{TelegramAPIHash: cfgHash}
	credsMixed2, err := cfgHashOnly.TelegramCredentials()
	if err != nil {
		t.Fatalf("unexpected error on mixed 2: %v", err)
	}
	if credsMixed2.APIID != 9999999 || credsMixed2.APIHash != cfgHash {
		t.Errorf("unexpected mixed 2 creds: %+v", credsMixed2)
	}
}

func TestTelegramCredentialsMalformed(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	const validHash = "0123456789abcdef0123456789abcdef"
	const secretMarker = "SUPER_SECRET_VALUE_DO_NOT_LOG"

	tests := []struct {
		name      string
		cfg       Config
		envID     string
		envHash   string
		secretVal string
	}{
		{
			name:      "negative config api_id",
			cfg:       Config{TelegramAPIID: -10, TelegramAPIHash: validHash},
			secretVal: "-10",
		},
		{
			name:      "non-numeric env api_id",
			cfg:       Config{TelegramAPIHash: validHash},
			envID:     "not-a-number-" + secretMarker,
			secretVal: secretMarker,
		},
		{
			name:      "negative env api_id",
			cfg:       Config{TelegramAPIHash: validHash},
			envID:     "-999",
			secretVal: "-999",
		},
		{
			name:      "config api_hash too short (31 chars)",
			cfg:       Config{TelegramAPIID: 12345, TelegramAPIHash: "0123456789abcdef0123456789abcde"},
			secretVal: "0123456789abcdef0123456789abcde",
		},
		{
			name:      "config api_hash too long (33 chars)",
			cfg:       Config{TelegramAPIID: 12345, TelegramAPIHash: "0123456789abcdef0123456789abcdef0"},
			secretVal: "0123456789abcdef0123456789abcdef0",
		},
		{
			name:      "config api_hash non-hex chars",
			cfg:       Config{TelegramAPIID: 12345, TelegramAPIHash: "0123456789abcdef0123456789abcdeg"},
			secretVal: "0123456789abcdef0123456789abcdeg",
		},
		{
			name:      "env api_hash too short",
			cfg:       Config{TelegramAPIID: 12345},
			envHash:   "short-" + secretMarker,
			secretVal: secretMarker,
		},
		{
			name:      "env api_hash non-hex chars",
			cfg:       Config{TelegramAPIID: 12345},
			envHash:   "0123456789abcdef0123456789abcdez",
			secretVal: "0123456789abcdef0123456789abcdez",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OMACHAT_TELEGRAM_API_ID", tc.envID)
			t.Setenv("OMACHAT_TELEGRAM_API_HASH", tc.envHash)

			_, err := tc.cfg.TelegramCredentials()
			if err == nil {
				t.Fatal("expected error for malformed credentials, got nil")
			}

			// Security invariant: secrets/raw values must never be leaked in errors
			errStr := err.Error()
			if tc.secretVal != "" && strings.Contains(errStr, tc.secretVal) {
				t.Errorf("SECURITY LEAK: error message %q contains secret value %q", errStr, tc.secretVal)
			}
		})
	}
}

func TestTelegramCredentialsUnconfigured(t *testing.T) {
	t.Setenv("OMACHAT_TELEGRAM_API_ID", "")
	t.Setenv("OMACHAT_TELEGRAM_API_HASH", "")

	// 1. Both unset
	cfgEmpty := Config{}
	_, err := cfgEmpty.TelegramCredentials()
	if err == nil {
		t.Fatal("expected ErrTelegramUnconfigured, got nil")
	}
	if !errors.Is(err, ErrTelegramUnconfigured) {
		t.Errorf("expected error to match ErrTelegramUnconfigured, got: %v", err)
	}

	// 2. ID set, Hash missing
	cfgIDOnly := Config{TelegramAPIID: 12345}
	_, err = cfgIDOnly.TelegramCredentials()
	if err == nil {
		t.Fatal("expected error for missing hash, got nil")
	}
	if !errors.Is(err, ErrTelegramUnconfigured) {
		t.Errorf("expected missing hash to match ErrTelegramUnconfigured, got: %v", err)
	}

	// 3. Hash set, ID missing
	cfgHashOnly := Config{TelegramAPIHash: "0123456789abcdef0123456789abcdef"}
	_, err = cfgHashOnly.TelegramCredentials()
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
	if !errors.Is(err, ErrTelegramUnconfigured) {
		t.Errorf("expected missing ID to match ErrTelegramUnconfigured, got: %v", err)
	}
}
