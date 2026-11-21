package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// newDiagDaemon is newTestDaemon with a private state folder and no access to
// the real browser, keyring, or sqlite3.
func newDiagDaemon(t *testing.T) (*Daemon, *store.Paths, string) {
	t.Helper()
	d, paths := newTestDaemon(t)
	paths.State = filepath.Join(t.TempDir(), "state", "omachat")
	d.diag = newAuthDiag(paths)
	d.diag.discover = func() []browser.Profile { return nil }
	d.diag.sqlite = func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("sqlite3 disabled in tests")
	}
	t.Cleanup(d.stopDiagnostics)
	logPath, err := paths.DebugLogFile()
	if err != nil {
		t.Fatal(err)
	}
	return d, paths, logPath
}

func enableDiag(t *testing.T, d *Daemon) {
	t.Helper()
	if err := d.applyDebugLogging(true, "test"); err != nil {
		t.Fatal(err)
	}
}

func httpResp(status int, contentType string, setCookies ...string) *http.Response {
	h := http.Header{}
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	for _, c := range setCookies {
		h.Add("Set-Cookie", c)
	}
	return &http.Response{StatusCode: status, Header: h,
		Request: &http.Request{URL: &url.URL{Scheme: "https", Host: "instantmessaging-pa.googleapis.com"}}}
}

const googleUnauthBody = `[16,"Request had invalid authentication credentials. Expected OAuth 2 access token, login cookie or other valid authentication credential.",[["type.googleapis.com/google.rpc.ErrorInfo",["SESSION_COOKIE_INVALID","googleapis.com"]]]]`

func googleRequestError(status int, body string, setCookies ...string) events.RequestError {
	he := events.HTTPError{Resp: httpResp(status, "application/json+protobuf", setCookies...), Body: []byte(body)}
	return events.RequestError{HTTP: &he, Data: &gmproto.ErrorResponse{
		Type:    16,
		Message: "Request had invalid authentication credentials.",
		Class:   []*gmproto.ErrorResponse_ErrorClass{{Class: "type.googleapis.com/google.rpc.ErrorInfo"}},
	}}
}

func TestDiagnosticsOffCreatesNothing(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	d.mu.Unlock()
	d.cookies.last = time.Now()
	ctx := context.Background()

	d.attachAuthObservers(d.auth)
	d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=x"))
	_, _ = withAuthRetryAt(ctx, d, "rpc:test", func() (int, error) { return 0, errors.New("HTTP 401") })
	d.diagObserveAuthError("rpc:markRead", errors.New("HTTP 401"))
	d.diagStartupConnectFailed(errors.New("HTTP 401"))
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(&events.PingFailed{Error: errors.New("HTTP 401")})
	handler(&gmproto.UserAlertEvent{AlertType: gmproto.AlertType_BROWSER_ACTIVE})
	d.sessionMu.Lock()
	d.saveSessionLocked()
	d.sessionMu.Unlock()
	d.setState(wire.StateConnecting, "")
	d.diagStartup("test")
	d.diag.watcher.tick(ctx, d)
	if err := d.DeleteDebugLog(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Dir(logPath)); !os.IsNotExist(err) {
		t.Fatalf("diagnostics while off created %s: %v", filepath.Dir(logPath), err)
	}
	if d.diag.watcher.running() {
		t.Fatal("watcher running while off")
	}
}

