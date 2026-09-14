# OmaChat review fixes and verification

Date: 2026-09-14

The review and image fixes were published in v0.1.1. The reconnect fix was
subsequently pushed to main. Later follow-ups are recorded below.

## Completed

- [x] Correct the GUI helper build command to put `go -C` before `build`.
- [x] Serialize account reset and session persistence. Unpair clears auth,
  cancels old account work, and prevents old events or shutdown saves from
  restoring credentials.
- [x] Protect JSON serialization with the auth cookie lock, serialize saves,
  and use private unique temporary files for atomic session/config writes.
  The small vendored token-writer synchronization change is recorded in NOTICE.
- [x] Recreate the private media directory and clear media, avatar, reaction,
  and conversation caches during account reset.
- [x] Keep drafts per conversation, clear attachment captions on recipient
  changes, and reject stale file-picker/download results after switching.
- [x] Preserve the inbox and its draft/selection while Settings is displayed.
- [x] Reconcile sends by transaction ID and direction, including delayed
  acknowledgements, identical text, and in-flight sends during refresh.
  Read receipts exclude local provisional IDs.
- [x] Send explicit Refresh through the daemon's network refresh RPC, then
  reload the conversation list. Keep refresh failures visible independently
  of thread-load success.
- [x] Allow failed media downloads to retry and reset retry state on refresh.
- [x] Preserve complete URL query strings while escaping links and surrounding
  text for rich-text rendering.
- [x] Tie sync, recovery, browser subprocesses, and old account cache writes to
  cancellable contexts. Recovery observes the request deadline.
- [x] Document the initial 60-message thread window. Older-history pagination was subsequently implemented; see below.
- [x] Normalize affected user-facing strings to US English and remove em dashes.
- [x] Guard delayed reaction errors against conversation changes.

## Verification results

| Check | Result |
| --- | --- |
| `go test -race -mod=vendor -count=1 ./...` | Passed |
| `go vet -mod=vendor ./...` | Passed |
| `omarchy plugin validate .` | Passed |
| `git diff --check` | Passed |
| `make test-ui`: JavaScript regressions | Six tests passed |
| Actual QML components with synthetic contacts | Passed: drafts, captions, Settings retention, stale callbacks, reconciliation, refresh errors, retries, account UI reset, and editor input |
| Actual panel Build helper button | Compiled with vendored modules and connected to an isolated unpaired daemon |
| Actual service refresh RPC | Reached the isolated daemon and surfaced its expected unpaired error |
| Rendered QML inbox | Visually inspected with synthetic contacts/messages |

Daemon regression tests use temporary data paths, synthetic credentials, and
a local rejecting HTTP proxy for revocation requests. They exercise concurrent
cookie writes/saves, unpair versus persistence, stale events, cache reset, and
cancellation. The QML helper test isolates the data, cache, and runtime paths.

The claimed keyboard interception was not reproduced: real search, caption,
and Settings key fields received the tested shortcut characters. The service
lookup binding also passed the focused reactivity probe. Neither claim was
treated as a confirmed defect. The intentional "unread fire" tooltip remains.

## Review assistance

Focused daemon/store regression tests and separate Go and UI reviews were used
throughout the fixes. The lead checked and adjusted those tests, owned production edits,
and ran final verification. Accepted follow-ups included cancellation guards,
the delayed reaction error, and retaining pending sends during refresh. The
suggested shallow auth snapshot was rejected because it would not synchronize
serialization with writers. A claimed missing `Pending` flag was also rejected
after checking the complete response literal.

## Live verification follow-up

Live testing was subsequently authorized and performed. See
[LIVE-TEST-RESULTS.md](LIVE-TEST-RESULTS.md) for the evidence and limits.
Pairing, remote revocation, text delivery, refresh, and attachment downloads
passed. The outgoing image-with-caption failure was isolated and fixed by
sending the attachment and caption separately. The final JPEG and caption both
reached OUTGOING_DELIVERED; the delivered attachment downloaded successfully.
Go race tests, vet, manifest validation, and expanded UI regression tests pass.
The user also confirmed that both the image and caption arrived on the phone.

The installed plugin was updated and the shell restarted. These live tests
finished before publication; Git tags and GitHub Releases record subsequent
publication. Automatic recovery from deliberately expired cookies
was not induced; re-pairing through the browser-cookie flow passed.


## Reconnect follow-up after v0.1.1

A restart exposed two additional defects: authentication failures could start
interactive Gaia pairing automatically, and initial sync announced connected
before fetching conversations. Transport recovery and pairing completion also
announced connected too early.

- Removed automatic pairing and its recovery-only entry points. Ordinary
  requests may refresh browser cookies and retry once; an invalid session or
  fatal authentication error requires the explicit Pair with Google action.
- Keep startup, transport recovery, and confirmed pairing in Connecting until
  authenticated conversation data arrives. Refresh can fetch while Connecting.
- Latch invalid sessions against late readiness events and sync responses.
  A new Gaia pairing replaces the client and cancels the old session context.
- Preserve the active pairing challenge through unrelated transport events.

