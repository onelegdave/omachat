# OmaChat Code and Architecture Review

Historical record. See the [current documentation](../README.md) for setup and service capabilities.

Historical review of the initial implementation. See [FIX-VERIFICATION.md](FIX-VERIFICATION.md) and [LIVE-TEST-RESULTS.md](LIVE-TEST-RESULTS.md) for the fixes and current verification.

Review date: 2026-09-14\
Repository: [omachat](../../README.md)

---

## 1. Executive Summary

OmaChat is well architected for the Omarchy desktop shell:
- Protocol helper (`omachatd`) is cleanly owned by the shell as a child process via a private Unix domain socket (`$XDG_RUNTIME_DIR/omachat/daemon.sock`), avoiding unnecessary systemd user units.
- Dependencies are vendored under [vendor/](../../vendor), and builds rely strictly on `go build -mod=vendor` without shipping compiled ELFs in git.
- Strong security hardening: SSRF defense on avatar URLs via a custom dialer filtering loopback, RFC 1918, link-local, and CGNAT IP ranges; strict bounding on incoming attachments ([`download.go`](../../internal/daemon/download.go)); cache eviction limits; and safe file permissions (0600 / 0700).
- Plugin manifest validation passes cleanly (`omarchy plugin validate .`).

However, there are several concrete functional bugs, UI interaction bugs, and project convention issues that should be addressed.

---

## 2. Critical Bugs and Broken Functionality