func TestSetDebugLoggingThroughDispatch(t *testing.T) {
	d, paths, logPath := newDiagDaemon(t)
	ctx := context.Background()

	for _, params := range []any{nil, map[string]any{}, map[string]any{"enabled": "yes"}} {
		resp := d.dispatch(ctx, wire.Request{ID: "1", Method: wire.MethodSetDebugLogging, Params: params})
		if resp.OK {
			t.Fatalf("malformed params %v were accepted", params)
		}
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) || d.config.Get().DebugLogging {
		t.Fatal("malformed requests must not switch diagnostics on")
	}

	resp := d.dispatch(ctx, wire.Request{ID: "2", Method: wire.MethodSetDebugLogging, Params: map[string]any{"enabled": true}})
	cfg, ok := resp.Result.(wire.ConfigResult)
	if !resp.OK || !ok || !cfg.DebugLogging || cfg.DebugLogPath == "" {
		t.Fatalf("enable response = %+v", resp)
	}
	if !store.NewConfigStore(paths.ConfigFile()).Get().DebugLogging {
		t.Fatal("setting was not saved")
	}
	if !d.diag.watcher.running() {
		t.Fatal("watcher did not start with the setting")
	}
	st, err := os.Stat(logPath)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("log = %v, %v", st, err)
	}
	startup := entriesOfKind(readLogEntries(t, logPath), "startup")
	if len(startup) != 1 || startup[0]["reason"] != "settings" || startup[0]["build"] == nil {
		t.Fatalf("startup summary = %v", startup)
	}
	if cfgReply := d.PluginConfig(); !cfgReply.DebugLogging {
		t.Fatal("config reply does not report the setting")
	}

	resp = d.dispatch(ctx, wire.Request{ID: "3", Method: wire.MethodSetDebugLogging, Params: map[string]any{"enabled": false}})
	if cfg, _ := resp.Result.(wire.ConfigResult); !resp.OK || cfg.DebugLogging {
		t.Fatalf("disable response = %+v", resp)
	}
	if d.diag.watcher.running() || d.diagOn() {
		t.Fatal("turning off must stop the watcher and the log immediately")
	}
	before, _ := os.ReadFile(logPath)
	d.diagWrite("afterOff", logFields{})
	d.setState(wire.StateError, "after off")
	after, _ := os.ReadFile(logPath)
	if !bytes.Equal(before, after) {
		t.Fatal("log changed after diagnostics were switched off")
	}
	if store.NewConfigStore(paths.ConfigFile()).Get().DebugLogging {
		t.Fatal("disabled setting was not saved")
	}
}

func TestSetDebugLoggingIsAGlobalSetting(t *testing.T) {
	if !globalSetting(wire.MethodSetDebugLogging) {
		t.Fatal("setDebugLogging returns a config snapshot and must be ordered with other global settings")
	}
}

func TestSetDebugLoggingWithoutStateFolderFails(t *testing.T) {
	d, paths := newTestDaemon(t)
	resp := d.dispatch(context.Background(), wire.Request{ID: "1", Method: wire.MethodSetDebugLogging, Params: map[string]any{"enabled": true}})
	if resp.OK {
		t.Fatal("diagnostics must refuse to start without a state folder")
	}
	if store.NewConfigStore(paths.ConfigFile()).Get().DebugLogging {
		t.Fatal("a failed enable must not be saved")
	}
	if _, err := os.Stat("auth.log"); !os.IsNotExist(err) {
		t.Fatal("diagnostics wrote auth.log into the working directory")
	}
}

