package daemon

import (
	"net/http"
	"testing"
)

func TestPublicMediaRedirectPolicy(t *testing.T) {
	for _, target := range []string{"http://media.example.com/test.gif", "file:///etc/passwd"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := avatarHTTP.CheckRedirect(req, nil); err == nil {
			t.Fatalf("accepted non-HTTPS redirect %q", target)
		}
	}
	req, err := http.NewRequest(http.MethodGet, "https://media.example.com/test.gif", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := avatarHTTP.CheckRedirect(req, make([]*http.Request, 10)); err == nil {
		t.Fatal("redirect limit not enforced")
	}
	if err := avatarHTTP.CheckRedirect(req, nil); err != nil {
		t.Fatal(err)
	}
}