### 2.1 Broken Helper Build via GUI (`go build -C` flag ordering)
- **Location**: [`Service.qml` (line 172)](../../Service.qml#L172)
- **Code**:
  ```qml
  buildProc.command = ["/usr/bin/go", "build", "-mod=vendor", "-C", root.pluginDir, "-o", root.helperPath, "./cmd/omachatd"]
  ```
- **Issue**: In the Go CLI, `-C` is a global flag to `go` itself and must precede the subcommand. Passing `-C` after `build` causes Go to reject the command with exit code 2:
  ```
  invalid value "<checkout>" for flag -C: -C flag must be first flag on command line
  usage: go build [-o output] [build flags] [packages]
  ```
- **Impact**: Clicking **Build helper** in the panel always fails and cannot compile the daemon.
- **Fix**: Reorder the array arguments so `-C` precedes `build`:
  ```qml
  buildProc.command = ["/usr/bin/go", "-C", root.pluginDir, "build", "-mod=vendor", "-o", root.helperPath, "./cmd/omachatd"]
  ```

---

### 2.2 `PanelKeyCatcher` Hijacks Input from Text Fields
- **Location**: [`Panel.qml` (line 85)](../../Panel.qml#L85), [`InboxView.qml`](../../InboxView.qml), [`SettingsView.qml`](../../SettingsView.qml)
- **Code**:
  ```qml
  PanelKeyCatcher {
    id: keyCatcher
    anchors.fill: parent
    blocked: bodyLoader.item && (bodyLoader.item.composerFocus === true || bodyLoader.item.linkConfirmOpen === true)
    onCloseRequested: root.close()
    onTabRequested: function(direction) { root.switchPanel(direction) }
    onTextKey: function(t) {
      if (t === "1") { root.settingsOpen = false; root.activeService = "gmessages" }
      else if (t === "2") { root.settingsOpen = false; root.activeService = "whatsapp" }
      else if (t === "3") { root.settingsOpen = false; root.activeService = "telegram" }
      else if (t === "r" || t === "R") root.refresh()
    }
  ```
- **Issue**: `PanelKeyCatcher` runs with `Keys.priority: Keys.BeforeItem`, intercepting keystrokes before any child item receives them unless `blocked` is true. Currently, `blocked` only checks `composerFocus` and `linkConfirmOpen`. However:
  1. `searchField` ("Hunt a thread" in [`InboxView.qml` lines 643-651](../../InboxView.qml#L643-L651)) does not update `composerFocus`.
  2. `attachCaption` (attachment caption in [`InboxView.qml` line 1308](../../InboxView.qml#L1308)) does not update `composerFocus`.
  3. `keyField` (GIPHY API key field in [`SettingsView.qml` line 210](../../SettingsView.qml#L210)) does not update `composerFocus`, and `composerFocus` is not defined on `SettingsView`.
- **Impact**: While typing in the search bar, typing an attachment caption, or pasting/editing a GIPHY API key:
  - Typing `1`, `2`, or `3` immediately switches service tabs and exits Settings or closes the thread.
  - Typing `r` or `R` triggers a refresh.
  - Typing `j`, `k`, `h`, `l`, `x`, `Space`, or `Return` is intercepted by navigation and activation shortcuts.
- **Fix**:
  1. In [`Panel.qml`](../../Panel.qml#L85), set:
     ```qml
     blocked: root.settingsOpen || (bodyLoader.item && (bodyLoader.item.editorFocus === true || bodyLoader.item.linkConfirmOpen === true))
     ```
  2. In [`InboxView.qml`](../../InboxView.qml), expose `readonly property bool editorFocus: composerFocus || (searchField && searchField.activeFocus) || (attachCaption && attachCaption.activeFocus)` and update `composerFocus` appropriately.

---

### 2.3 URL Truncation on Ampersands in `linkify()`
- **Location**: [`Model.js` (lines 139-148)](../../Model.js#L139-L148)
- **Code**:
  ```javascript
  function linkify(raw) {
    var e = escapeHtml(raw)
    return e.replace(/https?:\/\/[^\s<&]+/gi, function(m) {
      ...
    })
  }
  ```
- **Issue**: `escapeHtml(raw)` converts `&` to `&amp;`. The URL regex character class `[^\s<&]+` stops matching immediately upon hitting `&`.
- **Impact**: Any link containing query parameters (such as YouTube timestamps `https://youtube.com/watch?v=xxx&t=60s` or search links `https://example.com/?q=test&page=2`) is truncated at the first `&`. The rendered hyperlink will only link to `https://example.com/?q=test`, leaving `&amp;page=2` orphaned as unlinked plain text.
- **Fix**: Match URLs before HTML-escaping, or allow `&amp;` inside the URL matching pattern and decode entities before validating with `safeHttpUrl`.

---

### 2.4 Service Resolution Reactivity Failure in `BarWidget.qml`
- **Location**: [`BarWidget.qml` (lines 9-11, 51-56)](../../BarWidget.qml#L9-L11)
- **Code**:
  ```qml
  readonly property var chat: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor("onelegdave.omachat")
    : null
  ...
  Timer {
    interval: 500
    running: root.chat === null
    repeat: true
    onTriggered: root.injectPanel()
  }
  ```
- **Issue**: `bar.shell.serviceFor()` is an imperative method on the shell, not a QObject property with a change notification signal. If `BarWidget` loads before `Service.qml` registers itself, `root.chat` evaluates to `null`. Because `bar` and `bar.shell` do not change, QML never re-evaluates the `chat` property binding. The timer triggers `root.injectPanel()`, which continues assigning `target.service = root.chat` (statically `null`).
- **Impact**: If `BarWidget` initializes before `Service.qml`, the widget stays disconnected until a shell restart or external event forces a rebind.
- **Fix**: Re-query `bar.shell.serviceFor("onelegdave.omachat")` inside `injectPanel()` or an explicit `resolveChat()` helper:
  ```qml
  property var chat: null
  function resolveChat() {
    if (bar && bar.shell && typeof bar.shell.serviceFor === "function") {
      var s = bar.shell.serviceFor("onelegdave.omachat")
      if (s) root.chat = s
    }
    return root.chat
  }
  ```

---

## 3. Logic Quirks and Edge Cases

### 3.1 Permanent Media Lockout on Transient Network/Socket Errors
- **Location**: [`InboxView.qml` (lines 202-215)](../../InboxView.qml#L202-L215)
- **Code**:
  ```qml
  function requestMedia(key, attempt) {
    if (!key || !service) return
    var tries = attempt === undefined ? 0 : attempt
    if (tries === 0 && mediaPaths[key] !== undefined) return
    if (tries === 0) _withMedia(key, "")
    service.call("media", { key: key }, function(ok, res) {
      if (ok && res && res.path) {
        _withMedia(key, res.path)
        if (res.thumbnail && tries < 6) mediaRetry.schedule(key, tries + 1)
        return
      }
      if (ok && res && res.pending && tries < 6) mediaRetry.schedule(key, tries + 1)
    })
  }
  ```
- **Issue**: `_withMedia(key, "")` records `mediaPaths[key] = ""`. If the daemon returns an error (`!ok`) or retries expire, `mediaPaths[key]` remains `""`. Subsequent calls check `if (tries === 0 && mediaPaths[key] !== undefined) return` and exit immediately.
- **Impact**: If an attachment download fails due to a temporary socket timeout or network interruption, the image becomes permanently stuck in an empty state and will not be re-requested when reopening or refreshing the thread.
- **Fix**: In the failure branch (`!ok`), remove `key` from `mediaPaths` so subsequent view activations or manual refreshes can re-attempt the fetch.

---

### 3.2 `initialSync` Disregards Daemon Shutdown Context
- **Location**: [`internal/daemon/daemon.go` (lines 160-183)](../../internal/daemon/daemon.go#L160-L183)
- **Code**:
  ```go
  func (d *Daemon) initialSync() {
      time.Sleep(3 * time.Second)
      d.setState(wire.StateConnected, "")

      for attempt := 1; attempt <= 3; attempt++ {
          ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
          err := d.Refresh(ctx)
          cancel()
          ...
          time.Sleep(time.Duration(attempt*5) * time.Second)
      }
  }
  ```
- **Issue**: `initialSync()` runs in a detached goroutine using `context.Background()` with fixed sleeps and up to 3 retries (lasting up to 2.5 minutes). If `d.Stop()` is called shortly after startup (e.g. plugin disabled or shell restarting), `initialSync` keeps running in the background and querying conversations against a disconnected client.
- **Fix**: Tie `initialSync` to `d.maintCtx` (or a context cancelled on `Stop()`) and check `select { case <-ctx.Done(): return }`.

---

### 3.3 Context Timeout Mismatch During Automatic Re-Pair
- **Location**: [`internal/daemon/server.go` (line 200)](../../internal/daemon/server.go#L200) vs [`internal/daemon/authretry.go` (line 138)](../../internal/daemon/authretry.go#L138)
- **Issue**: In `server.go`, `dispatch()` sets a 60-second request deadline:
  ```go
  ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
  ```
  In `authretry.go`, `repairSession()` waits up to 90 seconds for pairing to complete:
  ```go
  for i := 0; i < 90; i++ {
      time.Sleep(time.Second)
      ...
  }
  ```
- **Impact**: If automatic re-pairing takes between 60 and 90 seconds, the parent request context will time out with `context.DeadlineExceeded`, failing the user action even if re-pairing eventually completes.
- **Fix**: Cap `repairSession` polling at 45 seconds, or adjust the parent timeout.

---

### 3.4 Composer Draft and Selection Loss on Opening Settings
- **Location**: [`Panel.qml` (lines 236-242)](../../Panel.qml#L236-L242)
- **Issue**: Toggling `settingsOpen` switches `bodyLoader.sourceComponent` between `inboxView` and `settingsView`.
- **Impact**: Switching components unloads and destroys `InboxView`. Any drafted message text in the composer, active search text, and selected conversation ID are lost. When returning from Settings, the user sees an unselected inbox.
- **Fix**: Keep `inboxView` mounted and toggle visibility/stacking instead of re-creating the component, or save the draft and `selectedConvID` in parent properties.

---

## 4. Project Conventions and Polish

### 4.1 Typo in Tooltip (`"unread fire"`)
- **Location**: [`BarWidget.qml` (lines 75-77)](../../BarWidget.qml#L75-L77)
- **Code**:
  ```qml
  tooltipText: root.unread > 0
    ? root.unread + (root.unread === 1 ? " unread fire" : " unread fires")
    : "OmaChat"
  ```
- **Correction**: Change `" unread fire"` / `" unread fires"` to `" unread thread"` / `" unread threads"` (or `" unread message"` / `" unread messages"`).

---

### 4.2 Em Dashes in User-Facing Copy
- **Rule**: `AGENTS.md`: *"US English in user-facing copy. No em dashes."*
- **Occurrences**:
  - [`cmd/omachatd/pair.go` (lines 25, 27)](../../cmd/omachatd/pair.go#L25):
    - `by page script — they have to be copied...` -> replace with colon or comma.
    - `The easy way — read them straight...` -> replace with colon or hyphen.
  - [`internal/wire/cookies.go` (line 38)](../../internal/wire/cookies.go#L38):
    - `could not find any cookies — paste a JSON object...` -> replace with colon or semicolon.
  - [`internal/daemon/daemon.go` (line 370)](../../internal/daemon/daemon.go#L370):
    - `Google signed this device out — pair again` -> replace with colon or semicolon.
  - [`internal/daemon/gaia.go` (lines 108, 118)](../../internal/daemon/gaia.go#L108):
    - `missing required cookie(s): %s — copy them...` -> replace with semicolon or period.
    - `They expire quickly — re-copy them...` -> replace with period or semicolon.

---

### 4.3 British/Commonwealth Spellings in User-Facing Strings
- **Rule**: `AGENTS.md`: *"US English in user-facing copy. No em dashes."*
- **Occurrences**:
  - [`manifest.json` (line 40)](../../manifest.json#L40): `"label": "Normalise voice recording level"` -> change to `"Normalize voice recording level"`.
  - [`cmd/omachatd/pair.go` (line 89)](../../cmd/omachatd/pair.go#L89): `"unrecognised arguments"` -> change to `"unrecognized arguments"`.
  - [`internal/daemon/methods.go` (line 223)](../../internal/daemon/methods.go#L223) & [`internal/daemon/gaia.go` (line 39)](../../internal/daemon/gaia.go#L39): `"client not initialised"` -> change to `"client not initialized"`.

---

## 5. Priority Action Items

1. **Fix Helper Build Command**: Fix `-C` flag placement in [`Service.qml`](../../Service.qml#L172).
2. **Fix `PanelKeyCatcher`**: Block key interception when typing in search, caption, or settings fields in [`Panel.qml`](../../Panel.qml#L85).
3. **Fix Linkifier**: Prevent query parameter truncation on `&` in [`Model.js`](../../Model.js#L140).
4. **Fix Service Resolution**: Ensure `BarWidget.qml` dynamically queries `bar.shell.serviceFor("onelegdave.omachat")`.
5. **Fix Tooltip and Style Conventions**: Correct `"unread fire"`, remove em dashes, and standardize on US English.