func TestDeleteDebugLogWorksWhileServiceDisabledOrRestarting(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	ctx := context.Background()
	d.servicesMu.Lock()
	d.activeServices = map[string]bool{}
	d.restartPending = true
	d.servicesMu.Unlock()

	// Off: succeeds without creating anything.
	if resp := d.dispatch(ctx, wire.Request{ID: "1", Method: wire.MethodDeleteDebugLog}); !resp.OK {
		t.Fatalf("delete while off = %+v", resp)
	}
	if _, err := os.Stat(filepath.Dir(logPath)); !os.IsNotExist(err) {
		t.Fatal("Delete log while off created the folder")
	}

	enableDiag(t, d)
	if err := os.WriteFile(logPath+".1", []byte("{\"old\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldID := d.diag.log.logID()
	if resp := d.dispatch(ctx, wire.Request{ID: "2", Method: wire.MethodDeleteDebugLog}); !resp.OK {
		t.Fatalf("delete while on = %+v", resp)
	}
	if _, err := os.Stat(logPath + ".1"); !os.IsNotExist(err) {
		t.Fatal("rotated log not deleted")
	}
	entries := readLogEntries(t, logPath)
	if len(entries) != 1 || entries[0]["kind"] != "logCleared" || entries[0]["log"] == oldID {
		t.Fatalf("new log after delete = %v", entries)
	}

	d.applyDebugLogging(false, "")
	if resp := d.dispatch(ctx, wire.Request{ID: "3", Method: wire.MethodDeleteDebugLog}); !resp.OK {
		t.Fatalf("delete after off = %+v", resp)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("delete while off must remove the log and not recreate it")
	}
}

// Every path that touches credentials, with fixed sentinel values. Neither
// log file may contain any of them; their fingerprints must appear.
func TestDiagnosticsNeverLogSecrets(t *testing.T) {
	d, paths, logPath := newDiagDaemon(t)
	ctx := context.Background()
	secrets := map[string]string{
		"SID":              "SENTINELsid0000aaaa",
		"HSID":             "SENTINELhsid111bbbb",
		"SSID":             "SENTINELssid222cccc",
		"APISID":           "SENTINELapisid33dddd",
		"SAPISID":          "SENTINELsapisid4eeee",
		"OSID":             "SENTINELosid5555ffff",
		"__Secure-1PSIDTS": "SENTINELpsidts66gggg",
	}
	token := []byte("SENTINELtachyonTOKEN77hhhh")
	auth := syntheticTestAuth()
	auth.SetCookies(map[string]string{})
	for k, v := range secrets {
		auth.Cookies[k] = v
	}
	auth.TachyonAuthToken = token
	auth.TachyonExpiry = time.Now().Add(time.Hour)
	if err := paths.SaveSession(auth); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	d.auth = auth
	d.paired = true
	d.status.State = wire.StateConnected
	d.mu.Unlock()
	d.attachAuthObservers(auth)
	d.diag.log.maxBytes = 48 << 10 // filler below forces one rotation so both files are checked
	d.diag.sqlite = func(_ context.Context, _, query string) ([]byte, error) {
		if strings.Contains(query, "pragma_table_info") {
			return []byte("name\nhost_key\nencrypted_value\ncreation_utc\nexpires_utc\nlast_update_utc\n"), nil
		}
		return []byte("SID|.google.com|13300000000000000|13300000000000001|13400000000000000|40|SENTINELhashOfCiphertext\n"), nil
	}
	enableDiag(t, d)

	allSecret := strings.Join([]string{secrets["SID"], secrets["SAPISID"], string(token)}, " ")
	d.cookies.last = time.Now()
	_, _ = withAuthRetryAt(ctx, d, "rpc:send", func() (int, error) {
		return 0, fmt.Errorf("HTTP 401: echoed %s", allSecret)
	})
	reqErr := googleRequestError(401, googleUnauthBody+" "+secrets["HSID"], "SIDCC="+secrets["SSID"]+"; Path=/")
	d.diagAuthFailure("rpc:listMessages", fmt.Errorf("failed to refresh auth token: %w", reqErr), true)
	auth.UpdateCookiesFromResponse(httpResp(200, "application/json", "SIDCC=SENTINELnewsidcc88iiii"))
	fresh := map[string]string{}
	for k, v := range secrets {
		fresh[k] = v + "-fresh"
	}
	d.diagCookieRefreshSuccess(ctx, "rpc:send", "c1", browser.Profile{Name: "Chrome / Default", CookieDB: "/none"}, nil, snapshotCookies(auth), fresh, []string{"SID"})
	d.mu.Lock()
	d.paired = true // the 401 above latched "needs pairing"; saving needs a pairing
	d.mu.Unlock()
	d.sessionMu.Lock()
	auth.SetCookies(fresh)
	d.saveSessionLocked()
	d.sessionMu.Unlock()
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: httpResp(401, "text/html"), Body: []byte("<html>" + fresh["OSID"] + "</html>")}})
	d.setState(wire.StateError, "status echoing "+fresh["APISID"])
	d.applyDebugLogging(false, "")
	enableDiag(t, d) // second startup summary reads session.json
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(logPath + ".1"); err == nil {
			break
		}
		d.diagWrite("filler", logFields{"pad": strings.Repeat("p", 200)})
	}

	var combined []byte
	for _, p := range []string{logPath + ".1", logPath} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		combined = append(combined, data...)
	}
	for _, s := range []string{"SENTINEL", "-fresh"} {
		if bytes.Contains(combined, []byte(s)) {
			idx := bytes.Index(combined, []byte(s))
			lo := idx - 120
			if lo < 0 {
				lo = 0
			}
			t.Fatalf("log contains a secret %q near: %s", s, combined[lo:idx+40])
		}
	}
	if !bytes.Contains(combined, []byte("[redacted]")) {
		t.Fatal("echoed credentials were not redacted")
	}
	if !bytes.Contains(combined, []byte(string(d.diag.log.fp(fresh["SID"])))) {
		t.Fatal("cookie fingerprints missing from the log")
	}
}

