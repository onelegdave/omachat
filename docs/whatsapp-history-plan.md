# WhatsApp older-history retrieval and bounded cache

Status: **In progress** — backend/config/storage slices are implemented and synthetic Go tests pass. Strict total physical-cache accounting, a byte-level memory cap, migration fault recovery, native UI behavior, and live-phone acceptance remain incomplete or unverified; do not treat this as finished.
Branch inspected: `merged-and-extended`.
Baseline inspected: `5429dac` (emoji search preference), with prior keyboard/emoji extensions and PR merges preserved.
This document is the execution handoff. Recheck the live worktree before implementing.

## Agreed scope

Improve older WhatsApp message availability only. Existing live messaging and cross-device behavior work for the user and should not be redesigned.

Target a WhatsApp-Web-like experience, not a promise of exact protocol/UI parity or complete phone history:

- Continue automatically ingesting recent history provided by the phone.
- Display cached messages immediately, including offline.
- Automatically fetch older pages as the user scrolls upward: local cache first, then an on-demand phone request when necessary.
- Preserve a keyboard-accessible Load older / Retry fallback.
- Show loading and useful unavailable/error states without losing the reading position.
- Do not crawl every conversation or perform a bulk history download in the background.
- Prioritize recent history across known conversations; do not promise history for every contact or a fixed number of days.
- Preserve view-once/ephemeral restrictions, service isolation, drafts, selected messages, reactions, unread state, and existing keyboard behavior.

Out of scope: full archival, calls, export/import tooling, changes to working send/reconnect behavior except necessary integration, and new remote account operations unrelated to older-history retrieval.

## Approved settings UX

Exactly one new select field:

**WhatsApp history cache**

Choices: **64 MB / 128 MB / 256 MB / 512 MB**.
Default: **128 MB**.
Use consistent MB units and conversion in the UI, configuration, implementation, and tests.

Helper text:

> Older cached messages are removed automatically when space is needed. This does not delete messages from WhatsApp. Photos and videos use the separate media cache.

Do not add retention-day, per-chat-count, eviction-policy, auto-sync, or bulk-download controls. Do not add a second usage/settings control. A usage display is not required for this version.

The budget covers retained message text, necessary message metadata, reaction state, attachment references/raw download metadata, and the history store's overhead. Downloaded media files and credentials are separate. Do not include the WhatsApp device/credential database in this history allowance or use it to store app history.

Raising the budget allows more history to accumulate; it does not initiate downloads. Lowering it triggers local-only eviction. Saving this preference must preserve every unrelated configuration field and must not require restarting all services.

## Non-negotiable personal-account boundary

The user will perform live testing manually at the end. Their WhatsApp account is a personal production account, and the agent must not read its data.

During implementation and automated verification:

- Do not read production conversation caches, WhatsApp databases, credentials, messages, account logs, or backups, even to inspect their schema or metadata.
- Do not connect a helper to the production session or daemon socket.
- Do not pair/re-pair, send messages, request phone history, log out, or unpair.
- Do not install/reload the plugin, restart the shell/helper, or launch the real app against normal account paths.
- Do not read repository `.env` or reuse authorization for other integrations as authorization for WhatsApp.
- Use synthetic fixtures, mock clients, isolated temporary data/config/cache/runtime paths, and no real-account network access.
- Resolve required directory/tool prerequisites without inspecting account files. Audit test/build launch behavior before execution.
- Native UI fixtures may use the desktop display but must use a synthetic service and isolated runtime; they must not connect to the live helper. Preserve display access when isolating runtime paths.
- Do not modify Hyprland configuration/bindings, commit, push, or rewrite merge history.

A successful synthetic suite is not live account compatibility. Report live phone-history availability and production UI acceptance as pending user testing.

## Verified code findings

These are source-inspection findings, not test results:

- `internal/whatsapp/backend.go`, `Messages`: now serves SQLite history pages with timestamp/ID cursors first; at a cached boundary it can begin an on-demand request. `HasMore` includes an in-flight/retryable request for the QML paging seam, while `CanFetchOlder` reports remote-fetch possibility separately.
- `internal/whatsapp/backend.go`, `ingestHistorySync`: imports received history, deduplicates by message ID within a chat, handles reactions, persists data, and emits conversation/status updates.
- That importer trims in-memory history to the newest `maxPersistedMessages` entries; SQLite now retains additional pages, but there is not yet a separately tested global memory budget.
- `internal/whatsapp/store.go`: conversation metadata is bounded to 1,000 recent chats; hot message/raw-media memory is bounded to the newest 100 messages in 50 chats. After successful SQLite migration/persistence, `whatsapp_store.json` is reduced to conversation metadata and hot reaction actors rather than duplicating message bodies/raw protobufs. The legacy JSON fallback remains if SQLite cannot open, and actor-index bytes plus SQLite peak journal/temp usage are not included in a proven strict physical cap.
- `internal/whatsapp/mock.go`: has a synthetic request seam; the live request path uses the pinned whatsmeow API, but no personal phone was contacted.
- Vendored `go.mau.fi/whatsmeow/send.go`, `BuildHistorySyncRequest`: builds a request anchored by chat ID, message ID, FromMe, and timestamp. Library recommends a page size of 50.
- `SendPeerMessage` sends the request to the primary device; response is an asynchronous `events.HistorySync` with type `ON_DEMAND`.
- The request's timestamp field name includes MS, but the vendored builder explicitly uses seconds. OmaChat message timestamps use microseconds. Preserve units deliberately.
- `InboxView.qml`: initial pages contain 60 messages; existing load-older callbacks guard service/selection/request generations. Viewport capture/restore and cursor-stall protection already exist.
- `internal/wire/wire.go`: now carries explicit history-fetch state and asynchronous history-update events; other services retain their existing paging behavior.
- `internal/store/config.go`, daemon config/method wiring, `Service.qml`, and `SettingsView.qml` form the preference integration path; inspect exact definitions/usages before editing.
- `tests/run-qml.py` uses mock fixtures and later isolates helper-build data/runtime. Audit isolation at the beginning of the run rather than assuming every fixture is safe.

## Execution phases

## Implementation notes and design-gate evidence

- **Response correlation:** pinned `events.HistorySync` exposes `Data` and its
  `Notification`; the payload has sync type, chunk order, progress, and
  conversation IDs, while the notification additionally has `originalMessageID`
  and `peerDataRequestSessionID`. The builder does not set a request-session ID,
  and no mapping from those notification fields to the outbound request ID is
  documented in the pinned source. The implementation therefore serializes to
  one on-demand request globally and matches `ON_DEMAND` payloads by chat; it
  does not claim strong request correlation. Same-chat stale responses and
  chunk-order semantics remain unverified. Completion accepts payload or
  notification progress 100, or conversation end-of-transfer markers. Empty
  completed responses are unavailable only when no earlier chunk was cached;
  completion-only notifications validate accumulated rows. Cached partial chunks
  remain available on retry after failure, but automatic intermediate UI refresh
  remains unverified. Timeout/disconnect remains retryable.
  These are source-based interpretations, not live phone compatibility evidence.
- **Direct-chat identity and names (user-approved):** the user reported that
  group history synced but some direct-chat requests failed with an error along
  the lines of “the phone returned history for a different conversation”; OmaChat
  also showed WhatsApp profile names instead of locally saved phone contacts.
  Match only exact JIDs, explicit history `PnJID`/`LidJID` aliases, or the pinned
  whatsmeow PN/LID mapping—never display names. Store an accepted on-demand
  chunk under the requested chat ID to avoid a duplicate alias row. Prefer local
  contact `FullName`/`FirstName`; retain the history/profile name as fallback.
  This behavior has synthetic-test evidence; phone compatibility awaits user retest.
- **Anchor units:** the pinned `BuildHistorySyncRequest` accepts chat, ID,
  FromMe, timestamp, and count; it writes `Timestamp.Unix()` seconds despite
  the protobuf field name containing `MS`. Its documented recommended page
  size is 50. A synthetic request-seam test verifies preserving group chat,
  message ID, FromMe, second precision, and the bounded count.
