# Security

This Native Omarchy Plugin talks to Google Messages, WhatsApp, Telegram, and Messenger. It
reads browser cookies while checking pairing profiles, pairing, changing the
selected profile, and recovering from authentication failures. WhatsApp and
Telegram use their QR flows. Message content and credentials are cached locally.

## Report a vulnerability

Use [GitHub's private vulnerability report form](https://github.com/onelegdave/omachat/security/advisories/new)
for credential or protocol bugs. Do not include credentials, personal messages,
or working exploit details in a public issue.

## What is stored

- Pairing credentials: `~/.local/share/omachat/session.json` (0600)
- WhatsApp device store: `~/.local/share/omachat/whatsapp.db` (0600 from creation)
- WhatsApp chat cache: `~/.local/share/omachat/whatsapp_store.json` (0600)
- Telegram session and chat cache: `~/.local/share/omachat/telegram.session` and `telegram_store.json` (0600)
- Messenger session, encrypted-device state, and chat cache: `~/.local/share/omachat/messenger.db` and `messenger_store.json` (0600)
- Config (browser profile, GIPHY key, Telegram API credentials): `~/.local/share/omachat/config.json` (0600)
- Attachment cache: `~/.cache/omachat/media/`, `media_whatsapp/`, `media_telegram/`, and `media_messenger/`
- Telegram conversation caches from before typed peer IDs are ignored on upgrade;
  pairing credentials are retained and ambiguous attachment filenames are not reused.
- Control socket: `$XDG_RUNTIME_DIR/omachat/daemon.sock` (0600)

OmaChat's private directories are restricted to 0700. The socket is restricted
to 0600. These permissions do not isolate OmaChat from other programs running
as the same user, or from root. Stored credentials and caches are not encrypted
by OmaChat. Trusted XDG parent directories and a trusted desktop account are
part of the security boundary.

Update preferences and cached public release metadata live in
`~/.local/state/omachat/updates.json` (or under `XDG_STATE_HOME`). They are
separate from messaging credentials. Daily checks are off by default.

## What is enforced

- Incoming attachments, avatars, and GIF fetches are read or written through a bounded
  stream. Telegram downloads stop at 32 MiB while transferring. Each service
  evicts old downloaded attachments above its 256 MiB cache budget. A declared
  `Content-Length` is never trusted as the allocation size.
- Group avatar URLs must be `https` and must resolve to a public address.
  Loopback, private, link-local, and carrier-grade NAT ranges are refused.
- OmaChat's direct child-process launches use argument arrays, not interpolated
  shell commands. External tools and desktop launchers have their own behavior.
- Cookie values are not logged. Errors name which cookie is missing.
- View-once and ephemeral WhatsApp media are omitted from disk persistence and cannot be reopened or redownloaded.
- Telegram self-destructing media is omitted from disk persistence and cannot be reopened or redownloaded.
- Google Messages, WhatsApp, Telegram, and Messenger maintain separate credential stores, databases, sessions, and cache directories. Unpairing one network never deletes or exposes files belonging to another.

## Accepted

- Community plugins and the helper run as your user without a security sandbox.
  Service separation prevents accidental cross-service state reuse, not access
  by a compromised helper or another process under the same account.
- Enabled services contact their messaging providers. Optional GIPHY search
  sends the query and API key to GIPHY; the QML picker loads preview images
  directly from allowed GIPHY HTTPS hosts. No claim of offline-only operation
  or anonymity is made.
- Manual update checks and optional daily checks request public release metadata
  from GitHub. They send no messaging credentials or message content; GitHub
  receives ordinary request metadata, including the client IP address. Updates
  require an explicit action and the native updater's terminal confirmation.
  The updater follows the repository default branch, not a pinned release tag.
- Source review and regression tests are limited checks, not a guarantee that
  this project or its dependencies contain no vulnerabilities. Keep the desktop
  toolchain and external tools current through your normal update workflow.

- `libgm` is a reverse-engineered client. Google can break or detect it.
- `mautrix-meta` uses Meta's unofficial Messenger client protocol. Meta can
  break or detect it, invalidate sessions, or restrict an account. Messenger
  pairing imports the minimum required browser cookies into a separate private
  store; cookie values are never exposed to QML or logs.
- Links in messages open only after a confirm dialog, and only `http`/`https`
  URLs. Images open locally with `xdg-open` after they are already in the cache.
- Pairing copies the browser cookie database to a 0600 tempfile so it can
  be read while the browser holds a lock, then deletes it.

Revoke the paired device on your phone when you stop using this machine.

## Vendored upstream API-key findings

GitHub Secret Scanning reports two Google API-key patterns in vendored protocol
code: the Google Messages relay constant from `mautrix-gmessages` and a
Google-looking string in the `whatsmeow` token dictionary. These are inherited
upstream client/protocol constants, not OmaChat user credentials. Do not remove
or obfuscate them locally: the relay constant is required for Google Messages
requests, and changing token-dictionary entries can alter WhatsApp wire indexes.
Track upstream fixes and update the vendored dependencies when they are
available. User credentials remain local under `~/.local/share/omachat/` and are
never committed.

Upstream tracking: [mautrix/gmessages#94](https://github.com/mautrix/gmessages/issues/94)
and [whatsmeow#1266](https://github.com/tulir/whatsmeow/issues/1266).