func TestAuthErrorDescriptions(t *testing.T) {
	wrapped := fmt.Errorf("failed to refresh auth token: %w", googleRequestError(401, googleUnauthBody, "SIDCC=v; Path=/"))
	f := describeAuthError(wrapped)
	if f["typed"] != "RequestError" || f["status"] != 401 || f["code"] != int64(16) || f["matched"] != "401" ||
		f["sessionInvalidMatched"] != false || f["bodyContainsSessionCookieInvalid"] != true || f["body"] != bodyText(googleUnauthBody) {
		t.Fatalf("RequestError fields = %v", f)
	}
	if names, _ := f["setCookieNames"].([]string); len(names) != 1 || names[0] != "SIDCC" {
		t.Fatalf("set-cookie names = %v", f["setCookieNames"])
	}
	if classes, _ := f["classes"].([]string); len(classes) != 1 {
		t.Fatalf("classes = %v", f["classes"])
	}
	if listenWhere(wrapped) != "tokenRefresh" {
		t.Fatal("token refresh failure not identified")
	}

	poll := events.HTTPError{Action: "polling", Resp: httpResp(403, "text/html"), Body: []byte("<html>denied</html>")}
	f = describeAuthError(poll)
	if f["typed"] != "HTTPError" || f["status"] != 403 || f["action"] != "polling" || f["body"] != bodyText("<html>denied</html>") || listenWhere(poll) != "longpoll" {
		t.Fatalf("long poll fields = %v", f)
	}

	f = describeAuthError(events.HTTPError{Action: "polling"})
	if f["typed"] != "HTTPError" || !strings.Contains(f["err"].(string), "unprintable") {
		t.Fatalf("nil response fields = %v", f)
	}

	f = describeAuthError(events.HTTPError{Resp: httpResp(500, "text/plain"), Body: []byte("oops")})
	if _, has := f["body"]; has {
		t.Fatal("bodies are kept only for 401 and 403")
	}

	media := &mediaHTTPError{status: 403, contentType: "text/html", body: []byte("<html>no</html>"), bodyLen: 15}
	f = describeAuthError(fmt.Errorf("download media: %w", media))
	if f["typed"] != "mediaHTTPError" || f["status"] != 403 || f["matched"] != "403" || f["body"] != bodyText("<html>no</html>") {
		t.Fatalf("media fields = %v", f)
	}
}

func TestMediaForbiddenChainIsLogged(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	d.mu.Unlock()
	enableDiag(t, d)
	d.cookies.mu.Lock()
	d.cookies.last = time.Now()
	d.cookies.lastTrigger = "rpc:listConversations"
	d.cookies.lastResult = "ok"
	d.cookies.mu.Unlock()

	_, err := withAuthRetryAt(context.Background(), d, "rpc:downloadMedia", func() ([]byte, error) {
		return nil, &mediaHTTPError{status: 403, contentType: "text/html", body: []byte("<html>expired</html>"), bodyLen: 20}
	})
	if err == nil || err.Error() != "download failed: HTTP 403" {
		t.Fatalf("error text changed: %v", err)
	}
	entries := readLogEntries(t, logPath)
	fail := entriesOfKind(entries, "authFailure")
	if len(fail) != 1 || fail[0]["where"] != "rpc:downloadMedia" || fail[0]["requestUsesCookies"] != false ||
		fail[0]["matched"] != "403" || fail[0]["typed"] != "mediaHTTPError" || fail[0]["withAuthRetry"] != true || fail[0]["body"] != "<html>expired</html>" {
		t.Fatalf("authFailure = %v", fail)
	}
	callID := fail[0]["callID"]
	refresh := entriesOfKind(entries, "cookieRefresh")
	if len(refresh) != 1 || refresh[0]["result"] != "throttled" || refresh[0]["callID"] != callID ||
		refresh[0]["trigger"] != "rpc:downloadMedia" || refresh[0]["lastTrigger"] != "rpc:listConversations" || refresh[0]["lastResult"] != "ok" {
		t.Fatalf("cookieRefresh = %v", refresh)
	}
	outcome := entriesOfKind(entries, "authOutcome")
	if len(outcome) != 1 || outcome[0]["callID"] != callID || outcome[0]["requirePairing"] != true || outcome[0]["retried"] != false {
		t.Fatalf("authOutcome = %v", outcome)
	}
	if len(entriesOfKind(entries, "requirePairing")) != 1 {
		t.Fatal("requirePairing not recorded")
	}
	states := entriesOfKind(entries, "state")
	if len(states) == 0 || states[len(states)-1]["to"] != string(wire.StateError) {
		t.Fatalf("state change not recorded: %v", states)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadKeepsErrorBodyOnlyWhileLogging(t *testing.T) {
	d, _, _ := newDiagDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.status.State = wire.StateConnected
	d.mu.Unlock()
	old := mediaHTTP
	t.Cleanup(func() { mediaHTTP = old })
	mediaHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := httpResp(403, "text/html")
		resp.Body = io.NopCloser(strings.NewReader("<html>forbidden</html>"))
		resp.Request = r
		return resp, nil
	})}
	_, err := d.downloadMediaBounded(context.Background(), "media-id", make([]byte, 32))
	var me *mediaHTTPError
	if !errors.As(err, &me) {
		t.Fatalf("download error = %v", err)
	}
	if me.body != nil || err.Error() != "download failed: HTTP 403" {
		t.Fatalf("logging off: err = %v, body = %q", err, me.body)
	}
	enableDiag(t, d)
	_, err = d.downloadMediaBounded(context.Background(), "media-id", make([]byte, 32))
	if !errors.As(err, &me) || string(me.body) != "<html>forbidden</html>" || err.Error() != "download failed: HTTP 403" {
		t.Fatalf("logging on: err = %v, body = %q", err, me.body)
	}
}