- **Storage direction:** a dedicated `whatsapp_history.sqlite` is separate
  from `whatsapp.db`, with indexed timestamp/ID paging, incremental vacuum,
  least-recently-viewed chat eviction, an eviction boundary, a SQLite page cap,
  and 0600 database permissions. Synthetic tests now cover paging after reopen,
  steady-state database-plus-sidecar size, retention of a viewed chat, eviction
  of an older unviewed chat, and budget shrink. A synthetic old-JSON migration
  test verifies the migrated row survives reopen without duplication. Raw attachment
  metadata is stored in SQLite and fetched on demand; reaction actors for hot
  chats remain in the bounded JSON index. Still unresolved: interruption at
  every migration/write boundary, reaction-actor byte accounting, legacy JSON
  fallback when SQLite is unavailable, peak temporary/journal usage, and crash
  recovery. The page cap/reserve is not evidence of a strict total physical or
  memory-budget guarantee.
- **Baseline evidence:** `make test`, `make lint`, and the synthetic QML
  readability fixture passed before feature changes. The full `make test-ui`
  baseline passed its Node and Python phases and several QML fixtures, then
  failed at `tests/qml/popout.qml` with a window-layout timeout. Isolated XDG
  paths and the real display socket were used; this failure is not attributed
  to the feature and remains an unresolved baseline regression.

## Current verification ledger (2026-10-07)

- **Verified synthetic backend/storage:** focused and full Go tests cover SQLite
  paging/reopen/offline reads, 50-message request anchors and count validation,
  exact/wrong-chat on-demand chunks, PN/LID alias matching via history fields
  and stored mapping, rejection of an unrelated direct chat despite a matching
  display name, saved-contact-name precedence in history/startup/contact events,
  completion from notification-only progress after an earlier chunk and from a
  conversation end marker, cache-backed retry after a partial-chunk failure,
  unknown transfer-enum rejection, empty response, timeout retry cooldown/expiry,
  disconnect,
  cache-write failure reporting, viewed-chat retention, eviction boundary and
  retained cursor anchors after eviction,
  database permissions, steady-state file-size budget, and local budget shrink.
- **Verified repository gates (after the latest edits):** `make test`,
  `make lint`, `go test -race -mod=vendor -count=1 ./...`, `make helper`, and
  `make validate` passed. The isolated installer test built and validated the
  staged plugin, verified helper-source identity and `.env` exclusion, and
  rejected symlinked parent/target paths without changing external contents.
- **Verified static QML:** `qmllint SettingsView.qml Service.qml InboxView.qml
  tests/qml/whatsapp-history-settings.qml` passed.
- **Verified native QML behavior:** `make test-ui` passed all 39 Node tests,
  29 Python tests, and serial QML fixtures. The pagination fixture covers
  pending-phone duplicate suppression, conversation-switch status reset, and
  completion-triggered page loading; the settings fixture covers cache-size
  save/reload.
- **Explicit storage gaps:** successful SQLite operation no longer duplicates
  message bodies/raw protobufs in the JSON index, and metadata now retains up to
  1,000 chats; when SQLite cannot open, the legacy JSON fallback remains. The
  actor-state index is outside the SQLite page budget, the 50-chat hot-memory
  bound is count-based rather than a byte cap, and peak temporary/journal usage
  and crash/interruption recovery lack acceptance tests. Do not claim the
  configured value is a strict total physical or memory cap until these gaps
  are fixed and verified.
- **Live-account boundary:** no WhatsApp account data, credentials, daemon
  socket, or live phone were accessed by the agent. User-reported partial manual
  testing: group history worked, while some direct-contact requests failed with
  the different-conversation error; the phone showed locally saved names while
  OmaChat showed WhatsApp-set names. The user did not share identifiers or
  message contents. Direct-chat response compatibility still needs a post-fix
  phone retest.

### 1. Baseline and focused design gate

Load the spec-driven-development and test-driven-development skills; read this plan, relevant project instructions, CONTRIBUTING, SECURITY, and current service documentation. Recheck branch/status, preserve existing changes, and inspect the concrete integration inventory.

