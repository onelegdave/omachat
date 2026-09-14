# OmaChat v0.2.0

OmaChat v0.2.0 adds native WhatsApp support alongside Google Messages, featuring
dual-network isolation, QR pairing, inbound stickers, and persisted media.

### Highlights

- **Dual-network isolation:** Switch between Google Messages and WhatsApp using
  panel header tabs. Credentials, databases, downloaded media, and composer drafts
  are strictly separated. Unpairing or disconnecting one network never modifies or
  deletes data for the other.
- **WhatsApp QR pairing:** Dedicated pairing flow on the WhatsApp tab displaying
  an in-panel QR code for scanning via WhatsApp > Linked devices on your phone.
  Pairing failures, timeouts, and cancellations cleanly reset to a retryable
  unpaired state.
- **Inbound stickers:** Incoming WhatsApp stickers render inline as image
  attachments (WebP) and persist in local storage.
- **Persisted media:** Downloaded media metadata and recent messages persist
  locally in `whatsapp_store.json` (mode 0600) with bounded bounds (50 chats,
  100 messages each). Failed media downloads display an in-line retry action.
- **Deliberate view-once behavior:** View-once and ephemeral messages are
  deliberately not persisted to disk, cached, or reopened, rendering an explicit
  placeholder in the thread to respect sender privacy.
- **Draft preservation:** Composer drafts are namespaced by network and
  preserved per conversation when switching tabs or navigating conversations.
- **Clean unpair workflows:** Dedicated unpair buttons per network wipe local
  credentials and caches. WhatsApp unpair logs out the session cleanly; Google
  unpair requests remote revocation and reports any remote errors with phone
  instructions.

### Verification

- Go concurrency tests: `go test -race -mod=vendor -count=1 ./...` passed across
  all packages (`internal/daemon`, `internal/store`, `internal/whatsapp`, `internal/wire`).
- Go static analysis: `go vet -mod=vendor ./...` passed.
- Manifest validation: `omarchy plugin validate .` passed with schema version 1.
- Whitespace and formatting: `git diff --check` passed.
- UI and integration suite: `make test-ui` passed 16 JavaScript unit tests, QML
  pagination scroll preservation tests, isolated helper compilation, and the full
  QML component suite covering network switching, draft isolation, and WhatsApp
  unpair flows.

### Known limits

- WhatsApp in this version pages cached companion history (initial phone sync plus
  live messages). OmaChat does not yet issue on-demand `BuildHistorySyncRequest` calls.
- Voice notes, GIF search, reactions, and calling are currently disabled on the
  WhatsApp tab with explicit UI notices rather than falling through to Google Messages.
- Deliberate view-once behavior: view-once media cannot be reopened or re-downloaded.
- Both Google Messages and WhatsApp rely on reverse-engineered protocols (`libgm`
  and vendored `whatsmeow`), which can change upstream without notice.

This is prepared for GitHub release. Marketplace submission remains deferred.