func TestListenEventsAreLoggedEvenWhenIgnored(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	enableDiag(t, d)
	// Unpaired: canSyncLocked is false, so the daemon ignores these.
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: httpResp(401, "text/plain"), Body: []byte("nope")}})
	handler(&events.ListenTemporaryError{Error: errors.New("dial tcp: timeout")})
	handler(&events.ListenRecovered{})
	other := libgm.NewClient(syntheticTestAuth(), nil, d.log)
	d.clientEventHandler(other, d.sessionContext())(&events.ListenFatalError{Error: errors.New("HTTP 401")})
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	d.clientEventHandler(d.client, ended)(&events.GaiaLoggedOut{})

	entries := readLogEntries(t, logPath)
	fatal := entriesOfKind(entries, "listenFatal")
	if len(fatal) != 2 || fatal[0]["ignored"] != "notSyncable" || fatal[0]["where"] != "longpoll" || fatal[0]["body"] != "nope" || fatal[1]["ignored"] != "staleClient" {
		t.Fatalf("listenFatal = %v", fatal)
	}
	if e := entriesOfKind(entries, "listenTemporary"); len(e) != 1 || e[0]["ignored"] != "notSyncable" {
		t.Fatalf("listenTemporary = %v", e)
	}
	if e := entriesOfKind(entries, "listenRecovered"); len(e) != 1 || e[0]["ignored"] != "notSyncable" {
		t.Fatalf("listenRecovered = %v", e)
	}
	if e := entriesOfKind(entries, "gaiaLoggedOut"); len(e) != 1 || e[0]["ignored"] != "sessionEnded" {
		t.Fatalf("gaiaLoggedOut = %v", e)
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.status.State != wire.StateUnpaired {
		t.Fatalf("logging changed state to %s", d.status.State)
	}
}

func TestNewLibgmEventsAreLogged(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	d.auth.TachyonExpiry = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	enableDiag(t, d)
	handler := d.clientEventHandler(d.client, d.sessionContext())
	handler(&events.PingFailed{Error: googleRequestError(401, googleUnauthBody), ErrorCount: 2})
	handler(&gmproto.UserAlertEvent{AlertType: gmproto.AlertType_BROWSER_ACTIVE})
	handler(&gmproto.RevokePairData{})
	handler(&events.NoDataReceived{})
	handler(&events.HackySetActiveMayFail{})
	handler(&events.AuthTokenRefreshed{})

	entries := readLogEntries(t, logPath)
	ping := entriesOfKind(entries, "pingFailed")
	if len(ping) != 1 || ping[0]["status"].(float64) != 401 || ping[0]["authError"] != true || ping[0]["errorCount"].(float64) != 2 {
		t.Fatalf("pingFailed = %v", ping)
	}
	if e := entriesOfKind(entries, "userAlert"); len(e) != 1 || e[0]["alert"] != "BROWSER_ACTIVE" {
		t.Fatalf("userAlert = %v", e)
	}
	for _, kind := range []string{"pairingRevoked", "noDataReceived", "setActiveMayFail"} {
		if len(entriesOfKind(entries, kind)) != 1 {
			t.Fatalf("%s not logged", kind)
		}
	}
	if e := entriesOfKind(entries, "tokenRefreshed"); len(e) != 1 || e[0]["tokenExpiry"] != "2030-01-02T03:04:05Z" {
		t.Fatalf("tokenRefreshed = %v", e)
	}
}

func TestStateChangesFromEveryPathAreLogged(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	enableDiag(t, d)
	d.setState(wire.StateConnecting, "")
	d.setState(wire.StateConnecting, "") // unchanged: not logged twice
	d.setPairErrorFor(context.Background(), "Not signed in to Google Messages", "Open messages.google.com")
	d.sessionMu.Lock()
	old, _ := d.resetSessionLocked()
	d.sessionMu.Unlock()
	if old != nil {
		old.Disconnect()
	}
	vias := map[string]bool{}
	for _, e := range entriesOfKind(readLogEntries(t, logPath), "state") {
		vias[e["via"].(string)] = true
	}
	for _, via := range []string{"setState", "pairFromBrowser", "resetSession"} {
		if !vias[via] {
			t.Fatalf("state change via %s not logged: %v", via, vias)
		}
	}
	count := 0
	for _, e := range entriesOfKind(readLogEntries(t, logPath), "state") {
		if e["via"] == "setState" && e["to"] == string(wire.StateConnecting) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("unchanged state logged %d times", count)
	}
}

