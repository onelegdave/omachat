# OmaChat review audit

Historical record. See the [current documentation](../README.md) for setup and service capabilities.

Historical review of the initial implementation. See [FIX-VERIFICATION.md](FIX-VERIFICATION.md) and [LIVE-TEST-RESULTS.md](LIVE-TEST-RESULTS.md) for the fixes and current verification.

Date: 2026-09-14\
Auditor: OneLegDave (development notes on `review1.md` and `review2.md`)\
Reviewed commit: `81b2e4a` (GitHub `main` at time of the two reviews)\
Code: no production changes as part of this audit.

This is not a third review of the tree. It is a judgment on the two existing reviews: what I accept, what I would not treat as confirmed, what I would defend, and what I would do next.

Sources:

- `review1.md`: architecture and QML/Go findings, including a keyboard-interception claim.
- `review2.md`: commit-bound review with isolated probes. It reproduced several daemon bugs and did not include the keyboard claim after a Qt test.

---

## Summary

Both reviews are competent. Review 2 is the one I would treat as release-blocking: it reproduced the serious helper and credential bugs, and it disproved one of review 1's "critical" claims.

I agree with the overlapping technical findings. I do not agree that `"unread fire"` is a typo. I would harden keyboard handling without selling review 1's catcher claim as confirmed.

I would not unwind vendoring, the child-process helper, or the SSRF and download bounds.

---

## Accepted findings (fix before any release)

These are real. I would not argue them.

### Helper build flag order (`Service.qml`)

Both reviews. Reproduced: `-C` after `build` is rejected (`-C flag must be first flag on command line`).

`make helper` still works because it never uses `-C`. The panel **Build helper** button is the documented path for people without a binary, and it always fails. That is a broken install flow.

**Action:** `["/usr/bin/go", "-C", pluginDir, "build", "-mod=vendor", ...]`.

### Unpair can write credentials back (`methods.go`, `daemon.go`)

Review 2, probed. `Unpair()` deletes `session.json` but can leave `paired` and auth in memory. Maintenance or shutdown `saveSession()` can recreate the file.

This is local persistence, not proof that Google still accepts the device. Still a credential bug.

**Action:** Clear auth, set unpaired, disable persistence, and coordinate with in-flight saves so a concurrent save cannot restore the old session.

### Cookie map race in `SaveSession` (`store.go`)

Review 2, race-enabled probe. JSON-encoding the live auth object can race with libgm cookie map writes. Existing `go test -race` not covering that path is not a defense.

**Action:** Snapshot under `CookiesLock`. Serialize session writes so two saves do not share one tempfile.

### `linkify()` truncates at `&` (`Model.js`)

Both reviews. Reproduced. HTML-escaping runs before the URL regex, so `&` becomes `&amp;` and the match stops. Query URLs (YouTube `t=`, signed links, pagination) render wrong.

**Action:** Find URL spans in the original text, validate, then escape text and attributes.

### Failed media stays empty forever (`InboxView.qml`)

Both reviews (review 1 §3.1, review 2 §9). `_withMedia(key, "")` then `mediaPaths[key] !== undefined` blocks retries after a transient failure.

**Action:** Track loading / success / failure separately, or delete the key on failure so a later open or refresh can try again.

### Re-pair then attachments fail (`store.go`, `media.go`)

Review 2, probed. `ClearSession()` removes the media directory. Startup created it; the same process after unpair/re-pair does not. Writes fail with `no such file or directory`.

**Action:** `MkdirAll` on the media path after reset, and clear in-memory media and avatar caches in the same transition.

---

## Accepted findings (soon, not the same urgency)

### Settings destroys the inbox (`Panel.qml`)

Switching `sourceComponent` unloads `InboxView`. Draft text, search, and selected conversation die. Returning from Settings is an unselected inbox.

**Action:** Keep `InboxView` mounted and hide it, or persist draft and `selectedConvID` on the panel.

### Composer draft follows the next conversation (`InboxView.qml`)

`selectConversation()` does not clear or key the composer (or attachment caption) by conversation. Wrong-thread send risk. Review 2 did not send a live message; the code path is still enough.

**Action:** Per-conversation drafts, or at least clear on switch.

### Pending outgoing bubble replaced by incoming same text (`InboxView.qml`)

Reconciliation requires `fromMe` on the local pending row, not on the incoming event. An incoming `OK` can replace a pending outgoing `OK`. Text is a weak identity.

**Action:** Require matching direction. Prefer stable IDs over text equality.

### Refresh does not talk to Google (`Service.qml`, `methods.go`)

User Refresh calls cached `conversations`, not daemon `refresh`. An open thread can still fetch messages; the inbox list cannot repair staleness from that button.

**Action:** Explicit refresh should call `refresh`, then reload the list, and show errors.

### History is 60 messages; README implied more

