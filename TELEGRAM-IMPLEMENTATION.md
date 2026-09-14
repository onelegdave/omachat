# Telegram Implementation Plan and Readiness Review

## Status: Milestone 1, Milestone 2, & Milestone 3 (Credential Configuration) Complete (2026-09-14)
- **Worktree**: `/home/onelegdave/Projects/omachat`
- **Branch**: `feature/telegram`
- **Human Owner / Maintainer**: OneLegDave
- **MTProto Dependency**: `github.com/gotd/td v0.161.0`
- **Review status**: Dependency vendoring, credential configuration, and offline validation complete. Live MTProto pairing and network integration remain subject to human provisioning and review.

---

## 1. Executive Summary & Concrete Recommendation

### Recommendation: Adopt `github.com/gotd/td` (Pure Go MTProto 2.0 Client)
For a real user-account MTProto integration in OmaChat, **`github.com/gotd/td` is the clear and concrete recommendation**.

**TDLib (`tdlib/td` with Go CGO bindings like `github.com/zelenin/go-tdlib`) is rejected** because it violates core OmaChat project constraints:
1. **No Binary ELFs or External C++ Build Toolchains**: TDLib is written in C++ and compiles to a heavy shared library (`libtdjson.so` ~50–100 MB). OmaChat is a native Omarchy desktop shell plugin designed to build reproducibly and offline from source via `go build -mod=vendor ./...`. Vendoring a C++ shared library or requiring users to build TDLib via CMake/g++ from source is incompatible with OmaChat's distribution model.
2. **Pure Go Alignment**: `gotd/td` is a pure Go MTProto 2.0 implementation licensed under the MIT License. It can be 100% vendored inside `vendor/`, builds cleanly with standard Go toolchains (Go 1.25–1.27+), and avoids CGO FFI overhead and JSON serialization boundaries.
3. **Native Fit for Existing Pairing and Storage Architecture**: `gotd/td` provides first-class QR login support (`github.com/gotd/td/telegram/auth/qrlogin`) generating `tg://login?token=...` URLs that map directly to OmaChat's existing `PairingView.qml` QR workflow. Session persistence maps directly to OmaChat's `paths.TelegramSessionFile()` (`~/.local/share/omachat/telegram.session`).

---

## 2. Authoritative Library Evaluation: `gotd/td` vs. TDLib

