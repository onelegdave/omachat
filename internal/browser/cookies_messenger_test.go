package browser

import (
	"context"
	"testing"
)

func TestExtractMessengerCookiesContext_NoProfile(t *testing.T) {
	ctx := context.Background()
	_, err := ExtractMessengerCookiesContext(ctx, Profile{
		Name:        "Fake",
		CookieDB:    "/does/not/exist",
		KeyringApp:  "chrome",
	})
	if err == nil {
		t.Fatal("expected error for missing profile DB")
	}
}