Go race tests and vet pass, including focused regression coverage and a
local-proxy test that exercises an actual startup fetch while Connecting. QML/model checks and manifest validation pass. The installed helper
matches the new local build. Restarting it with the invalid stored session
showed the explicit renewal error with no pairing challenge during a 30-second
check. The user then explicitly paired through the panel and confirmed completion.
Live refresh returned 50 conversations. A subsequent shell/helper restart restored
the saved session, passed live refresh, and produced no pairing events or emoji
challenge during a 36-second observation. Credentials remained mode 0600.


## Unpair error reporting follow-up

The daemon previously discarded remote revocation errors, and the panel's
Unpair button discarded its RPC reply. Local cleanup still takes place first,
but a failed Google request now returns an error and publishes an actionable
warning while remaining locally unpaired. Local cleanup failures have distinct
copy; combined failures preserve both underlying causes.

The panel handles the reply, shows transport failures, and displays the complete
warning as wrapping text on the unpaired screen. Duplicate pending requests
are blocked. Late results cannot overwrite a newer Google or QR pairing.

Verification: Go race tests and vet, model/QML tests, and manifest validation
passed. Synthetic cases cover successful revocation, remote failure, local
filesystem failure, combined failures, persistence after failure, replacement
sessions, and QR pairing. The public unpair RPC was exercised against a locally
rejecting proxy. The warning was visually inspected with synthetic data.
Focused Go regressions were reviewed and strengthened during integration,
implemented the change, and verified the UI.

The helper and changed QML were installed and checked against the local files.
The existing live pairing survived restart and inbox refresh passed. This
unpair reporting follow-up was pushed to main, without a new release.


## Older-message pagination follow-up

Threads now offer Load older messages using the helper's existing cursor ID and
timestamp. Each request asks for 60 messages. Page merges deduplicate message
and transaction IDs, preserve newer live records over older overlapping data,
and retain loaded history during refresh. A new conversation resets the window.

The QML list restores the visible message and its pixel offset after prepending
history. Incoming events preserve that position; an intentional send scrolls to
the end. Requests are serialized for older pages, and stale replies are rejected
after refresh or conversation changes. Failures retain the cursor for retry.
Repeated cursor pairs stop paging with an explanatory message; empty pages with
advancing cursors remain pageable.

Verification: 16 model tests, the existing QML suite, a dedicated real-list
pagination suite, isolated helper-build checks, and manifest validation passed.
The pagination suite exercises more than 60 synthetic messages and checks the
visible message and pixel offset before and after updates. Focused review and
model regressions were checked and strengthened during integration.
A live self-chat test requested two five-message pages: the second returned five
distinct older messages and an advancing cursor. No messages were sent for that
test. UI rendering was inspected with synthetic content.

The pagination UI was installed and matched the local sources. The helper
reconnected after the shell restart and live inbox refresh passed. Pagination is included with the reconnect and unpair reporting fixes in
v0.1.2. See the Git tag and GitHub Release for publication history.


## WhatsApp integration and v0.2.0 release scope

OmaChat v0.2.0 extends the verified Google Messages plugin with a native WhatsApp
backend, dual-network UI routing, inbound sticker support, and persisted media.

- Native WhatsApp protocol backend in `internal/whatsapp` powered by vendored
  `whatsmeow` (MPL-2.0) and CGO sqlite (`github.com/mattn/go-sqlite3`).
- Explicit QR pairing path via the WhatsApp tab. The helper requests a pairing QR
  channel and updates status cleanly. Failures, context cancellations, and timeouts
  publish a retryable UNPAIRED state with actionable error text and retire the
  session generation so late QR goroutines cannot overwrite retry state.
- Dual-network isolation:
  - Separate credential stores: Google uses `session.json`; WhatsApp uses `whatsapp.db`
    and `whatsapp_store.json`.
  - Separate media directories: `media/` for Google, `media_whatsapp/` for WhatsApp.
  - Independent lifecycle: unpairing WhatsApp logs out the session and deletes its
    local database/store without affecting Google credentials, and vice versa.
  - Isolated drafts: composer drafts are namespaced by network so typing on one
    network is retained when viewing or working in the other.
- Inbound stickers: incoming WhatsApp stickers are parsed, rendered inline as image
  attachments (WebP), and cached locally. Sticker metadata persists in
  `whatsapp_store.json` across restarts and history syncs.
- Persisted media: bounded local persistence (up to 50 chats and 100 messages each)
  stores downloadable media metadata in `whatsapp_store.json` (mode 0600). Downloads
  use atomic temporary files before renaming to opaque chat/message paths, and
  failed downloads display an in-line retry action.
- Deliberate view-once behavior: view-once and ephemeral messages (`IsViewOnce`,
  `IsEphemeral`, and wrapper envelopes) are deliberately not persisted to disk,
  cached, or redownloaded. A clear placeholder message is rendered to respect
  sender privacy.

Verification results:
- `go test -race -mod=vendor -count=1 ./...` passed across all packages.
- `go vet -mod=vendor ./...` passed.
- `omarchy plugin validate .` passed.
- `git diff --check` passed.
- `make test-ui` passed: 16 JavaScript regressions, pagination tests, helper build
  and refresh RPC checks, and full QML test suite covering network switching,
  draft isolation, tab navigation, sticker rendering, and WhatsApp unpair.
