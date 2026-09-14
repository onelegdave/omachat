# OmaChat review fixes and verification

Date: 2026-09-14

Implementation addresses the confirmed findings in `review2.md` and
`review-audit.md`. Changes are local and uncommitted; no release was published.

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
- [x] Document the latest-60-message thread window. Pagination remains future work.
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

agy provided focused daemon/store regression tests and separate Go and UI
reviews. The lead checked and adjusted those tests, owned production edits,
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