The UI asks for 60 and ignores `hasMore` / cursor. "Full thread history" in earlier copy is overstated. Limits already mention an inbox of 50 conversations; the thread window is a separate lie.

**Action:** Document "latest 60 in a thread" until pagination exists. Then load older pages, merge without duplicates, keep scroll position.

### `initialSync` ignores shutdown; 60s vs 90s re-pair

Detached goroutine on `context.Background()` can keep hitting Google after `Stop()`. `dispatch` times out at 60s while `repairSession` may wait 90s.

**Action:** Bind sync to `maintCtx` (or equivalent). Make the two timeouts agree.

### Em dashes and British spelling vs `AGENTS.md`

US English, no em dashes. Most hits are inherited helper/CLI strings (`Normalise`, `initialised`, `unrecognised`, several em dashes in cookie/pair errors).

**Action:** US English. Replace em dashes with a colon, semicolon, or period.

---

## Not treated as a confirmed critical bug

### PanelKeyCatcher stealing search / settings / caption keys

Review 1 called this critical (`Keys.BeforeItem`, `blocked` only for composer and link confirm). Review 2 ran a minimal offscreen Qt test: a focused text field still received the tested characters. I trust that over reading the catcher stack.

**Action:** Still harden: `blocked` when Settings is open and when search, caption, or GIPHY key fields have focus. Do not claim that typing `1` always dumps the user to Google.

### BarWidget `serviceFor` not reactive

`chat` is a binding on an imperative method. If the widget loads before the service, the 500ms timer re-injects the same `null`. Plausible, not observed as a stuck widget after a normal shell restart in development.

**Action:** Cheap fix: re-query `serviceFor` inside `injectPanel` / `resolveChat`. Do not treat the plugin as dead without it.

---

## Defended

### `"unread fire"` is not a typo

Review 1 §4.1 wants `"unread thread(s)"`. That tooltip was intentional voice after a request for nerdy, non-critical copy. It is not a misspelling of "thread."

If the bar tooltip should be plainer, that is a product call, not a review defect.

### Architecture the reviews already praised

Vendored modules, no ELF in git, helper as a child of `omarchy-shell`, Unix socket, 0600/0700 paths, SSRF filter on avatar URLs, bounded downloads. I would not unwind those for "easier install."

### Scope of the unpair finding

Review 2 is explicit: local file restore, not "Google still accepts the revoked device." Fix local state anyway. Do not overclaim remote revocation.

### What neither review certified

No live Google pairing, revocation, or send in either document. Isolated probes are enough for the bugs above. I would not send live messages to "prove" draft-carry or pending-bubble replacement.

---

## Priority order I would take

1. Fix `go -C … build` in `Service.qml`.
2. Fix unpair so it cannot resurrect `session.json`; snapshot cookies under lock; recreate the media directory after clear.
3. Fix `linkify`, media retry lockout, pending-bubble direction, and user Refresh calling daemon `refresh`.
4. Keep inbox mounted across Settings; drafts per conversation; key-catcher `blocked` for all editors.
5. Honest README on the 60-message window; pagination later if "full history" is still the goal.
6. US English, no em dashes, `initialSync` cancellation, aligned re-pair timeout.
7. Ask whether `"unread fire"` stays. Leave the rest of the flavor unless something is unreadable.

I would not open a marketplace ticket until items 1 through 3 are in. I would not rewrite published history again for this. I would not auto-install Go or ffmpeg as part of the fixes.

---

## Mapping (review section to verdict)

| ID | Topic | Verdict |
|----|--------|---------|
| R1 2.1 / R2 1 | GUI `go build -C` | Accept. Fix now. |
| R1 2.2 | Key catcher hijacks fields | Not confirmed as stated. Harden anyway. |
| R1 2.3 / R2 10 | `linkify` and `&` | Accept. Fix now. |
| R1 2.4 | `serviceFor` binding | Plausible. Cheap fix. |
| R1 3.1 / R2 9 | Stuck empty media | Accept. Fix now. |
| R1 3.2 | `initialSync` vs `Stop` | Accept. Soon. |
| R1 3.3 | 60s vs 90s re-pair | Accept. Soon. |
| R1 3.4 | Settings unloads inbox | Accept. Soon. |
| R1 4.1 | `"unread fire"` | Defend as voice. Product call to change. |
| R1 4.2 / 4.3 | Em dashes, UK spelling | Accept. Polish. |
| R2 2 | Unpair restores session | Accept. Fix now. |
| R2 3 | Cookie save race | Accept. Fix now. |
| R2 4 | Media dir after re-pair | Accept. Fix now. |
| R2 5 | Draft follows next chat | Accept. Soon. |
| R2 6 | Incoming replaces pending | Accept. Soon. |
| R2 7 | No older history | Accept. Document, then paginate. |
| R2 8 | Refresh is cache-only | Accept. Soon. |
