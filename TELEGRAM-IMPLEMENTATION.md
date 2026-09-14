# Telegram Implementation Plan and Checkpoint

## Status: Milestone 1 Complete (2026-09-14)
- Worktree: `/home/onelegdave/Projects/omachat`
- Branch: `feature/telegram`
- Human owner / maintainer: OneLegDave
- Codex owns planning, implementation decisions, integration, and final verification

---

## 1. Architecture Decisions

### Protocol Client and Library Selection
- OmaChat is a native Omarchy desktop shell plugin designed to run with vendored dependencies (`go build -mod=vendor`). It does not download modules at runtime and does not ship binary ELFs in git.
- Telegram uses the MTProto protocol. In the Go ecosystem, the primary client options are:
  1. `github.com/gotd/td`: Pure Go MTProto client implementation. It avoids external shared libraries, C compiler requirements for the protocol core, and integrates natively with Go standard context and concurrency.
  2. TDLib (`github.com/zelenin/go-tdlib` or similar CGO bindings): The official Telegram Database Library written in C++. It requires building and shipping heavy C++ shared libraries (`libtdjson.so`), significantly complicating distribution and packaging on Omarchy systems.
  3. Telegram Bot API: Not applicable, as OmaChat is a native companion client for user accounts, not an automated bot.
- Decision for Milestone 1:
  - Do not invent an unreviewed or unvendored protocol client.
  - No MTProto library is currently vendored in `vendor/`.
  - Telegram user authentication requires official API credentials (`api_id` and `api_hash` registered at my.telegram.org) and a user authentication UX decision (MTProto QR code login versus phone number plus SMS or in-app verification code).
  - This safe first milestone establishes the network routing abstraction, wire protocol constants, honest backend service state, and local storage isolation without requiring unreviewed credentials or network downloads.

### Wire Protocol and Routing
- Network constant: `wire.NetworkTelegram = "telegram"` added alongside `NetworkGMessages` and `NetworkWhatsApp`.
- Wire routing: `wire.Request.Network` supports `"telegram"`, `"whatsapp"`, and omitted/`"gmessages"`. Unknown networks (such as `"signal"` or arbitrary strings) return an explicit `"unknown network: <name>"` error.
- Daemon routing abstraction: In `internal/daemon/server.go`, request dispatching cleanly routes by network to `dispatchGMessages`, `dispatchWhatsApp`, or `dispatchTelegram`.
- Connection status push: Fresh plugin connections over the daemon socket receive initial status pushes for all three networks immediately upon connection.

### Storage Isolation
- Google Messages uses `~/.local/share/omachat/session.json` and `~/.cache/omachat/media/`.
- WhatsApp uses `~/.local/share/omachat/whatsapp.db`, `~/.local/share/omachat/whatsapp_store.json`, and `~/.cache/omachat/media_whatsapp/`.
- Telegram uses `~/.local/share/omachat/telegram.session`, `~/.local/share/omachat/telegram_store.json`, and `~/.cache/omachat/media_telegram/`.
- Unpairing any network strictly wipes only that network's credentials and cached media, leaving the other two networks intact.

### UI Integration and Coming Soon Path
- In `Panel.qml`, the header service tabs include Telegram (`value: "telegram"`, label: `"Telegram"`, icon: `"\uf2c6"`).
- The brand glyph switches to `"\uf2c6"` when the Telegram tab is active.
- `Panel.qml` preserves `serviceLive: activeService === "gmessages" || activeService === "whatsapp"`, so navigating to the Telegram tab safely displays `ComingSoon.qml` without attempting chat RPCs.
- `Service.qml` maintains `statusTG` and `conversationsTG`, queries initial status on socket connect, and processes Telegram status and conversation events.

---

## 2. Completed in Milestone 1

1. **Wire Protocol (`internal/wire/wire.go`)**:
   - Added `NetworkTelegram = "telegram"`.
   - Added `KnownNetworks` slice and `IsKnownNetwork(net string)` validator.

2. **Storage Paths and Isolation (`internal/store/store.go`, `internal/store/store_test.go`)**:
   - Added `TelegramMediaDir()`, `TelegramSessionFile()`, `TelegramStoreFile()`, and `ClearTelegramSession()`.
   - Updated `NewPaths()` to create `TelegramMediaDir()` with `0700` permissions.
   - Expanded `TestStorageIsolation` to verify three-way storage isolation across Google, WhatsApp, and Telegram.