func TestUnwrappedRequestsRecordAuthFailures(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	enableDiag(t, d)
	d.diagObserveAuthError("rpc:markRead", googleRequestError(401, googleUnauthBody))
	d.diagObserveAuthError("rpc:listContacts", errors.New("context deadline exceeded"))
	fail := entriesOfKind(readLogEntries(t, logPath), "authFailure")
	if len(fail) != 1 || fail[0]["where"] != "rpc:markRead" || fail[0]["withAuthRetry"] != false || fail[0]["requestUsesCookies"] != true {
		t.Fatalf("authFailure = %v", fail)
	}
}

func TestOmaChatCookieChangesAreLogged(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	d.mu.Lock()
	d.paired = true
	d.mu.Unlock()
	d.attachAuthObservers(d.auth)
	enableDiag(t, d)
	d.auth.UpdateCookiesFromResponse(httpResp(401, "application/json+protobuf", "SIDCC=v1", "__Secure-1PSIDTS=; Max-Age=0"))
	d.auth.CookiesLock.Lock()
	delete(d.auth.Cookies, "OSID")
	d.auth.CookiesLock.Unlock()
	d.sessionMu.Lock()
	d.saveSessionLocked()
	d.saveSessionLocked() // nothing changed: no second entry
	d.sessionMu.Unlock()

	entries := readLogEntries(t, logPath)
	set := entriesOfKind(entries, "omachatCookiesSet")
	if len(set) != 1 || set[0]["host"] != "instantmessaging-pa.googleapis.com" || set[0]["authFailure"] != true {
		t.Fatalf("omachatCookiesSet = %v", set)
	}
	names := set[0]["names"].([]any)
	if len(names) != 2 || names[1].(map[string]any)["cleared"] != true {
		t.Fatalf("names = %v", names)
	}
	saved := entriesOfKind(entries, "omachatCookiesSaved")
	if len(saved) != 1 {
		t.Fatalf("omachatCookiesSaved = %v", saved)
	}
	str := fmt.Sprint(saved[0]["changed"], saved[0]["added"], saved[0]["removed"])
	if !strings.Contains(fmt.Sprint(saved[0]["added"]), "SIDCC") || !strings.Contains(fmt.Sprint(saved[0]["changed"]), "__Secure-1PSIDTS") || !strings.Contains(fmt.Sprint(saved[0]["removed"]), "OSID") {
		t.Fatalf("save diff = %s", str)
	}
}

func TestCookieRefreshSuccessLogsDropsAndBrowserMetadata(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	d.diag.sqlite = func(_ context.Context, uri, query string) ([]byte, error) {
		if !strings.HasSuffix(uri, "?mode=ro&immutable=1") {
			t.Fatalf("browser database opened without immutable=1: %s", uri)
		}
		if strings.Contains(query, "pragma_table_info") {
			return []byte("name\nhost_key\nencrypted_value\ncreation_utc\nexpires_utc\nlast_update_utc\n"), nil
		}
		return []byte("SID|.google.com|13300000000000000|13300000000000001|13400000000000000|40|feedfacecafe\n"), nil
	}
	enableDiag(t, d)
	before := map[string]string{"SID": "a", "SIDCC": "c"}
	fresh := map[string]string{"SID": "b", "OSID": "o"}
	d.diagCookieRefreshSuccess(context.Background(), "rpc:send", "c7", browser.Profile{Name: "Chrome / Profile 1", CookieDB: "/x/Cookies"},
		[]logFields{{"profile": "Chrome / Default", "missing": []string{"OSID"}}}, before, fresh, []string{"SID"})
	e := entriesOfKind(readLogEntries(t, logPath), "cookieRefresh")
	if len(e) != 1 {
		t.Fatalf("cookieRefresh = %v", e)
	}
	r := e[0]
	if fmt.Sprint(r["removed"]) != "[SIDCC]" || fmt.Sprint(r["added"]) != "[OSID]" || r["profile"] != "Chrome / Profile 1" || r["skippedProfiles"] == nil {
		t.Fatalf("refresh entry = %v", r)
	}
	sid := r["cookies"].(map[string]any)["SID"].(map[string]any)
	if sid["differs"] != true || sid["omachat"] == sid["browser"] {
		t.Fatalf("SID comparison = %v", sid)
	}
	meta := r["browserMetadata"].([]any)[0].(map[string]any)
	if meta["name"] != "SID" || meta["lastUpdate"] == "" || meta["encryptedFP"] == "feedfacecafe" || meta["encryptedFP"] == nil {
		t.Fatalf("browser metadata = %v", meta)
	}
}

