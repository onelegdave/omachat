# OmaChat Code Review

Historical record. See the [current documentation](../README.md) for setup and service capabilities.

Historical review of the initial implementation. See [FIX-VERIFICATION.md](FIX-VERIFICATION.md) and [LIVE-TEST-RESULTS.md](LIVE-TEST-RESULTS.md) for the fixes and current verification.

Review date: September 14, 2026\
Repository: https://github.com/onelegdave/omachat\
Reviewed commit: `81b2e4a3f538cd5f2aa6663f4a30e51dbc95ea37`

The local checkout matched GitHub's HEAD when reviewed. This review identified 10 actionable issues. The first three should be addressed before release.

## 1. High: The panel's helper build always fails

**Location:** [Service.qml, line 172](../../Service.qml#L172)

The build command places `-C` after `-mod=vendor`:

```qml
buildProc.command = ["/usr/bin/go", "build", "-mod=vendor", "-C", root.pluginDir, "-o", root.helperPath, "./cmd/omachatd"]
```

Running this command reproduced the error:

```text
-C flag must be first flag on command line
```

This blocks the documented installation flow through **Build helper**.

**Suggested correction:** Put the directory option before the subcommand:

```qml
buildProc.command = ["/usr/bin/go", "-C", root.pluginDir, "build", "-mod=vendor", "-o", root.helperPath, "./cmd/omachatd"]
```

## 2. High: Unpairing can recreate deleted credentials

**Location:** [internal/daemon/methods.go, line 314](../../internal/daemon/methods.go#L314)\
**Related code:** [internal/daemon/daemon.go, line 196](../../internal/daemon/daemon.go#L196)

`Unpair()` deletes the session file but leaves `d.paired` true and retains the authentication data. The periodic maintenance save or shutdown subsequently calls `saveSession()`, which can write those credentials back to disk.

A local probe using synthetic credentials reproduced a session loading as paired after unpairing and saving again. This demonstrates local credential persistence; it does not establish that Google will accept the revoked session.

**Suggested correction:** Disable persistence and reset authentication state during unpairing. Coordinate cleanup with concurrent saves and pairing operations so an operation already in flight cannot restore the previous session.

## 3. High: Session saving races with cookie updates

**Location:** [internal/store/store.go, line 108](../../internal/store/store.go#L108)

`SaveSession()` JSON-encodes the live authentication object. Meanwhile, libgm can update its cookies map when processing HTTP responses. The encoder does not acquire `AuthData.CookiesLock`, so it reads the map concurrently with those writes.

A targeted race-enabled probe reproduced data races between `SaveSession()` and libgm's `UpdateCookiesFromResponse()`. Concurrent map access can crash the daemon during normal credential updates.

**Suggested correction:** Serialize a synchronized snapshot of authentication data, respecting the locks used by its writers. Also serialize session persistence operations so concurrent saves do not compete over the same temporary file.

## 4. Medium: Re-pairing breaks attachment downloads until restart

**Location:** [internal/store/store.go, line 125](../../internal/store/store.go#L125)\
**Related code:** [internal/daemon/media.go, line 295](../../internal/daemon/media.go#L295)

`ClearSession()` removes the entire media directory. That directory is created during daemon startup, but it is not recreated when pairing again in the same process. Subsequent attachment writes fail.

A local probe modeled successful re-pairing after cleanup and reproduced:

```text
write media: open .../media/...png: no such file or directory
```

**Suggested correction:** Recreate the private media directory when resetting the session or before writing attachments. Reset the in-memory media and avatar caches as part of the same lifecycle transition.

## 5. Medium: Drafts carry over to another recipient

**Location:** [InboxView.qml, line 129](../../InboxView.qml#L129)

`selectConversation()` changes the selected recipient and clears the message list, but it neither clears the composer text nor saves drafts per conversation. Text written for one person therefore remains ready to send to the next selected person. Attachment captions also persist across conversation changes.

This finding follows from the conversation-selection and composer code; it was not tested by sending a live message.

**Suggested correction:** Store drafts by conversation ID and restore the appropriate draft on selection. Reset or associate attachment captions with their staged attachment and conversation.

## 6. Medium: Incoming messages can replace outgoing bubbles

**Location:** [InboxView.qml, line 484](../../InboxView.qml#L484)

Pending-message reconciliation checks whether an existing outgoing bubble has the same text as an event, but does not check whether that event is itself outgoing:

```qml
if (list[i].pending && list[i].fromMe && list[i].text === msg.text) {
  list[i] = msg
}
```

A JavaScript probe reproduced an incoming `OK` replacing a pending outgoing `OK`. Text matching also cannot reliably distinguish repeated identical sends or attachments with identical captions.

**Suggested correction:** Reconcile messages using stable message or transaction identifiers, with direction checks. Do not use text equality as the primary identity test.

## 7. Medium: Older message history is inaccessible

**Location:** [InboxView.qml, line 149](../../InboxView.qml#L149)

The UI requests the latest 60 messages and replaces the thread with that page. It ignores the pagination cursor and `hasMore` information returned by the daemon. There is no older-page loading path, despite the README advertising full thread history.

**Suggested correction:** Retain the cursor and load older pages on demand, merging them without duplicates and preserving scroll position. Until implemented, document the 60-message limit accurately.

## 8. Medium: Refresh does not refresh the inbox from Google

**Location:** [Service.qml, line 70](../../Service.qml#L70)\
**Related code:** [internal/daemon/methods.go, line 41](../../internal/daemon/methods.go#L41) and [line 189](../../internal/daemon/methods.go#L189)

`refreshConversations()` calls the daemon's `conversations` method, which returns only its cached list. It does not invoke the separate `refresh` method that fetches conversations from the phone. Consequently, the manual Refresh action cannot repair a stale inbox, although refreshing an open thread separately fetches its messages.

**Suggested correction:** Have an explicit user refresh invoke the daemon's network refresh, then retrieve the updated conversation list and surface any error. Keep routine cached reads separate from that action.

## 9. Medium: Failed image downloads become stuck

**Location:** [InboxView.qml, line 202](../../InboxView.qml#L202)

`requestMedia()` inserts an empty entry in `mediaPaths` before requesting an attachment. On failure, the entry remains. Subsequent initial attempts return immediately because the key is already present. Reopening or refreshing a thread within the same view does not reset this state.

A JavaScript probe simulated a transient failure and confirmed that two attempts produced only one service call. Exhausted pending retries have a similar problem.

**Suggested correction:** Track loading, success, and failure separately. Clear failed entries or provide an explicit retry path, including after the automatic retry budget is exhausted.

## 10. Medium: Links lose query parameters after an ampersand

**Location:** [Model.js, line 138](../../Model.js#L138)

`linkify()` escapes HTML before matching URLs. Escaping changes `&` to `&amp;`, while the URL regular expression stops at `&`.

Direct execution reproduced this input:

```text
https://example.com/?q=test&page=2
```

becoming a hyperlink whose destination is only:

```text
https://example.com/?q=test
```

The remaining parameter appears outside the hyperlink. This can break signed URLs, filters, timestamps, and other query-dependent links.

**Suggested correction:** Identify URL spans in the original text, validate their destinations, then HTML-escape both the surrounding text and generated link attributes.

## Verification and scope

The following checks passed against the existing project:

- Vendored Go build through the normal command-line build path.
- `omarchy plugin validate .`.
- `go vet -mod=vendor ./...`.
- `go test -mod=vendor -count=1 ./...`.
- `go test -race -mod=vendor -count=1 ./...`.

Additional isolated probes reproduced the helper-build failure, credential restoration after unpairing, missing media directory after re-pairing, cookie persistence race, incorrect message reconciliation, failed media retry suppression, and URL truncation. Go probes used temporary overlay files and synthetic data; JavaScript probes executed the relevant project functions.

Passing the existing race-enabled suite does not contradict the cookie race finding: the additional probe exercises concurrency that the existing tests do not cover.

A keyboard-interception claim in the existing `review1.md` was also checked with a minimal offscreen Qt test. A focused text field received the tested characters despite its parent's `Keys.BeforeItem` handler, so that claim is not included as a confirmed finding here.

No production code was changed during the review. Live Google pairing, revocation, and message sending were not exercised. This document records code findings and isolated reproductions, not a complete end-to-end certification of the plugin.