### Authoritative Metadata & Primary Sources
- **`gotd/td`**:
  - Repository: [`https://github.com/gotd/td`](https://github.com/gotd/td)
  - Documentation: [`https://gotd.dev`](https://gotd.dev)
  - License: MIT License
  - Language: Pure Go (`go 1.25.0` base in upstream `go.mod`)
  - Description: Full native MTProto 2.0 client implementation in Go, with code-generated types from official Telegram TL schemas, connection pooling, automatic DC migration, and modular authentication flows.
- **TDLib (`tdlib/td`) + `zelenin/go-tdlib`**:
  - Repository (TDLib core): [`https://github.com/tdlib/td`](https://github.com/tdlib/td)
  - Repository (Go wrapper): [`https://github.com/zelenin/go-tdlib`](https://github.com/zelenin/go-tdlib)
  - License: Boost Software License 1.0 (TDLib core); MIT License (`go-tdlib`)
  - Language: C++ (TDLib core); Go with CGO (`go-tdlib`)
  - Description: The official cross-platform Telegram client library developed by Telegram. Go wrappers interact with `libtdjson.so` via CGO and JSON string exchange.
- **Official Telegram API Documentation**:
  - MTProto Protocol: [`https://core.telegram.org/api`](https://core.telegram.org/api)
  - QR Code Login API: [`https://core.telegram.org/api/qr-login`](https://core.telegram.org/api/qr-login)
  - Telegram API Terms of Service: [`https://core.telegram.org/api/terms`](https://core.telegram.org/api/terms)

### Side-by-Side Comparison Matrix

| Evaluation Dimension | `github.com/gotd/td` | TDLib (`tdlib/td` + `zelenin/go-tdlib`) | OmaChat Impact & Decision |
| :--- | :--- | :--- | :--- |
| **Language & Architecture** | 100% Pure Go | C++20 core engine + Go CGO FFI wrapper | `gotd/td` is native Go; zero CGO overhead for MTProto calls. |
| **Vendoring & Build (`go build -mod=vendor`)** | **Fully vendorable** in `vendor/`; standard Go source code only | **Cannot be vendored** in `vendor/`; requires external `libtdjson.so` | **Hard blocker for TDLib**. OmaChat requires offline vendored builds without runtime downloads or checked-in ELF blobs. |
| **External Build Dependencies** | Standard Go compiler (`go 1.27`) | CMake, C++20 compiler (g++/clang), OpenSSL, zlib, gperf | TDLib adds heavy multi-gigabyte build requirements and 20+ min compile times. |
| **Binary Footprint** | Incremental addition to single `omachatd` ELF (~8–12 MB compiled) | External `libtdjson.so` (~50–80 MB stripped) | `gotd/td` keeps OmaChat compact and single-binary. |
| **Packaging on Omarchy / Linux** | Trivial: builds as part of `make helper` (`bin/omachatd`) | Complex: requires packaging custom system shared libraries on Arch/Omarchy | `gotd/td` eliminates packaging friction for end users. |
| **Licensing** | MIT License (Permissive) | Boost Software License 1.0 (Core) + MIT (Wrapper) | Both are permissively licensed, but `gotd/td` avoids dynamic linking liabilities. |
| **QR Code Authentication** | Native high-level helper `auth/qrlogin` (`tg://login?token=...`) | TDLib JSON event `updateAuthorizationStateWaitOtherDeviceConfirmation` | Both support MTProto QR login; `gotd/td` provides direct URL string generation. |
| **2FA / Cloud Password** | Native SRP calculation via `client.Auth().Password()` | TDLib `checkAuthenticationPassword` | Both support SRP without transmitting plaintext passwords. |
| **Updates & Gap Handling** | Modular: `tg.UpdateDispatcher` + `updates.Manager` | Built into TDLib internal SQLite engine | TDLib is more turnkey, but `gotd/td` gives OmaChat full control over entity caching. |
| **Active Ecosystem & Maintenance** | Actively maintained Go library; widely used for bots and user clients | TDLib is officially maintained; Go wrappers lag behind TDLib releases | `gotd/td` is maintained directly in Go with typed schemas. |

---

## 3. Go 1.27 and Vendor Constraints

### Current OmaChat Environment
- **Go Version**: `go 1.27.0` (`go version go1.27.0-X:nodwarf5 linux/amd64` installed on host).
- **Module Mode**: Mandatory vendored build (`go build -mod=vendor`). The project rule strictly prohibits unreviewed runtime module downloads (`GOPROXY="off"`) and rejects committing binary ELF files to git.
- **CGO Posture**: `Makefile` currently permits `CGO_ENABLED=1` solely for `github.com/mattn/go-sqlite3` (used by WhatsApp session store), requiring a standard host C compiler (gcc/clang). Adding a massive C++ dependency like TDLib would violate the architecture boundary.

### `gotd/td` Dependency & Module Analysis
- `gotd/td` specifies `go 1.25.0` in its `go.mod`, making it fully compatible with Go 1.27.
- **Shared Dependencies Already Vendored in OmaChat**:
  - `github.com/coder/websocket v1.8.15` (already in `vendor/`)
  - `github.com/google/uuid v1.6.0` (already in `vendor/`)
  - `golang.org/x/crypto v0.56.0` (already in `vendor/`)
  - `golang.org/x/net v0.58.0` (already in `vendor/`)
  - `golang.org/x/sync v0.22.0` (already in `vendor/`)
  - `golang.org/x/sys v0.47.0` (already in `vendor/`)
  - `golang.org/x/text v0.41.0` (already in `vendor/`)
- **New Transitive Dependencies Required by `gotd/td`**:
  - `github.com/go-faster/errors`, `github.com/go-faster/jx`, `github.com/go-faster/xor` (Apache-2.0)
  - `github.com/cenkalti/backoff/v4` (MIT)
  - `rsc.io/qr` (BSD-3-Clause)
  - `go.uber.org/zap`, `go.uber.org/atomic`, `go.uber.org/multierr` (MIT)
  - `github.com/gotd/ige`, `github.com/gotd/neo` (MIT)
- **Constraint Verdict**: All new dependencies are 100% pure Go, permissively licensed, and vendor cleanly via standard `go mod vendor` without patch hacks or C++ headers.

---

## 4. Licensing and Dependency Implications

### Licensing Audit
1. **OmaChat**: MIT License.
2. **`gotd/td`**: MIT License.
3. **Transitive Dependencies of `gotd/td`**:
   - MIT: `backoff/v4`, `zap`, `atomic`, `multierr`, `gotd/ige`, `gotd/neo`.
   - Apache-2.0: `go-faster/jx`, `go-faster/xor`, `go-faster/errors`, `coder/websocket`.
   - BSD-3-Clause: `google/uuid`, `rsc.io/qr`, `golang.org/x/*`.
4. **Copyleft & Restrictive Licenses**: **Zero**. There are no GPL, AGPL, LGPL, or SSPL dependencies in the core `gotd/td` tree.
5. **Attribution Requirements**: Standard permissive license notices will be preserved in `vendor/modules.txt` and project notices.

---

## 5. Existing Session, Storage, and Pairing Architecture Integration

OmaChat’s storage and pairing subsystem was designed in Milestone 1 to provide strict three-way isolation between Google Messages, WhatsApp, and Telegram.

### Storage Layout Integration
| Network | Credentials / Session | Conversation / Message Cache | Media Cache |
| :--- | :--- | :--- | :--- |
| **Google Messages** | `~/.local/share/omachat/session.json` | In-memory / libgm internal | `~/.cache/omachat/media/` |
| **WhatsApp** | `~/.local/share/omachat/whatsapp.db` (SQLite) | `~/.local/share/omachat/whatsapp_store.json` | `~/.cache/omachat/media_whatsapp/` |
| **Telegram** | `~/.local/share/omachat/telegram.session` | `~/.local/share/omachat/telegram_store.json` | `~/.cache/omachat/media_telegram/` |

### Session & Entity Persistence with `gotd/td`
1. **Session Storage (`telegram.session`)**:
   - `gotd/td` defines the `session.Storage` interface:
     ```go
     type Storage interface {
         LoadSession(ctx context.Context) ([]byte, error)
         StoreSession(ctx context.Context, data []byte) error
     }
     ```
   - OmaChat will implement a custom `session.Storage` backed by `paths.TelegramSessionFile()` using OmaChat's existing `store.WritePrivateFile` helper. This guarantees:
     - File creation with strict `0600` permissions.
     - Atomic write via temporary file rename (`.omachat-*.tmp` -> `telegram.session`).
     - Session deletion on `Unpair` via `paths.ClearTelegramSession()`.
2. **Entity & Dialog Cache (`telegram_store.json`)**:
   - Telegram MTProto references users, chats, and channels by ID and `access_hash`.
   - Inbound updates and dialog lists will be cached in `paths.TelegramStoreFile()` using OmaChat's proven `store.WritePrivateJSON` pattern (matching `whatsapp_store.json`), storing:
     - Ordered conversation list (`wire.Conversation`).
     - Cached message bubbles (`wire.Message`).
     - Peer entity table (`id` -> `access_hash`, username, phone, display title).
   - This avoids pulling in heavy key-value stores (such as PebbleDB or BoltDB) and keeps the disk format human-inspectable and cleanly wipeable.
3. **Media Cache (`media_telegram/`)**:
   - Incoming photo attachments, voice notes, stickers, and profile avatars will download on demand into `paths.TelegramMediaDir()` using opaque SHA-256 filenames (`mediaKeyToOpaque`), preventing path traversal or filename collision.

---

## 6. API Credentials and Telegram Terms of Service Compliance

### Official Telegram API Credential Requirements
Telegram requires all MTProto client applications to identify themselves using two credentials obtained from [`https://my.telegram.org`](https://my.telegram.org):
- `api_id` (integer, e.g., `1234567`)
- `api_hash` (32-character hexadecimal string)

### Anti-Abuse and Terms of Service Rules
1. **Never Hardcode Credentials in Source Code**:
   - Telegram’s terms treat `api_id` and `api_hash` as belonging to the developer who created the app. Hardcoding credentials in an open-source repository risks immediate compromise, token scraping, automated abuse, and `API_ID_PUBLISHED_FLOOD` ban waves.
   - If third parties use a published API ID for spam, **Telegram permanently bans the owner’s personal Telegram account and revokes the API credentials**.
2. **No AI/ML Model Training**:
   - Telegram's Terms of Service explicitly prohibit scraping or using Telegram user data to train machine learning models. OmaChat operates strictly as a local display and desktop shell plugin; no chat data is ever exported or used for model training.
3. **Companion Client Integrity**:
   - Telegram permits alternative third-party companion clients under the API Terms of Service, provided that official branding guidelines are respected, security measures are maintained, and MTProto security proofs (e.g. SRP) are computed properly.

### Credential Configuration Strategy for OmaChat
OmaChat supports two non-intrusive, secure credential sources:
1. **Config File (Primary)**:
   - File: `~/.local/share/omachat/config.json` (mode `0600`).
   - JSON keys: `telegramApiID` (integer) and `telegramApiHash` (32-character hexadecimal string). Backward-compatible alias support for `telegram_api_id` and `telegram_api_hash` is also included.
     ```json
     {
       "telegramApiID": 1234567,
       "telegramApiHash": "0123456789abcdef0123456789abcdef"
     }
     ```
2. **Environment Variables (Secondary / Development Override)**:
   - `OMACHAT_TELEGRAM_API_ID`
   - `OMACHAT_TELEGRAM_API_HASH`
3. **Precedence and Validation**:
   - Configuration file values take precedence over environment variables.
   - Validation requires a positive integer `api_id` and a non-empty 32-character hexadecimal `api_hash`.
   - Security invariant: credential secrets and raw values are never logged or exposed in error messages.
4. **Unconfigured Behavior**:
   - If neither source provides valid credentials, `internal/telegram/backend.go` remains in `wire.StateUnpaired` and provides an honest, clear hint in `wire.Status.Hint`:
      `"Telegram API credentials required: configure api_id and api_hash in ~/.local/share/omachat/config.json (obtain from my.telegram.org)"`
   - If credentials are valid, the backend reports `wire.StateUnpaired` with hint `"Telegram API credentials configured; pairing not yet started"`.
   - No crashes, no unhandled panics, and no attempt to initiate network connections without valid credentials.

---

## 7. Authentication UX Requirements

### Comparison of User-Account Authentication Flows

| Flow | Mechanism | User Experience | Recommendation |
| :--- | :--- | :--- | :--- |
| **Option A: QR Code Login** (`auth.exportLoginToken`) | Client requests login token; Telegram returns token URL `tg://login?token=...`. | **Seamless**: OmaChat renders QR code; user opens Telegram mobile app -> Settings -> Devices -> Link Desktop Device -> scans QR code. | **Strongly Recommended (Primary)** |
| **Option B: Phone + In-App / SMS Code** (`auth.sendCode` / `auth.signIn`) | User inputs phone number; Telegram sends verification code via Telegram app or SMS; user inputs code. | **Friction**: Requires multi-step text inputs in QML. Telegram heavily restricts or charges for SMS verification on 3rd-party apps. | Secondary / Future Enhancement |

### Selected Authentication Workflow: QR Code Login
The QR code flow is identical to the WhatsApp Web pairing experience already loved by OmaChat users:
1. **Start Pairing**:
   - User clicks `"Use a QR code"` in OmaChat's Telegram view.
   - Frontend issues daemon RPC: `call("startPairing", null, null, "telegram")`.
2. **Token Generation & Display**:
   - `gotd/td/telegram/auth/qrlogin` calls `AuthExportLoginToken`.
   - Backend populates `wire.Status.QRURL` with `tg://login?token=<base64url>` and sets `wire.Status.State = wire.StatePairing`.
   - Status event is pushed across the daemon socket to `Service.qml`.
   - `PairingView.qml` executes `qrencode` to render `~/.cache/omachat/pairing-qr.png` and displays the QR code.
3. **Token Refresh Loop**:
   - Telegram QR login tokens expire every ~30 seconds.
   - The backend pairing loop handles token expiration by exporting refreshed tokens and updating `wire.Status.QRURL` until acceptance or cancellation.
4. **Acceptance & Session Finalization**:
   - When the user scans the code on their phone, Telegram's DC emits `tg.UpdateLoginToken`.
   - The backend calls `AuthImportLoginToken`, completing MTProto authentication.
   - The session key is persisted to `~/.local/share/omachat/telegram.session` with `0600` permissions.
   - Backend transitions to `wire.StateConnected` and notifies `Service.qml`.
5. **Two-Factor Authentication (2FA) / Cloud Password Handling**:
   - If the user account has 2FA enabled, MTProto returns `SESSION_PASSWORD_NEEDED`.
   - In this state, OmaChat will transition to a dedicated sub-state with an honest prompt (e.g., `"Telegram 2FA Cloud Password required"`), allowing the user to provide the password via a secure RPC call (`telegram2FAPassword`) before finalizing the session using `gotd` SRP proof calculation (`client.Auth().Password()`).

---

## 8. Human Approval Gates (Pre-requisites Before Credentials or Live Network)

The following routine development actions are authorized within scope, but **the following specific milestones require explicit OneLegDave human review and approval**:

1. **Dependency Addition & Vendoring**:
   - Adding `github.com/gotd/td` and its dependencies to `go.mod` and populating `vendor/`.
   - Human check: Verify licensing and vendor tree size before merging.
2. **API Credential Registration & Provisioning**:
   - Registering an application on `https://my.telegram.org` using OneLegDave's Telegram account to obtain a real `api_id` and `api_hash`.
   - Placing the credentials into `~/.local/share/omachat/config.json`.
   - *Note*: AI agents cannot autonomously register credentials on `my.telegram.org` due to SMS/in-app verification and CAPTCHA requirements.
3. **Live Network Integration Authorization**:
   - Authorizing the first live outbound MTProto network connection from `bin/omachatd` to Telegram DC production servers (`149.154.167.50:443`, etc.).

---

## 9. Staged Next Implementation Plan

### Stage 1: Dependency Vendoring and Build Baseline (COMPLETED - 2026-09-14)
- Added pure Go MTProto 2.0 client `github.com/gotd/td v0.161.0` (compatible with Go 1.25–1.27+).
- Updated `go.mod` and `go.sum` and vendored all dependencies into `vendor/`.
- Preserved local `libgm` tachyon token synchronization patch in `vendor/go.mau.fi/mautrix-gmessages/pkg/libgm/client.go`.
- Implemented atomic `FileSessionStorage` (`internal/telegram/session.go`) satisfying `session.Storage` with `0600` file permissions via OmaChat's `store.WritePrivateFile`.
- Wired `*telegram.Client` field and `Client()` accessor in `internal/telegram/backend.go`, resetting on `Unpair`.
- Added focused build and dependency test suite (`internal/telegram/dependency_test.go`) validating offline client initialization, session file storage, and QR login token URL round-tripping.
- Verified offline compilation and tests pass cleanly:
  - `go test -mod=vendor ./...`: PASS
  - `go vet -mod=vendor ./...`: PASS
  - `GOPROXY=off go build -mod=vendor ./cmd/omachatd`: PASS
  - `make helper` (`CGO_ENABLED=1 GOPROXY=off go build -mod=vendor ...`): PASS
  - `make test-ui`: PASS
  - `make validate` (`omarchy plugin validate .`): PASS

### Stage 2: Configuration & Credential Management (COMPLETED - 2026-09-14)
- Added `TelegramAPIID` (integer) and `TelegramAPIHash` (string) to `store.Config` in `internal/store/config.go`.
- Supported JSON keys `telegramApiID` and `telegramApiHash` with backward/forward-compatible unmarshaling for `telegram_api_id` and `telegram_api_hash`.
- Integrated atomic persistence via existing `writePrivateJSON` (`SetTelegramCredentials`, `SetTelegramAPIID`, `SetTelegramAPIHash`).
- Added environment variable fallback `OMACHAT_TELEGRAM_API_ID` and `OMACHAT_TELEGRAM_API_HASH` for development, with config taking precedence.
- Validated positive `api_id` and non-empty 32-character hexadecimal `api_hash` with strict protection against logging or exposing secrets in errors.
- Implemented `TelegramCredentials` type and `ResolveTelegramCredentials` helper returning validated credentials or an explicit `ErrTelegramUnconfigured`.
- Wired `internal/telegram/backend.go` to inspect credentials at `Start`, reporting an honest unpaired hint without initiating network calls, creating live client connections, or starting QR pairing.
- Preserved existing Google Messages and WhatsApp behaviors and isolation.
- Added focused unit test suite (`internal/store/telegram_config_test.go` and `internal/telegram/backend_test.go`) covering config round-trip, precedence, malformed values, secret leak prevention, and unconfigured status hints.

### Stage 3: MTProto Client Lifecycle and QR Pairing Engine
- Implement `telegram.Client` initialization in `internal/telegram/backend.go`:
  - Hook custom `session.Storage` backed by `paths.TelegramSessionFile()`.
  - Implement `StartPairing(ctx)` using `gotd/td/telegram/auth/qrlogin`:
    - Export login token URL `tg://login?token=...`.
    - Set `wire.Status.QRURL` and push status updates.
    - Handle token refresh timer and context cancellation.
    - Handle `SESSION_PASSWORD_NEEDED` (2FA error handling).
- Implement `Unpair(ctx)`:
  - Terminate client connection.
  - Delete `telegram.session`, `telegram_store.json`, and `media_telegram/` files via `paths.ClearTelegramSession()`.
  - Reset in-memory state and publish `wire.StateUnpaired`.
- Unit tests with mock MTProto handlers.

### Stage 4: Dialogs, Message Synchronization, and Entity Caching
- Connect `tg.UpdateDispatcher` to receive incoming message events (`UpdateNewMessage`, `UpdateEditMessage`, `UpdateReadHistoryInbox`).
- Implement initial sync on connect:
  - Query dialog list via `messages.GetDialogs`.
  - Map Telegram users and chats into `wire.Conversation` structs.
  - Persist dialogs and entity mapping to `telegram_store.json`.
- Implement `Conversations(count int)` and `Messages(ctx, p)` reading from cache.
- Implement `MarkRead(ctx, p)` to send `messages.ReadHistory` RPC.

### Stage 5: Outbound Messaging and Media Handling
- Implement `Send(ctx, p)` using `sender.Send()` with peer entity resolution.
- Implement `SendMedia(ctx, p)` for outbound image attachments.
- Implement inbound media downloading:
  - Download photo attachments on demand into `paths.TelegramMediaDir()`.
  - Implement thumbnail resolution.

### Stage 6: UI Activation and Verification
- Update `PairingView.qml` hero text and glyphs for Telegram (`root.network === "telegram"`).
- Update `Panel.qml`:
  - Add `"telegram"` to `serviceLive` once pairing and sync are operational.
  - Retire `ComingSoon.qml` for Telegram in favor of live `InboxView.qml`.
- Execute full verification suite:
  - `go test -race -mod=vendor ./...`
  - `make test-ui`
  - `omarchy plugin validate .`

---

## 10. Verification and Checkpoint Status

### Milestone 2 Verification Results (2026-09-14)
- **Dependency Added**: `github.com/gotd/td v0.161.0` (pure Go MTProto 2.0 client; direct requirement in `go.mod`).
- **Vendoring**: Full offline vendoring in `vendor/` with `vendor/modules.txt` updated.
- **Offline Build Check (`GOPROXY=off`)**:
  - `GOPROXY=off go build -mod=vendor ./cmd/omachatd`: PASS.
  - `GOPROXY=off make helper`: PASS (`bin/omachatd` compiled with `CGO_ENABLED=1`).
- **Unit & Dependency Tests**:
  - `go test -mod=vendor ./...`: PASS across all packages (`cmd/omachatd`, `internal/daemon`, `internal/store`, `internal/telegram`, `internal/whatsapp`, `internal/wire`).
  - Focused tests in `internal/telegram/dependency_test.go` (`TestFileSessionStorage`, `TestMTProtoClientInitialization`, `TestQRLoginTokenParsing`, `TestTGTypesInstantiable`): PASS.
- **Static Analysis**:
  - `go vet -mod=vendor ./...`: PASS.
- **UI & Integration Suite**:
  - `make test-ui`: PASS (16 Node.js model tests, QuickShell QML tests, pagination suite, and isolated unpaired daemon build check).
- **Plugin Validation**:
  - `omarchy plugin validate .` (`make validate`): PASS.
- **Licensing Audit**:
  - All new transitive dependencies audited: 100% permissively licensed (MIT, Apache-2.0, BSD-3-Clause). Zero GPL/AGPL/LGPL/MPL in new modules. Project `NOTICE` updated.
- **Safety & Isolation Invariants**:
  - Zero Telegram API credentials added.
  - Zero live network connections initiated (no DC connections, client unstarted).
  - Existing `ComingSoon.qml` path preserved; Telegram UI remains inactive until Milestone 6.
  - Existing Google Messages and WhatsApp isolation and tests fully intact.
- **Authorship Policy**: Human authorship policy strictly maintained; no AI author/co-author trailers added.

### Milestone 3 Verification Results (2026-09-14)
- **Credential Fields Added**: `TelegramAPIID` (int) and `TelegramAPIHash` (string) added to `store.Config` and `store.ConfigStore`.
- **JSON Serialization**: Documented JSON tags `telegramApiID` and `telegramApiHash` with alias unmarshaling for `telegram_api_id` and `telegram_api_hash`.
- **Atomic Persistence**: Persisted using existing `writePrivateJSON` 0600 tempfile rename pattern (`SetTelegramCredentials`, `SetTelegramAPIID`, `SetTelegramAPIHash`).
- **Environment Fallback**: `OMACHAT_TELEGRAM_API_ID` and `OMACHAT_TELEGRAM_API_HASH` supported with config precedence.
- **Validation**: Strict validation for positive integer `api_id` and 32-character hex `api_hash`, preventing secret exposure in errors.
- **Unconfigured Error & Status**: Explicit `ErrTelegramUnconfigured` sentinel returned; `internal/telegram/backend.go` reports honest unpaired hint `"Telegram API credentials required: configure api_id and api_hash in ~/.local/share/omachat/config.json (obtain from my.telegram.org)"`.
- **Safety Invariants**:
  - Zero live MTProto client connections initiated.
  - Zero QR pairing started.
  - Zero network calls made.
  - No secret values exposed in log output or errors.
  - Google Messages and WhatsApp behavior and isolation fully intact.
- **Tests & Static Analysis**:
  - `go test -mod=vendor ./...`: PASS across all packages.
  - `go vet -mod=vendor ./...`: PASS.
  - Focused test suite (`internal/store/telegram_config_test.go`, `internal/telegram/backend_test.go`): PASS.
- **Authorship Policy**: Human authorship policy strictly maintained; no AI author/co-author trailers added.
