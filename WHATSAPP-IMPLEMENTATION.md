# WhatsApp Implementation Plan and Checkpoint

## Status: REVIEWABLE IMPLEMENTATION (final fixes applied by agy 2026-09-14)
- Worktree: `/home/onelegdave/Projects/omachat-whatsapp`
- Branch: `feature/whatsapp` (based on v0.1.2)
- Manifest version: 0.1.2 (no release packaging)
- Human owner / maintainer: OneLegDave
- Codex owns final review, live pairing, and installed verification
- This checkpoint was completed by Grok after agy was interrupted

Do not treat older checkboxes as current. The items below reflect the tree at handoff.

---

## 1. Architecture

- Library: vendored `go.mau.fi/whatsmeow` (MPL-2.0)
- Session DB: `github.com/mattn/go-sqlite3` via CGO (`CGO_ENABLED=1`)
- Process: one shell-owned `omachatd` helper. No systemd unit. No extra daemon.
- Routing: `wire.Request.Network` is `"whatsapp"` or omitted/`"gmessages"`. Unknown networks error. Omitted still means Google for compatibility. QML chat calls pass an explicit network.
- Isolation: Google uses `session.json` and `media/`. WhatsApp uses `whatsapp.db`, `whatsapp_store.json`, and `media_whatsapp/`. Unpair of one never deletes the other.

---

## 2. Completed in this pass

### Lifecycle (Codex regressions, including review 2)
- Event handlers capture client/session generation at registration.
- Unpair increments generation and clears in-memory maps under the same lock, then `Logout` while still connected, then disconnect, close sqlite, delete files. RemoveEventHandler runs after Disconnect, never from inside an event callback.
- Phone `LoggedOut` is applied on a goroutine that takes `sessionMu`, rechecks generation, then removes the handler. The mock now holds its handler read-lock across callbacks (whatsmeow `dispatchEvent` shape). TryLock-drop of LoggedOut is gone.
- Send/media/QR/connect/message/receipt/history commit generation check and mutation under one lock. Media download writes a temp file, then rename only if generation still matches.
- Local cleanup failure is stored in WhatsApp status and blocks `StartPairing` until files can be removed. Google files are not touched.
- Next pairing is explicit `StartPairing` only.

### Persistence and media
- Conversations, messages, and downloadable media protobufs persist in `whatsapp_store.json` (0600).
- Maps are bounded to the newest 50 conversations and 100 messages each; evicted media metadata is dropped.
- Media keys are `chatID + messageID`. View-once and ephemeral (`IsViewOnce`/`IsEphemeral` plus wrappers) are omitted and shown as a placeholder; they are not stored or re-downloaded.
- Declared FileLength is checked before download. Download uses `DownloadToFile` into a size-capped tempfile, then rename (does not follow a dest symlink via WriteFile).
- sqlite file is created 0600 before open.

### UI routing
- Inbox, pairing, settings, and profiles pass an explicit `network` argument.
- `Service.call` no longer defaults to shared `currentNetwork` (omitted still means Google).
- Tab and shortcut switches go through `setActiveService`, which updates `currentNetwork`.
- Search and caption fields set `composerFocus` so `PanelKeyCatcher` (`Keys.BeforeItem`) does not steal shortcut characters.
- Telegram stays Coming Soon and does not send.

### History copy
- whatsmeow `BuildHistorySyncRequest` can request on-demand phone history. This version pages cached companion history only. UI copy says that. It is an OmaChat implementation limit, not a protocol impossibility.

---

## 3. Tests

Added or extended in review 2:
- LoggedOut vs handler read-lock (timeout if deadlock)
- Concurrent message events vs Unpair (must finish unpaired and empty)
- Media commit stall then Unpair (must abort, no leftover files)
- Unwritable data dir: local cleanup error in status, StartPairing refused
- Ephemeral event flags not persisted
- Cross-chat media keys
- Deterministic 50/100 store bounds

