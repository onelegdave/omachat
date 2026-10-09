# WhatsApp profile pictures

Status: **Implemented; local synthetic gates passed.** Scope and acceptance boundary were agreed on October 8, 2026. Live linked-device compatibility and the user's visual acceptance remain pending; no WhatsApp account, device, or live phone was accessed.

## Agreed scope and behavior

- Add WhatsApp pictures for both direct conversations and groups.
- Fetch pictures in the background after WhatsApp is connected, without blocking conversation/history loading or user sends.
- Cache pictures locally and expose the cached file through the existing conversation avatar field. If no picture is available, access is denied, or fetching fails, keep the existing initials/color fallback; avatar failure must not affect messaging.
- Keep all implementation and automated tests synthetic. The user will perform any linked-account/device test and visual acceptance afterward.
- Do not add Signal support in this slice.

## Existing implementation findings

These are source-inspection findings, not exercised feasibility evidence:

- `internal/wire/wire.go` already defines `Conversation.AvatarPath`; the UI/daemon path can carry a local avatar path.
- `internal/daemon/avatars.go` contains a background avatar-store/fetch pattern for Google Messages, including per-conversation completion events and a fallback when there is no photo. It is an analogue, not proof that WhatsApp has the same transport or event lifecycle.
- `internal/store/store.go` provides the private WhatsApp media directory. The new avatar files share its existing 256 MiB media-cache pruning behavior and are removed by the existing WhatsApp unpair cleanup.
- `internal/whatsapp/client.go` is the mockable backend interface; it now exposes whatsmeow's profile-picture lookup method for synthetic tests.
- The pinned vendored whatsmeow source has `Client.GetProfilePictureInfo(ctx, jid, params)` (`vendor/go.mau.fi/whatsmeow/user.go`). Its documented result provides a download URL and profile-picture ID; passing an existing ID can report no change. The API covers user and group photos. This confirms a library API exists, not that this repository's session, group identity, URL download, caching, or UI integration has been exercised.
- WhatsApp conversations are built and updated in `internal/whatsapp/backend.go`; group/direct JIDs and saved contact identity/name resolution already exist there. The avatar work should use canonical conversation JIDs, not display names or phone-number guesses.
- The WhatsApp history plan prohibits the agent from reading personal account data, connecting to the live helper, launching/reloading the production app, or doing live phone tests. Those boundaries apply here too.

## Implemented decisions and limits

- Avatar lookups accept ordinary direct user/LID JIDs and standard group JIDs; unsupported JID types fall back to initials. No identity is inferred from display names.
- The worker serializes requests, coalesces duplicate work, runs outside message/history paths, and spaces requests. Errors are retried on reconnect or a picture-change event rather than in a hot loop.
- Picture changes use the pinned `events.Picture` event; removals clear the local path. Content-derived cache paths make changed images visible to QML's image cache.
- Downloads are HTTPS-only and restricted to WhatsApp CDN domains, have a 20-second timeout and 4 MiB body cap, and accept fully decoded JPEG/PNG/GIF images up to 2048×2048. Files use private atomic writes and the existing media-cache pruning path.
- Cached images share the 256 MiB WhatsApp media cache with attachments; the history-cache allowance remains separate. Existing WhatsApp unpair cleanup removes them.
- `InboxView.qml` already passes `avatarPath` to the shared `Avatar.qml` in its conversation list and thread header. The new QML fixture verifies initials fallback, image decode, and changed-path reload in `Avatar.qml`; the full inbox list/pop-out composition and installed UI remain for the user's visual check.

## Implementation summary

The implementation is in `internal/whatsapp/avatar.go`, with backend/client/mock wiring and focused tests. The service guide documents local caching, fallbacks, and storage.

Verified local commands: `make test`, `make lint`, `go test -race -mod=vendor -count=1 ./...`, `make helper`, `make validate`, and `make test-ui` all passed after the implementation. The helper/UI test paths use synthetic fixtures and isolated temporary runtime/data paths; no production helper or shell restart was performed.

## Acceptance matrix

- [x] Synthetic direct-contact and group lookups preserve their canonical JIDs and publish local avatar paths.
- [x] Background retrieval is non-blocking at enqueue and serialized; picture-change events refresh images, removal clears them, and unavailable-photo fallback is preserved.
- [x] Image URL, body-size, format, dimension, permission, and cache-budget protections are implemented; history-cache accounting remains separate.
- [x] Stale-session completion cannot write the old session's avatar file.
- [x] Dedicated `Avatar.qml` fixture verifies initials fallback, local image decode, and changed-path reload.
- [ ] Full InboxView conversation-list/pop-out composition and installed UI rendering are confirmed by the user.
- [x] Local test, race, lint, helper build, validation, and UI suites passed (commands listed above).
- [x] No WhatsApp account data, credentials, helper socket, or live phone were accessed.
- [ ] User confirms real direct/group photos, refresh after a photo change/reconnect, fallback behavior, and installed UI rendering.

## User-only manual acceptance

After the user chooses to deploy the locally verified change:

1. Check a direct contact with a visible profile photo and one without a visible photo.
2. Check a group with a photo and one without; note any group/community type that falls back unexpectedly.
3. Confirm pictures appear without delaying opening or using conversations, and that unavailable pictures show initials.
4. Disconnect/reconnect or restart normally and confirm cached pictures remain and changed pictures eventually refresh.
5. Unpair only if already intended; verify the documented WhatsApp cleanup behavior without affecting other services.

Report non-sensitive symptoms only; no message contents, identifiers, account logs, or screenshots are needed.
