package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetTelegramCredentialsParamsUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		wantID   int
		wantHash string
		wantErr  bool
	}{
		{
			name:     "standard camelCase apiId and apiHash",
			json:     `{"apiId":12345,"apiHash":"0123456789abcdef0123456789abcdef"}`,
			wantID:   12345,
			wantHash: "0123456789abcdef0123456789abcdef",
		},
		{
			name:     "snake_case api_id and api_hash",
			json:     `{"api_id":54321,"api_hash":"fedcba9876543210fedcba9876543210"}`,
			wantID:   54321,
			wantHash: "fedcba9876543210fedcba9876543210",
		},
		{
			name:     "alternate apiID tag",
			json:     `{"apiID":99999,"apiHash":"11111111111111111111111111111111"}`,
			wantID:   99999,
			wantHash: "11111111111111111111111111111111",
		},
		{
			name:     "string integer for apiId",
			json:     `{"apiId":"88888","apiHash":"22222222222222222222222222222222"}`,
			wantID:   88888,
			wantHash: "22222222222222222222222222222222",
		},
		{
			name:     "empty string integer for apiId",
			json:     `{"apiId":"","apiHash":""}`,
			wantID:   0,
			wantHash: "",
		},
		{
			name:     "whitespace trimmed hash",
			json:     `{"apiId":123,"apiHash":"  0123456789abcdef0123456789abcdef  "}`,
			wantID:   123,
			wantHash: "0123456789abcdef0123456789abcdef",
		},
		{
			name:    "invalid non-numeric string apiId",
			json:    `{"apiId":"not-a-number","apiHash":"0123456789abcdef0123456789abcdef"}`,
			wantErr: true,
		},
		{
			name:    "invalid type boolean apiId",
			json:    `{"apiId":true,"apiHash":"0123456789abcdef0123456789abcdef"}`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p SetTelegramCredentialsParams
			err := json.Unmarshal([]byte(tc.json), &p)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.APIID != tc.wantID {
				t.Errorf("APIID = %d, want %d", p.APIID, tc.wantID)
			}
			if p.APIHash != tc.wantHash {
				t.Errorf("APIHash = %q, want %q", p.APIHash, tc.wantHash)
			}
		})
	}
}

func TestConfigResultExcludesAPIHash(t *testing.T) {
	res := ConfigResult{
		EnabledServices:    []string{"gmessages", "telegram"},
		TelegramConfigured: true,
		TelegramAPIID:      1234567,
		UiScale:            1.15,
	}

	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("failed to marshal ConfigResult: %v", err)
	}

	str := string(data)
	if !strings.Contains(str, `"telegramConfigured":true`) {
		t.Errorf("expected telegramConfigured:true in %s", str)
	}
	if !strings.Contains(str, `"telegramApiId":1234567`) {
		t.Errorf("expected telegramApiId:1234567 in %s", str)
	}
	if strings.Contains(strings.ToLower(str), "hash") {
		t.Errorf("SECURITY LEAK: ConfigResult marshaled output must never contain hash: %s", str)
	}
}