Added in final fix pass (agy 2026-09-14):
- QR timeout/error → StateUnpaired (retryable): `TestQRTimeoutReturnsRetryableUnpaired`, `TestQRGenericErrorReturnsRetryableUnpaired`
- StartPairing rejects if already paired: `TestStartPairingRejectsWhenAlreadyPaired`
- QR success followed by Connected doesn't regress: `TestQRSuccessFollowedByConnected`
- commitMessage/handleReceipt/ingestHistorySync emit-under-lock race: `TestCommitMessageEmitAfterUnpairRace`, `TestReceiptEmitAfterUnpairRace`, `TestHistorySyncEmitAfterUnpairRace`

Added in lead-acceptance follow-up pass (agy 2026-09-14):
- All StartPairing early failure paths (initClientAndStore, GetQRChannel, Connect, closed channel, unexpected first event, context cancel, timeout) now publish retryable UNPAIRED status with actionable error, cancel/disconnect incomplete attempt safely, and retire the generation so stale QR goroutines from the failed attempt cannot overwrite subsequent retry state.
- QR success case guarded against regressing StateConnected to StateConnecting when Connected arrives before success is processed.
- AGENTS.md stale attribution bullet corrected: removed AI co-author/contributor/trailer language per current user instruction.
- New tests: `TestStartPairingGetQRChannelFailurePublishesUnpaired`, `TestStartPairingConnectFailurePublishesUnpaired`, `TestRetryAfterFailureGetsFreshGeneration`, `TestConnectedBeforeQRSuccessDoesNotRegress`

Ran in this worktree after lead-acceptance follow-up (agy, 2026-09-14):
- `go test -race -mod=vendor -count=1 ./...` PASS (all packages)
- `go vet -mod=vendor ./...` PASS
- `make test-ui` PASS (16 JS, QML shell, pagination, isolated helper build+RPC)
- `make validate` PASS
- `git diff --check` PASS

Codex should still re-run these on the review pass.

---

## 4. Material blockers for live use (not code)

- Live WhatsApp QR pairing and a real send/receive pass belong to Codex after review. This worktree must not pair or send.
- Helper build needs Go plus a C compiler. The plugin does not install them.
- On-demand phone history is not requested yet.
- WhatsApp voice, GIF search, reactions, and calling are disabled with copy.
- View-once content is not kept, by design.

---

## 5. Remaining live pairing steps (Codex)

1. Review the worktree diff. Do not copy it to the installed plugin until review passes.
2. Build with `make helper` in this worktree (`CGO_ENABLED=1`).
3. Point a throwaway runtime/data dir at the built helper, or install only after review.
4. Pair Google still works (existing session). Pair WhatsApp with an explicit QR from the WhatsApp tab.
5. Confirm independent unpair, drafts, send, image send/receive, restart restores cached chats and regular image download, view-once is not reopened.
6. Keep version 0.1.2 until a real release.

---

## 6. Lead review map

| Review item | State |
|---|---|
| 1. Start uses `deviceStore.ID != nil` | Done |
| 2. Publish outside append/history locks | Done |
| 3. Unpair generation + logout-before-disconnect | Done this pass |
| 4. Persist chats and media metadata | Done this pass |
| 5. Opaque media paths, bounds, stale download | Done |
| 6. Cursor timestamp+ID | Done |
| 7. Honest history limitation | Done this pass |
| 8. libgm CookiesLock patch | Present in vendor |
| 9. sqlite 0600 + CGO docs | Done this pass |
| 10. ReplyToID / group receipts | Done |
| 11. Unknown network error; QML isolation | Done this pass |
| 12. Independent Daemon.Start | Done |
| 13. Control messages / view-once | Done this pass |

## Lead integration checkpoint

- Independently verified the final Go race suite, vet, manifest validation and whitespace checks. Earlier rendered UI suite also passed; agy repeated it after QR changes.
- Lead regression proved a retry handler still captured a retired generation. Rebinding the client handler after failure now accepts the retry connection; strengthened existing retry test passes.
- UI drafts and isolation blocker resolved: Google drafts are preserved while visiting unpaired WhatsApp, only the affected account state is cleared on unpair/re-pair, drafts are properly namespaced by network, and QML tests were added to cover UI isolation (agy 2026-09-14).
- Feature remains isolated, unpublished, and not installed. Original Google plugin remains connected. Rollback plugin archive saved outside the repository.
