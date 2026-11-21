package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.mau.fi/mautrix-gmessages/pkg/libgm"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/events"
	"go.mau.fi/mautrix-gmessages/pkg/libgm/gmproto"

	"github.com/onelegdave/omachat/internal/browser"
	"github.com/onelegdave/omachat/internal/buildinfo"
	"github.com/onelegdave/omachat/internal/store"
	"github.com/onelegdave/omachat/internal/wire"
)

// authDiag connects the daemon to the opt-in sign-in diagnostics log. Every
// method is a no-op while the Debug setting is off, and none of them changes
// what the daemon does.
type authDiag struct {
	log      *authLog
	sqlite   browser.SQLiteRunner
	discover func() []browser.Profile

	// ctlMu serializes switching on and off; it is never held while writing.
	ctlMu   sync.Mutex
	watcher *cookieWatcher

	stateMu sync.Mutex
	savedFP map[string]fingerprint

	calls atomic.Uint64
}

func newAuthDiag(paths *store.Paths) *authDiag {
	path, err := paths.DebugLogFile()
	return &authDiag{
		log:      newAuthLog(path, err),
		sqlite:   browser.DefaultSQLiteRunner,
		discover: browser.DiscoverProfiles,
		watcher:  newCookieWatcher(),
	}
}

func (d *Daemon) diagOn() bool { return d.diag != nil && d.diag.log.on() }

// secretsSnapshot returns the live credentials so text fields can be
// scrubbed of them. Callers must not hold d.mu or the auth CookiesLock.
func (d *Daemon) secretsSnapshot() []string {
	d.mu.RLock()
	auth := d.auth
	d.mu.RUnlock()
	if auth == nil {
		return nil
	}
	var secrets []string
	auth.CookiesLock.RLock()
	for _, v := range auth.Cookies {
		if len(v) >= 6 {
			secrets = append(secrets, v)
		}
	}
	if len(auth.TachyonAuthToken) > 0 {
		tok := auth.TachyonAuthToken
		secrets = append(secrets, string(tok), base64.StdEncoding.EncodeToString(tok),
			base64.RawStdEncoding.EncodeToString(tok), base64.URLEncoding.EncodeToString(tok),
			base64.RawURLEncoding.EncodeToString(tok))
	}
	auth.CookiesLock.RUnlock()
	return secrets
}

func scrubValue(v any, secrets []string) any {
	switch t := v.(type) {
	case string:
		return scrubString(t, secrets)
	case bodyText:
		return bodyText(scrubString(string(t), secrets))
	case []string:
		out := make([]string, len(t))
		for i, s := range t {
			out[i] = scrubString(s, secrets)
		}
		return out
	case logFields:
		out := make(logFields, len(t))
		for k, inner := range t {
			out[k] = scrubValue(inner, secrets)
		}
		return out
	case []logFields:
		out := make([]logFields, len(t))
		for i, inner := range t {
			out[i] = scrubValue(inner, secrets).(logFields)
		}
		return out
	default:
		return v
	}
}