// The same scenarios with diagnostics off and on must leave identical state.
func TestDiagnosticsDoNotChangeBehaviour(t *testing.T) {
	type outcome struct {
		state  wire.ConnState
		msg    string
		paired bool
		calls  int
		errMsg string
	}
	scenarios := map[string]func(d *Daemon) (int, error){
		"sessionCookieInvalid": func(d *Daemon) (int, error) {
			calls := 0
			_, err := withAuthRetry(context.Background(), d, func() (string, error) {
				calls++
				return "", errors.New(`HTTP 401: [["type.googleapis.com/google.rpc.ErrorInfo",["SESSION_COOKIE_INVALID"]]]`)
			})
			return calls, err
		},
		"throttled401": func(d *Daemon) (int, error) {
			d.cookies.last = time.Now()
			calls := 0
			_, err := withAuthRetryAt(context.Background(), d, "rpc:send", func() (string, error) {
				calls++
				return "", errors.New("HTTP 401: invalid authentication credentials")
			})
			return calls, err
		},
		"media403": func(d *Daemon) (int, error) {
			d.cookies.last = time.Now()
			calls := 0
			_, err := withAuthRetryAt(context.Background(), d, "rpc:downloadMedia", func() ([]byte, error) {
				calls++
				return nil, &mediaHTTPError{status: 403}
			})
			return calls, err
		},
		"notAuth": func(d *Daemon) (int, error) {
			calls := 0
			_, err := withAuthRetry(context.Background(), d, func() (int, error) {
				calls++
				return 0, errors.New("context deadline exceeded")
			})
			return calls, err
		},
		"listenFatal401": func(d *Daemon) (int, error) {
			d.clientEventHandler(d.client, d.sessionContext())(&events.ListenFatalError{Error: events.HTTPError{Action: "polling", Resp: httpResp(401, "")}})
			return 0, nil
		},
		"listenRecovered": func(d *Daemon) (int, error) {
			d.clientEventHandler(d.client, d.sessionContext())(&events.ListenRecovered{})
			return 0, nil
		},
		"gaiaLoggedOutWhilePaired": func(d *Daemon) (int, error) {
			d.clientEventHandler(d.client, d.sessionContext())(&events.GaiaLoggedOut{})
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				d.mu.RLock()
				state := d.status.State
				d.mu.RUnlock()
				if state == wire.StateUnpaired {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			return 0, nil
		},
		"unpair": func(d *Daemon) (int, error) {
			calls := 0
			err := d.unpair(context.Background(), func(context.Context, *libgm.Client) error { calls++; return nil })
			return calls, err
		},
		"gaiaLoggedOutWhilePairing": func(d *Daemon) (int, error) {
			d.sessionMu.Lock()
			d.gaiaActive = true
			d.sessionMu.Unlock()
			d.mu.Lock()
			d.paired = false
			d.mu.Unlock()
			d.clientEventHandler(d.client, d.sessionContext())(&events.GaiaLoggedOut{})
			return 0, nil
		},
	}
	for name, run := range scenarios {
		var results []outcome
		for _, on := range []bool{false, true} {
			d, _, _ := newDiagDaemon(t)
			d.mu.Lock()
			d.paired = true
			d.status.State = wire.StateConnected
			d.mu.Unlock()
			if on {
				enableDiag(t, d)
			}
			calls, err := run(d)
			d.mu.RLock()
			results = append(results, outcome{d.status.State, d.status.Error, d.paired, calls, errText(err)})
			d.mu.RUnlock()
			d.stopDiagnostics()
		}
		if results[0] != results[1] {
			t.Fatalf("%s: off %+v, on %+v", name, results[0], results[1])
		}
	}
}

func TestDiagnosticsConcurrentToggleDeleteAndWrites(t *testing.T) {
	d, _, _ := newDiagDaemon(t)
	d.diag.log.maxBytes = 4096
	d.mu.Lock()
	d.paired = true
	d.mu.Unlock()
	d.attachAuthObservers(d.auth)
	d.diag.watcher.interval = time.Millisecond
	d.diag.watcher.profiles = func() []browser.Profile { return []browser.Profile{{Name: "P", CookieDB: "/x"}} }
	mod := time.Now()
	var modMu sync.Mutex
	d.diag.watcher.stat = func(string) (time.Time, error) {
		modMu.Lock()
		defer modMu.Unlock()
		mod = mod.Add(time.Second)
		return mod, nil
	}
	d.diag.watcher.read = func(context.Context, browser.Profile) ([]browser.CookieMeta, error) {
		return []browser.CookieMeta{{Name: "SID", Host: ".google.com", EncryptedHash: fmt.Sprint(time.Now().UnixNano())}}, nil
	}
	enableDiag(t, d)
	var wg sync.WaitGroup
	run := func(n int, f func(i int)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < n; i++ {
				f(i)
			}
		}()
	}
	run(30, func(i int) { _ = d.applyDebugLogging(i%2 == 0, "race") })
	run(20, func(int) { _ = d.DeleteDebugLog() })
	run(300, func(i int) { d.diagWrite("w", logFields{"i": i}) })
	run(100, func(int) { d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=x")) })
	run(50, func(int) { d.sessionMu.Lock(); d.saveSessionLocked(); d.sessionMu.Unlock() })
	run(50, func(i int) { d.setState(wire.StateConnecting, fmt.Sprint(i)) })
	run(50, func(int) { d.diagAuthFailure("rpc:x", errors.New("HTTP 401"), true) })
	wg.Wait()
	d.stopDiagnostics()
	if d.diag.watcher.running() {
		t.Fatal("watcher still running after stop")
	}
}

func TestStartupSummaryDistinguishesSessionFileStates(t *testing.T) {
	for name, tc := range map[string]struct {
		write   func(path string) error
		present bool
		decodes any
		paired  any
	}{
		"missing":  {nil, false, nil, nil},
		"corrupt":  {func(p string) error { return os.WriteFile(p, []byte("{not json"), 0o600) }, true, false, nil},
		"unpaired": {func(p string) error { return os.WriteFile(p, []byte(`{"cookies":{"SID":"x"}}`), 0o600) }, true, true, false},
	} {
		t.Run(name, func(t *testing.T) {
			d, paths, logPath := newDiagDaemon(t)
			if tc.write != nil {
				if err := tc.write(paths.SessionFile()); err != nil {
					t.Fatal(err)
				}
			}
			enableDiag(t, d)
			s := entriesOfKind(readLogEntries(t, logPath), "startup")[0]["session"].(map[string]any)
			if s["present"] != tc.present || s["decodes"] != tc.decodes || s["paired"] != tc.paired {
				t.Fatalf("session summary = %v", s)
			}
		})
	}
}

func TestSessionClearedReasonsAreLogged(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	enableDiag(t, d)
	if err := d.unpair(context.Background(), func(context.Context, *libgm.Client) error { return nil }); err != nil {
		t.Fatal(err)
	}
	cleared := entriesOfKind(readLogEntries(t, logPath), "sessionCleared")
	if len(cleared) != 1 || cleared[0]["reason"] != "unpair" {
		t.Fatalf("sessionCleared = %v", cleared)
	}
}

func TestRoutineCookieUpdatesAreSummarized(t *testing.T) {
	d, _, logPath := newDiagDaemon(t)
	now := time.Unix(5000, 0)
	d.diag.log.now = func() time.Time { return now }
	d.attachAuthObservers(d.auth)
	enableDiag(t, d)
	for i := 0; i < 30; i++ {
		d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=v"))
	}
	// A different cookie set, a deletion, and a failure are never hidden by
	// the routine SIDCC updates.
	d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=v", "__Secure-1PSIDTS=v"))
	d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=; Max-Age=0"))
	d.auth.UpdateCookiesFromResponse(httpResp(401, "", "SIDCC=v"))
	now = now.Add(11 * time.Minute)
	d.auth.UpdateCookiesFromResponse(httpResp(200, "", "SIDCC=v"))

	entries := readLogEntries(t, logPath)
	if got := len(entriesOfKind(entries, "omachatCookiesSet")); got != 6 {
		t.Fatalf("omachatCookiesSet entries = %d, want 2 routine + 3 distinct + 1 in the next window", got)
	}
	s := entriesOfKind(entries, "suppressed")
	if len(s) != 1 || s[0]["count"].(float64) != 28 || !strings.Contains(s[0]["key"].(string), "SIDCC=false") {
		t.Fatalf("suppressed summary = %v", s)
	}
}