Run the relevant existing baseline tests under audited isolation before edits. Do not use account data to reproduce the problem.

Resolve these implementation choices using the pinned vendored code and synthetic probes:

- Response correlation/completion: what request or chat metadata is available, how chunked/empty replies behave, and how stale or unrelated history batches are identified.
- Request lifecycle: bounded pending work, duplicate suppression, timeout/cancellation, reconnect/unpair generation guards, and no network waits while holding backend locks.
- History-storage representation: evaluate JSON versus a dedicated app-history SQLite store, using existing vendored dependencies. Prefer a paged store if JSON requires full-cache rewrites or unbounded memory. This is a local technical gate, not permission to inspect the production database.
- Physical budget accounting: define retained-store accounting, DB/index/journal overhead, cleanup/compaction policy, and bounded temporary migration/write overhead. Do not call a logical payload estimate a hard physical limit. Oversized pages/messages must have a safe bounded outcome.
- Native fixture safety: isolate config/data/cache/state/runtime before any real helper or Service.qml startup; use only mock-backed instances for UI behavior checks.

Record the chosen design and synthetic evidence in this document before broad implementation. Do not upgrade dependencies or demand re-pairing as a speculative workaround. If feasibility requires production access or changes agreed UX, stop and explain the blocker.

### 2. Durable history and cache budget

Start with failing synthetic persistence/eviction tests, then implement the minimum working store path.

- Replace fixed message/conversation truncation that would defeat older-history paging. Keep lightweight conversation/anchor metadata bounded without arbitrarily hiding all but 50 chats.
- Account for all retained history representations, including raw protobuf download metadata and reaction actors.
- Prioritize recent windows fairly across chats; evict older history from least-recently-viewed chats first. The budget takes precedence over recent-history targets.
- Persist meaningful recency/eviction state; background arrivals must not automatically count as a user viewing a chat.
- Preserve the currently displayed page in bounded transient memory, even if its disk entries are evicted. Do not pin arbitrary history forever or bypass the configured disk cap.
- Bound in-memory message/raw-payload retention separately; a disk allowance must not imply loading the entire cache into memory/UI models.
- Track gaps/evicted boundaries so paging cannot silently skip missing ranges, falsely claim exhaustion, or repeatedly refetch and evict the same page without progress.
- Preserve private permissions, restart durability, and safe handling of malformed data and write failures.
- If changing formats, make migration idempotent and interruption-safe. Test using synthetic old-format fixtures only. Validate migration before retiring the old cache, avoid indefinite duplicate retention, and never alter the credential DB.
- Do not erase local history silently on an error. Account for concurrent arrivals, retrieval, eviction, and budget changes.

### 3. On-demand phone retrieval

Add narrow, mockable client operations and the backend request state machine.

- Serve initial cached pages without waiting for the phone.
- At an older-history boundary, request preceding messages using a valid protocol anchor and bounded page size; preserve group/LID chat identity and timestamp units.
- Define a truthful missing-anchor outcome for empty chats; do not fabricate an anchor or promise inaccessible history.
- Permit only bounded in-flight requests; coalesce duplicate requests and apply cooldown/backoff for retries.
- Correlate responses with the right chat/session/request where the protocol permits; serialize conservatively if correlation is insufficient.
- Merge overlapping/out-of-order batches without duplicates, history reorder jumps, unread increments from backfill, preview regression, or overwriting newer live state with stale history data.
- Ensure newly received older pages reach the selected message view; conversation/status events alone do not prove this.
- Separate cached-page availability, remote-fetch possibility, pending, retryable failure, and phone-provided unavailability. Timeout/disconnect is not proof of the beginning of history.
- Handle late replies, unsolicited initial-history chunks, cancellation, session retirement, and service disable without leaking state into another chat/account.
- Keep wire changes backward-compatible for other services; do not change their pagination semantics.

### 4. Automatic upward-scroll loading and one setting

