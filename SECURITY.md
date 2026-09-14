# Security

This plugin talks to Google Messages, reads browser cookies once at pairing
time, and decrypts message content on this machine so the bar can show it.

## Report a vulnerability

Email or message OneLegDave privately rather than opening a public issue for
credential or protocol bugs.

## What is stored

- Pairing credentials: `~/.local/share/omachat/session.json` (0600)
- Config (chosen browser profile): `~/.local/share/omachat/config.json` (0600)
- Attachment cache: `~/.cache/omachat/media/`
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

## Accepted

- `libgm` is a reverse-engineered client. Google can break or detect it.
- Links in messages open only after a confirm dialog, and only `http`/`https`
  URLs. Images open locally with `xdg-open` after they are already in the cache.
- Pairing copies the browser cookie database to a 0600 tempfile so it can
  be read while the browser holds a lock, then deletes it.

Revoke the paired device on your phone when you stop using this machine.