func scrubString(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(s, secret) {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	return s
}

func (d *Daemon) diagWrite(kind string, fields logFields) {
	if !d.diagOn() {
		return
	}
	d.diag.log.write(kind, scrubValue(fields, d.secretsSnapshot()).(logFields))
}

func (d *Daemon) diagWriteLimited(key string, limit int, window time.Duration, kind string, fields logFields) {
	if !d.diagOn() {
		return
	}
	d.diag.log.writeLimited(key, limit, window, kind, scrubValue(fields, d.secretsSnapshot()).(logFields))
}

// --- switching on and off ---

// SetDebugLogging switches diagnostics on or off and persists the choice.
func (d *Daemon) SetDebugLogging(enabled bool) error {
	if enabled {
		if err := d.applyDebugLogging(true, "settings"); err != nil {
			return err
		}
		if err := d.config.SetDebugLogging(true); err != nil {
			d.applyDebugLogging(false, "")
			return err
		}
		return nil
	}
	d.applyDebugLogging(false, "")
	return d.config.SetDebugLogging(false)
}

// applyDebugLogging starts or stops the log and the browser watcher without
// touching the saved setting.
func (d *Daemon) applyDebugLogging(enabled bool, reason string) error {
	if d.diag == nil {
		return errors.New("diagnostics are unavailable")
	}
	d.diag.ctlMu.Lock()
	defer d.diag.ctlMu.Unlock()
	if !enabled {
		d.diag.watcher.stop()
		d.diag.log.disable()
		return nil
	}
	wasOn := d.diag.log.on()
	if err := d.diag.log.enable(); err != nil {
		return err
	}
	if !wasOn {
		d.diagStartup(reason)
		d.diagResetSavedBaseline()
	}
	parent := d.maintCtx
	if parent == nil {
		parent = context.Background()
	}
	d.diag.watcher.start(parent, d)
	return nil
}

func (d *Daemon) stopDiagnostics() {
	if d.diag == nil {
		return
	}
	d.diag.ctlMu.Lock()
	defer d.diag.ctlMu.Unlock()
	d.diag.watcher.stop()
	d.diag.log.disable()
}

// DeleteDebugLog removes auth.log and auth.log.1. It works whether logging
// is on or off; with logging on, a new log starts.
func (d *Daemon) DeleteDebugLog() error {
	if d.diag == nil {
		return errors.New("diagnostics are unavailable")
	}
	reopened, err := d.diag.log.remove()
	d.diag.watcher.resetBaseline()
	if reopened {
		d.diagWrite("logCleared", logFields{"build": buildinfo.SourceID})
		d.diagResetSavedBaseline()
	}
	return err
}

// --- startup summary ---

func (d *Daemon) diagStartup(reason string) {
	if !d.diagOn() {
		return
	}
	f := logFields{
		"reason":           reason,
		"build":            buildinfo.SourceID,
		"gmessagesEnabled": d.serviceEnabled(wire.NetworkGMessages),
		"session":          d.diagSessionFileSummary(),
	}
	d.mu.RLock()
	f["live"] = logFields{"paired": d.paired, "state": string(d.status.State), "hasClient": d.client != nil}
	d.mu.RUnlock()
	cfg := d.config.Get()
	f["selectedProfile"] = cfg.BrowserProfile
	var profiles []string
	for _, p := range d.diag.discover() {
		profiles = append(profiles, p.Name)
	}
	f["profiles"] = profiles
	d.diagWrite("startup", f)
}

// diagSessionFileSummary separates "missing", "unreadable or corrupt" and
// "not paired", which LoadSession reports identically.
func (d *Daemon) diagSessionFileSummary() logFields {
	path := d.paths.SessionFile()
	st, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return logFields{"present": false}
		}
		return logFields{"present": true, "statError": err.Error()}
	}
	out := logFields{"present": true, "modified": st.ModTime().UTC().Format(time.RFC3339), "size": st.Size()}
	data, err := os.ReadFile(path)
	if err != nil {
		out["readError"] = err.Error()
		return out
	}
	var auth libgm.AuthData
	if err := json.Unmarshal(data, &auth); err != nil {
		out["decodes"] = false
		return out
	}
	out["decodes"] = true
	out["paired"] = store.IsPaired(&auth)
	out["googleAccount"] = auth.IsGoogleAccount()
	if !auth.TachyonExpiry.IsZero() {
		out["tokenExpiry"] = auth.TachyonExpiry.UTC().Format(time.RFC3339)
	}
	out["cookies"] = d.diagFingerprints(auth.Cookies)
	return out
}

func (d *Daemon) diagFingerprints(cookies map[string]string) logFields {
	out := make(logFields, len(cookies))
	for name, value := range cookies {
		out[name] = d.diag.log.fp(value)
	}
	return out
}

// snapshotCookies copies the jar under its lock. The copy stays in memory
// and is only fingerprinted.
func snapshotCookies(auth *libgm.AuthData) map[string]string {
	if auth == nil {
		return nil
	}
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()
	out := make(map[string]string, len(auth.Cookies))
	for k, v := range auth.Cookies {
		out[k] = v
	}
	return out
}