- Use existing viewport anchors and generation guards; extend them for delayed history responses as needed.
- Trigger WhatsApp-only loading when the user navigates toward the older boundary, through scrolling or keyboard navigation.
- Do not fetch merely because the view initially sits near the top, because prepending/restoring an anchor moved content, or because the viewport remains underfilled. Prevent fetch cascades.
- Keep Load older / Retry accessible. A failed automatic attempt must not cause an endless retry loop.
- Preserve selected message, drafts, focus, reading offset, and newly arriving live messages in both anchored panel and pop-out.
- Add the approved single select and helper text through validated config/RPC/UI wiring, defaulting older configurations safely to 128 MB.
- Synthetic UI tests must demonstrate settings save/reload, scroll-trigger behavior, delayed response, failure/retry, and switching conversations during a pending request.

### 5. Regression verification, documentation, and user handoff

Run narrow tests after each slice, then the full audited automated gates:

- `make test`
- `make lint`
- `make helper` (build only; audit launcher before running)
- `make test-ui` (synthetic fixtures; isolate from all real-account paths)
- `go test -race -mod=vendor -count=1 ./...`
- `make validate` if available and confirmed validation-only

Extend Go mock/store/config/wire tests, JavaScript pagination/model tests, and native QML fixtures. At minimum cover duplicate/chunked/delayed history, equal timestamps, cursor progress, request units/anchors, empty/unavailable replies, timeout/retry, offline cached paging, wrong-chat/stale replies, concurrent live arrivals, unread/preview preservation, reactions/media restrictions, budget shrink, oversized entries, orphan cleanup, reopen/migration, and no cross-service regression.

Update WhatsApp service documentation and README history limitations; update roadmap claims only with observed evidence. Do not claim exact WhatsApp Web parity or complete history availability.

Stop after synthetic verification and provide a short manual checklist for the user. Do not install/launch/restart the production app yourself, and do not ask for private message content or account screenshots. If deployment is needed, provide a reviewed user-run procedure consistent with existing local plugin workflows.

## Acceptance matrix

- [x] Cached history opens immediately and remains available after restart/offline (synthetic evidence).
- [ ] Upward navigation requests cached pages first, then phone history without a fetch cascade (mock/native fixture evidence).
- [ ] Delayed history prepends without losing reading position or message selection (native fixture evidence).
- [x] Backend loading, unavailable, and retryable failure remain distinct; timeout/disconnect never falsely marks history complete (synthetic lifecycle tests; native UI presentation remains unverified).
- [x] PN/LID alias history is matched and stored under the requested chat ID; unrelated direct chats are rejected even if their display name matches (synthetic tests).
- [x] Saved contact names take precedence over history/profile names when available, including startup refresh and contact events (synthetic tests).
- [ ] Duplicate/stale and overlapping history cannot corrupt current messages/unread state.
- [ ] Budget choices/default/save/reload work with exactly one new settings control.
- [ ] Eviction/accounting/cleanup/reopen/migration satisfy the documented bounded-storage contract.
- [ ] Current page stays visible while disk eviction is bounded; memory retention is also bounded.
- [ ] Existing service, keyboard/emoji, draft, and pop-out regressions pass.
- [x] The agent did not read personal account data or perform a live-account operation, install/reload, commit, or desktop-config change.
- [ ] User retests direct-chat history and confirms real phone-history availability and installed UI behavior (pending after the identity fix).

## User-only manual checklist

After the agent finishes synthetic verification and the user chooses to deploy:

1. Open a chat whose older history is currently missing and scroll upward repeatedly. Check that more history appears when the phone supplies it and that the reading position is stable.
2. Repeat in a group chat and in anchored/pop-out surfaces; use keyboard navigation and Retry where appropriate.
3. Switch chats during a request; confirm no messages land in the wrong view.
4. Open cached history while offline, then retry retrieval after reconnecting.
5. Reopen OmaChat and confirm retained history is still available. Save another cache size and confirm the preference survives reopening.
6. Confirm live send/receive and unread behavior still look normal. Budget stress/eviction correctness is primarily established with synthetic fixtures, not by inspecting personal stored messages.

Report pass/fail and non-sensitive symptoms only. Do not share conversation contents. Live checks remain explicitly unverified until reported by the user.