3. **Telegram Backend Scaffold (`internal/telegram/backend.go`, `internal/telegram/backend_test.go`)**:
   - Implemented `Backend` struct with concurrency-safe state and lifecycle.
   - Initial state reports `wire.Status` with `Network: "telegram"`, `State: "unpaired"`, `PhoneOK: true`, and descriptive hint.
   - Methods requiring a live MTProto client (`Send`, `SendMedia`, `Media`, `MarkRead`, `StartPairing`) return `ErrNotConfigured`.
   - `Unpair` cleanly wipes local Telegram storage and resets in-memory state.
   - Comprehensive test suite covering initial state, start with and without existing session, unconfigured rejections, conversation caching, and unpair cleanup.

4. **Daemon Routing and Server Integration (`internal/daemon/`)**:
   - Integrated `d.tg` into `Daemon` lifecycle (`New`, `Start`, `Telegram()`, `SetTelegram()`).
   - Initial status push for Telegram sent to new client connections in `server.go`.
   - Implemented `dispatchTelegram` handling status, conversations, messages, unpair, refresh, image picking, capture discard, UI scale, and config.
   - Explicitly rejects unsupported methods (`gaiaPairing`, `pairFromBrowser`, `listProfiles`, `setProfile`, `react`, `setTyping`, `gifSearch`).
   - Added `TestTelegramRouting`, updated `TestUnknownNetworkRejection`, and added `TestThreeWayStorageIsolation` in `routing_test.go`.

5. **UI Layer (`Service.qml`, `Panel.qml`)**:
   - `Service.qml`: tracks `statusTG` and `conversationsTG`, requests status for Telegram upon connection, and parses incoming Telegram events.
   - `Panel.qml`: updated brand glyph, unpair advice, and multi-network readiness check while preserving the `ComingSoon.qml` path.

6. **Documentation (`README.md`, `TELEGRAM-IMPLEMENTATION.md`)**:
   - Updated file locations table and limits section in `README.md`.
   - Created this implementation guide and integration roadmap.

---

## 3. Verification and Test Results

The following test suites were executed and passed cleanly:
- `go test -mod=vendor ./...`: PASS across all packages (`cmd/omachatd`, `internal/browser`, `internal/daemon`, `internal/store`, `internal/telegram`, `internal/whatsapp`, `internal/wire`).
- `go test -race -mod=vendor ./internal/telegram/... ./internal/store/... ./internal/daemon/...`: PASS (race detector clean).
- `go vet -mod=vendor ./...`: PASS.
- `make test-ui`: PASS (16 JS model tests via node, QML shell tests, pagination tests, build helper test).
- `omarchy plugin validate .`: PASS.

---

## 4. What Remains Before Live Telegram Pairing

To advance from this scaffold to live Telegram pairing, the following steps are required:

1. **Library Selection and Review**:
   - Select MTProto library: `gotd/td` is recommended to maintain pure Go builds without C++ shared library compilation requirements.
   - Review licensing and dependency footprint.
2. **Vendoring Dependencies**:
   - Run `go get` and `go mod vendor` in a separate authorized step to populate `vendor/` and update `go.mod` / `go.sum`.
   - Ensure the build remains fully functional offline with `go build -mod=vendor`.
3. **API Credentials Configuration**:
   - Acquire or allow configuration of Telegram `api_id` and `api_hash`.
   - Add secure credential storage options in `~/.local/share/omachat/config.json`.
4. **Authentication and Pairing UX**:
   - Implement MTProto QR code login flow or phone number login with verification code input in QML.
   - Create pairing UI components in QML or adapt `PairingView.qml`.
5. **Session and Entity Persistence**:
   - Implement session storage saving MTProto session data to `telegram.session` (mode 0600).
   - Implement companion entity cache (dialogs, users, chats) in `telegram_store.json`.
6. **Live Message Sync and Sending**:
   - Connect MTProto update dispatcher to `d.PublishEvent`.
   - Implement text send, media upload/download, and pagination.
7. **Activate UI View**:
   - Update `Panel.qml` `serviceLive` property to include `"telegram"` once pairing and sync handlers are operational.
