package libgm

// OmaChat: tests for the local, log-only CookieObserver patch in client.go.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func cookieResponse(setCookies ...string) *http.Response {
	header := http.Header{}
	for _, c := range setCookies {
		header.Add("Set-Cookie", c)
	}
	header.Set("Content-Type", "application/json+protobuf")
	return &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     header,
		Request:    &http.Request{URL: &url.URL{Scheme: "https", Host: "instantmessaging-pa.googleapis.com", Path: "/x"}},
	}
}

func TestCookieObserverUnsetKeepsUpstreamMerge(t *testing.T) {
	ad := &AuthData{Cookies: map[string]string{"SID": "old-sid", "HSID": "keep"}}
	ad.UpdateCookiesFromResponse(cookieResponse("SID=new-sid; Path=/", "SIDCC=cc-value", "GONE=; Max-Age=0"))
	want := map[string]string{"SID": "new-sid", "HSID": "keep", "SIDCC": "cc-value", "GONE": ""}
	if !reflect.DeepEqual(ad.Cookies, want) {
		t.Fatalf("cookies = %v, want %v", ad.Cookies, want)
	}
}

func TestCookieObserverNilCookieMapStillIgnored(t *testing.T) {
	called := false
	ad := &AuthData{CookieObserver: func(CookieUpdate) { called = true }}
	ad.UpdateCookiesFromResponse(cookieResponse("SID=x"))
	if ad.Cookies != nil || called {
		t.Fatalf("nil cookie map must stay nil and unobserved; cookies=%v called=%v", ad.Cookies, called)
	}
}

func TestCookieObserverReportsNamesAndClearedFlags(t *testing.T) {
	var got []CookieUpdate
	ad := &AuthData{Cookies: map[string]string{}, CookieObserver: func(u CookieUpdate) { got = append(got, u) }}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	ad.UpdateCookiesFromResponse(cookieResponse(
		"SIDCC=secret-sidcc-value; Path=/",
		"DELETED=; Path=/",
		"MAXAGE=v; Max-Age=0",
		"EXPIRED=v; Expires="+past,
	))
	if len(got) != 1 {
		t.Fatalf("observer calls = %d, want 1", len(got))
	}
	u := got[0]
	if u.Host != "instantmessaging-pa.googleapis.com" || u.Status != http.StatusUnauthorized || u.ContentType != "application/json+protobuf" {
		t.Fatalf("unexpected update metadata: %+v", u)
	}
	want := []CookieName{{"SIDCC", false}, {"DELETED", true}, {"MAXAGE", true}, {"EXPIRED", true}}
	if !reflect.DeepEqual(u.Names, want) {
		t.Fatalf("names = %+v, want %+v", u.Names, want)
	}
	if strings.Contains(fmtUpdate(u), "secret-sidcc-value") {
		t.Fatal("cookie value reached the observer")
	}
}

func TestCookieObserverNotCalledWithoutSetCookie(t *testing.T) {
	calls := 0
	ad := &AuthData{Cookies: map[string]string{}, CookieObserver: func(CookieUpdate) { calls++ }}
	ad.UpdateCookiesFromResponse(cookieResponse())
	if calls != 0 {
		t.Fatalf("observer called %d times for a response without cookies", calls)
	}
}

func TestCookieObserverHandlesResponseWithoutRequest(t *testing.T) {
	var got CookieUpdate
	ad := &AuthData{Cookies: map[string]string{}, CookieObserver: func(u CookieUpdate) { got = u }}
	resp := cookieResponse("SID=x")
	resp.Request = nil
	ad.UpdateCookiesFromResponse(resp)
	if got.Host != "" || len(got.Names) != 1 {
		t.Fatalf("unexpected update: %+v", got)
	}
}

// The update types must never be able to carry a cookie value.
func TestCookieUpdateTypesHaveNoValueField(t *testing.T) {
	allowed := map[reflect.Type][]string{
		reflect.TypeOf(CookieUpdate{}): {"Host", "Status", "ContentType", "Names"},
		reflect.TypeOf(CookieName{}):   {"Name", "Cleared"},
	}
	for typ, fields := range allowed {
		var names []string
		for i := 0; i < typ.NumField(); i++ {
			names = append(names, typ.Field(i).Name)
		}
		if !reflect.DeepEqual(names, fields) {
			t.Fatalf("%s fields = %v, want exactly %v", typ.Name(), names, fields)
		}
	}
}

// The observer runs after CookiesLock is released, so it may read the jar.
func TestCookieObserverRunsOutsideCookieLock(t *testing.T) {
	done := make(chan struct{})
	ad := &AuthData{Cookies: map[string]string{"SAPISID": "s"}}
	ad.CookieObserver = func(CookieUpdate) {
		req, _ := http.NewRequest(http.MethodGet, "https://messages.google.com/", nil)
		ad.AddCookiesToRequest(req)
		ad.CookiesLock.Lock()
		ad.CookiesLock.Unlock()
	}
	go func() {
		ad.UpdateCookiesFromResponse(cookieResponse("SID=x"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("observer deadlocked on CookiesLock")
	}
}

func TestCookieObserverIsNotSerialized(t *testing.T) {
	ad := NewAuthData()
	ad.CookieObserver = func(CookieUpdate) {}
	data, err := json.Marshal(ad)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "observer") {
		t.Fatalf("observer leaked into session JSON: %s", data)
	}
}

func fmtUpdate(u CookieUpdate) string {
	b, _ := json.Marshal(u)
	return string(b)
}