// --- sign-in failures ---

var noCookieRequests = map[string]bool{"rpc:downloadMedia": true, "rpc:downloadThumbnail": true}

// describeAuthError extracts what is known about a failure without changing
// how it is classified. It never panics on a malformed error.
func describeAuthError(err error) (f logFields) {
	f = logFields{}
	if err == nil {
		return f
	}
	defer func() {
		if r := recover(); r != nil {
			f["describeError"] = fmt.Sprint("could not describe error: ", r)
		}
	}()
	f["err"] = safeErrorText(err)
	f["matched"] = safeAuthMatch(err)
	f["sessionInvalidMatched"] = strings.Contains(strings.ToLower(safeErrorText(err)), "session_cookie_invalid")
	var reqErr events.RequestError
	var httpErr events.HTTPError
	var mediaErr *mediaHTTPError
	switch {
	case errors.As(err, &reqErr):
		f["typed"] = "RequestError"
		if reqErr.Data != nil {
			f["code"] = reqErr.Data.GetType()
			f["message"] = reqErr.Data.GetMessage()
			var classes []string
			for _, c := range reqErr.Data.GetClass() {
				classes = append(classes, c.GetClass())
			}
			if len(classes) > 0 {
				f["classes"] = classes
			}
		}
		if reqErr.HTTP != nil {
			addHTTPErrorFields(f, *reqErr.HTTP)
		}
	case errors.As(err, &httpErr):
		f["typed"] = "HTTPError"
		addHTTPErrorFields(f, httpErr)
	case errors.As(err, &mediaErr):
		f["typed"] = "mediaHTTPError"
		f["status"] = mediaErr.status
		if mediaErr.status == http.StatusUnauthorized || mediaErr.status == http.StatusForbidden {
			for k, v := range describeBody(mediaErr.body, mediaErr.contentType, mediaErr.bodyLen) {
				f[k] = v
			}
		} else if mediaErr.contentType != "" {
			f["contentType"] = mediaErr.contentType
		}
	default:
		f["typed"] = "none"
	}
	return f
}

// safeAuthMatch is authErrorMatch for errors whose Error method may panic
// (an HTTPError without a response). The daemon's own checks are unchanged.
func safeAuthMatch(err error) (match string) {
	defer func() {
		if recover() != nil {
			match = ""
		}
	}()
	return authErrorMatch(err)
}

func safeErrorText(err error) (text string) {
	defer func() {
		if recover() != nil {
			text = fmt.Sprintf("<unprintable %T>", err)
		}
	}()
	return err.Error()
}

func addHTTPErrorFields(f logFields, he events.HTTPError) {
	if he.Action != "" {
		f["action"] = he.Action
	}
	if he.Resp == nil {
		f["status"] = 0
		return
	}
	f["status"] = he.Resp.StatusCode
	ct := he.Resp.Header.Get("Content-Type")
	if v := he.Resp.Header.Get("WWW-Authenticate"); v != "" {
		f["wwwAuthenticate"] = v
	}
	if names := setCookieNames(he.Resp.Header); len(names) > 0 {
		f["setCookieNames"] = names
	}
	if he.Resp.StatusCode == http.StatusUnauthorized || he.Resp.StatusCode == http.StatusForbidden {
		for k, v := range describeBody(he.Body, ct, len(he.Body)) {
			f[k] = v
		}
	} else if ct != "" {
		f["contentType"] = ct
	}
}

