# Security

This plugin talks to Google Messages and WhatsApp, reads browser cookies once at Google
pairing time, pairs WhatsApp via QR code, and decrypts message content on this
machine so the bar can show it.

## Report a vulnerability

Email or message OneLegDave privately rather than opening a public issue for
credential or protocol bugs.

## What is stored

- Pairing credentials: `~/.local/share/omachat/session.json` (0600)
- WhatsApp device store: `~/.local/share/omachat/whatsapp.db` (0600 from creation)
- WhatsApp chat cache: `~/.local/share/omachat/whatsapp_store.json` (0600)
- Config (chosen browser profile): `~/.local/share/omachat/config.json` (0600)
- Attachment cache: `~/.cache/omachat/media/` and `~/.cache/omachat/media_whatsapp/`
- Control socket: `$XDG_RUNTIME_DIR/omachat/daemon.sock` (0600)

Directories are 0700. The socket is only reachable by your account.

## What is enforced

- Incoming attachments, avatars, and GIF fetches are read through a bounded
  reader. A declared `Content-Length` is never trusted as the allocation size.
- Group avatar URLs must be `https` and must resolve to a public address.
  Loopback, private, link-local, and carrier-grade NAT ranges are refused.
- Child processes are spawned with an argument array, never a shell, except
  for the one-time `go build` of the helper.
- Cookie values are not logged. Errors name which cookie is missing.
- View-once and ephemeral WhatsApp media are omitted from disk persistence and cannot be reopened or redownloaded.
- Dual-network isolation: Google Messages and WhatsApp maintain separate credential stores, database files, and cache directories. Unpairing one network never deletes or exposes files belonging to the other.

## Accepted

- `libgm` is a reverse-engineered client. Google can break or detect it.
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