func setCookieNames(h http.Header) []string {
	var names []string
	for _, line := range h.Values("Set-Cookie") {
		name, _, _ := strings.Cut(line, "=")
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func statusOf(f logFields) string {
	if s, ok := f["status"].(int); ok {
		return strconv.Itoa(s)
	}
	return "?"
}

// diagAuthFailure records one failure that the daemon classified as a
// sign-in failure and returns a call ID that ties later entries to it.
func (d *Daemon) diagAuthFailure(where string, err error, wrapped bool) string {
	if !d.diagOn() {
		return ""
	}
	callID := fmt.Sprintf("c%d", d.diag.calls.Add(1))
	f := describeAuthError(err)
	f["where"] = where
	f["callID"] = callID
	f["authError"] = safeAuthMatch(err) != ""
	f["withAuthRetry"] = wrapped
	f["requestUsesCookies"] = !noCookieRequests[where]
	d.diagWriteLimited("authFailure:"+where+":"+statusOf(f), 3, time.Minute, "authFailure", f)
	return callID
}

// diagObserveAuthError records a sign-in-looking failure from a request that
// does not go through withAuthRetry. It changes nothing.
func (d *Daemon) diagObserveAuthError(where string, err error) {
	if err == nil || !d.diagOn() || safeAuthMatch(err) == "" {
		return
	}
	d.diagAuthFailure(where, err, false)
}

func (d *Daemon) diagAuthOutcome(callID, where string, f logFields) {
	if !d.diagOn() {
		return
	}
	f["where"] = where
	f["callID"] = callID
	d.diagWrite("authOutcome", f)
}

// --- browser cookie reads ---

func (d *Daemon) diagCookieRefresh(f logFields) {
	d.diagWrite("cookieRefresh", f)
}

func (d *Daemon) diagCookieRefreshSuccess(ctx context.Context, trigger, callID string, p browser.Profile, attempts []logFields, before, fresh map[string]string, changed []string) {
	if !d.diagOn() {
		return
	}
	var removed, added []string
	for name := range before {
		if _, ok := fresh[name]; !ok {
			removed = append(removed, name)
		}
	}
	for name := range fresh {
		if _, ok := before[name]; !ok {
			added = append(added, name)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	cookies := logFields{}
	names := map[string]bool{}
	for _, n := range browser.SignInCookieNames {
		names[n] = true
	}
	for n := range before {
		names[n] = true
	}
	for n := range fresh {
		names[n] = true
	}
	for n := range names {
		b, inBefore := before[n]
		fr, inFresh := fresh[n]
		if !inBefore && !inFresh {
			continue
		}
		cookies[n] = logFields{"omachat": d.diag.log.fp(b), "browser": d.diag.log.fp(fr), "differs": b != fr}
	}
	f := logFields{
		"trigger": trigger, "callID": callID, "result": "ok", "profile": p.Name,
		"changed": changed, "removed": removed, "added": added, "cookies": cookies,
	}
	if len(attempts) > 0 {
		f["skippedProfiles"] = attempts
	}
	metas, err := browser.ReadSignInMetadata(ctx, p, d.diag.sqlite)
	if err != nil {
		f["browserMetadataError"] = err.Error()
	} else {
		f["browserMetadata"] = d.diagCookieMetas(metas)
	}
	d.diagCookieRefresh(f)
}

func (d *Daemon) diagCookieMetas(metas []browser.CookieMeta) []logFields {
	out := make([]logFields, 0, len(metas))
	for _, m := range metas {
		out = append(out, logFields{
			"name": m.Name, "host": m.Host,
			"created": diagTime(m.Created), "lastUpdate": diagTime(m.LastUpdate), "expires": diagTime(m.Expires),
			"encryptedLen": m.EncryptedLen, "encryptedFP": d.diag.log.fp(m.EncryptedHash),
		})
	}
	return out
}

func diagTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// --- OmaChat's own copy ---

// attachAuthObservers connects a session's cookie updates to the log. The
// observer does nothing while logging is off.
func (d *Daemon) attachAuthObservers(auth *libgm.AuthData) {
	if auth == nil {
		return
	}
	auth.CookiesLock.Lock()
	auth.CookieObserver = d.diagCookieUpdate
	auth.CookiesLock.Unlock()
}

func (d *Daemon) diagCookieUpdate(u libgm.CookieUpdate) {
	if !d.diagOn() {
		return
	}
	names := make([]logFields, 0, len(u.Names))
	keyNames := make([]string, 0, len(u.Names))
	for _, n := range u.Names {
		names = append(names, logFields{"name": n.Name, "cleared": n.Cleared})
		keyNames = append(keyNames, n.Name+"="+strconv.FormatBool(n.Cleared))
	}
	sort.Strings(keyNames)
	// Google sets some cookies on nearly every request. Log the first two of
	// each distinct set per ten minutes and count the rest, so routine
	// updates cannot push sign-in failures out of the log.
	key := "cookieUpdate:" + u.Host + ":" + strconv.Itoa(u.Status) + ":" + strings.Join(keyNames, ",")
	d.diagWriteLimited(key, 2, 10*time.Minute, "omachatCookiesSet", logFields{
		"host": u.Host, "status": u.Status, "contentType": u.ContentType, "names": names,
		"authFailure": u.Status == http.StatusUnauthorized || u.Status == http.StatusForbidden,
	})
}

func (d *Daemon) diagResetSavedBaseline() {
	if d.diag == nil {
		return
	}
	d.mu.RLock()
	auth := d.auth
	d.mu.RUnlock()
	d.diag.stateMu.Lock()
	d.diag.savedFP = d.fingerprintMap(snapshotCookies(auth))
	d.diag.stateMu.Unlock()
}

func (d *Daemon) fingerprintMap(cookies map[string]string) map[string]fingerprint {
	out := make(map[string]fingerprint, len(cookies))
	for k, v := range cookies {
		out[k] = d.diag.log.fp(v)
	}
	return out
}

// diagSessionSaved logs which cookies changed in OmaChat's own copy since the
// previous save. Called after a successful save; the caller holds sessionMu.
func (d *Daemon) diagSessionSaved(auth *libgm.AuthData) {
	if !d.diagOn() || auth == nil {
		return
	}
	now := d.fingerprintMap(snapshotCookies(auth))
	d.diag.stateMu.Lock()
	prev := d.diag.savedFP
	d.diag.savedFP = now
	d.diag.stateMu.Unlock()
	var changed, removed, added []string
	for name, fp := range now {
		old, ok := prev[name]
		switch {
		case !ok:
			added = append(added, name)
		case old != fp:
			changed = append(changed, name)
		}
	}
	for name := range prev {
		if _, ok := now[name]; !ok {
			removed = append(removed, name)
		}
	}
	if len(changed)+len(removed)+len(added) == 0 {
		return
	}
	sort.Strings(changed)
	sort.Strings(removed)
	sort.Strings(added)
	fps := logFields{}
	for name, fp := range now {
		fps[name] = fp
	}
	d.diagWrite("omachatCookiesSaved", logFields{"changed": changed, "removed": removed, "added": added, "cookies": fps})
}

// --- connection and session events ---

func (d *Daemon) diagStateChange(via string, fromState wire.ConnState, fromMsg string, toState wire.ConnState, toMsg string) {
	if !d.diagOn() || (fromState == toState && fromMsg == toMsg) {
		return
	}
	d.diagWriteLimited("state:"+string(toState), 20, time.Minute, "state", logFields{
		"via": via, "from": string(fromState), "fromMessage": fromMsg, "to": string(toState), "toMessage": toMsg,
	})
}

func (d *Daemon) diagTokenExpiry() string {
	d.mu.RLock()
	auth := d.auth
	d.mu.RUnlock()
	if auth == nil {
		return ""
	}
	auth.CookiesLock.RLock()
	defer auth.CookiesLock.RUnlock()
	return diagTime(auth.TachyonExpiry)
}

// listenWhere names where a listener failure came from.
func listenWhere(err error) string {
	var httpErr events.HTTPError
	text := strings.ToLower(safeErrorText(err))
	switch {
	case errors.As(err, &httpErr) && httpErr.Action == "polling":
		return "longpoll"
	case strings.Contains(text, "failed to refresh auth token"):
		return "tokenRefresh"
	case strings.Contains(text, "failed to reconnect after pair success"):
		return "pairReconnect"
	default:
		return "listen"
	}
}

// diagLibgmEvent records sign-in-relevant libgm events before the daemon
// handles (or ignores) them. ignored is non-empty when the event will not be
// handled at all. The caller holds sessionMu but not d.mu.
func (d *Daemon) diagLibgmEvent(raw any, ignored string) {
	if !d.diagOn() {
		return
	}
	syncable := func() bool { return d.canSyncLocked() }
	notSyncable := func() string {
		if ignored != "" {
			return ignored
		}
		if !syncable() {
			return "notSyncable"
		}
		return ""
	}
	switch evt := raw.(type) {
	case *events.ListenFatalError:
		where := listenWhere(evt.Error)
		f := describeAuthError(evt.Error)
		f["where"] = where
		f["authError"] = safeAuthMatch(evt.Error) != ""
		f["ignored"] = notSyncable()
		d.diagWrite("listenFatal", f)
	case *events.ListenTemporaryError:
		d.diagWriteLimited("listenTemporary", 5, time.Minute, "listenTemporary", logFields{
			"err": safeErrorText(evt.Error), "where": listenWhere(evt.Error), "ignored": notSyncable(),
		})
	case *events.ListenRecovered:
		d.diagWriteLimited("listenRecovered", 5, time.Minute, "listenRecovered", logFields{"ignored": notSyncable()})
	case *events.PingFailed:
		f := logFields{"errorCount": evt.ErrorCount, "ignored": ignored}
		if evt.Error != nil {
			for k, v := range describeAuthError(evt.Error) {
				f[k] = v
			}
			f["authError"] = safeAuthMatch(evt.Error) != ""
		}
		d.diagWriteLimited("pingFailed", 5, time.Minute, "pingFailed", f)
	case *events.NoDataReceived:
		d.diagWrite("noDataReceived", logFields{"ignored": ignored})
	case *events.HackySetActiveMayFail:
		d.diagWrite("setActiveMayFail", logFields{"ignored": ignored})
	case *events.GaiaLoggedOut:
		reason := ignored
		if reason == "" {
			d.mu.RLock()
			paired := d.paired
			d.mu.RUnlock()
			if d.gaiaActive && !paired {
				reason = "unconfirmedPairing"
			}
		}
		d.diagWrite("gaiaLoggedOut", logFields{"ignored": reason})
	case *events.AuthTokenRefreshed:
		d.diagWrite("tokenRefreshed", logFields{"tokenExpiry": d.diagTokenExpiry(), "ignored": ignored})
	case *events.PairSuccessful:
		d.diagWrite("pairSuccessful", logFields{"ignored": ignored, "gaia": d.gaiaActive})
	case *events.ClientReady:
		d.diagWrite("clientReady", logFields{"conversations": len(evt.Conversations), "ignored": notSyncable()})
	case *events.PhoneNotResponding:
		d.diagWriteLimited("phone", 5, time.Minute, "phoneNotResponding", logFields{"ignored": ignored})
	case *events.PhoneRespondingAgain:
		d.diagWriteLimited("phone", 5, time.Minute, "phoneRespondingAgain", logFields{"ignored": ignored})
	case *events.BrowserActive:
		d.diagWrite("browserActive", logFields{"ignored": ignored})
	case *gmproto.RevokePairData:
		d.diagWrite("pairingRevoked", logFields{"ignored": ignored})
	case *gmproto.UserAlertEvent:
		alert := evt.GetAlertType().String()
		d.diagWriteLimited("alert:"+alert, 5, time.Minute, "userAlert", logFields{"alert": alert, "ignored": ignored})
	}
}

func (d *Daemon) diagEvent(kind string, f logFields) {
	d.diagWrite(kind, f)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return safeErrorText(err)
}

func (d *Daemon) diagStartupConnectFailed(err error) {
	if !d.diagOn() {
		return
	}
	f := describeAuthError(err)
	f["where"] = "startupConnect"
	f["authError"] = safeAuthMatch(err) != ""
	d.diagWrite("startupConnectFailed", f)
}

// mediaHTTPError is a failed attachment download. Its text is exactly what
// the download path has always returned; the extra fields are diagnostics.
type mediaHTTPError struct {
	status      int
	contentType string
	body        []byte
	bodyLen     int
}

func (e *mediaHTTPError) Error() string {
	return fmt.Sprintf("download failed: HTTP %d", e.status)
}
