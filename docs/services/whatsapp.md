# WhatsApp service guide

Enable WhatsApp in **Settings > Services** before pairing. Disabling it keeps
credentials but stops its client after the shared helper restarts.

[Overview](../../README.md) · [Dependencies](../dependencies.md)

OmaChat is a Native Omarchy Plugin. WhatsApp runs in the shell-owned
`omachatd` helper through the vendored `whatsmeow` client. The helper is a child
process of `omarchy-shell`; OmaChat does not install a systemd unit.

## Pairing and dependencies

Select WhatsApp in the panel and choose **Use a QR code**. In WhatsApp on the
phone, open **Linked devices**, choose **Link a device**, and scan the code.
Pairing is explicit and does not start automatically.

The user chooses whether to install Go, a C compiler, ffmpeg, qrencode, or any
other optional dependency. OmaChat never installs dependencies automatically. The helper
build uses Go modules from `vendor/`; the WhatsApp SQLite store requires
CGO and a standard C compiler such as gcc or clang.

## Supported behavior

- QR pairing and reconnect handling with clear failure states.
- Conversation and message synchronization with live incoming updates.
- Send text, images, GIF files, and captions. Incoming stickers render as
  WebP image attachments; there is no dedicated outgoing sticker picker.
- On-demand media downloads with bounded files and retry behavior.
- View-once and ephemeral media are intentionally not cached or reopened.

WhatsApp history is based on the initial phone sync and cached live messages.
OmaChat does not currently request additional on-demand phone history. Calling,
voice notes, GIF search, and reactions remain explicitly unavailable in the
WhatsApp panel. The shared composer keeps those actions isolated from the
Google Messages and Telegram services.

## Storage and isolation

| Data | Location |
| --- | --- |
| Device database | `~/.local/share/omachat/whatsapp.db` |
| Conversation cache | `~/.local/share/omachat/whatsapp_store.json` |
| Media cache | `~/.cache/omachat/media_whatsapp/` |
| Configuration | `~/.local/share/omachat/config.json` |

WhatsApp state is separate from Google Messages (`session.json`, `media/`) and
Telegram (`telegram.session`, `telegram_store.json`, `media_telegram/`).
Unpairing WhatsApp logs out the WhatsApp session and clears only WhatsApp
state. It does not delete another network's credentials or cache.

If the QR expires, retry from the WhatsApp tab. If the desktop was unlinked,
scan a new QR code. Use the in-app unpair action before uninstalling, and
check **WhatsApp > Linked devices** on your phone to remove any remaining
device. Empty older-history pages can mean the phone has not supplied that
history, rather than a download failure.

Media downloads validate declared and actual sizes, use private cache paths,
and avoid exposing untrusted filenames. Session and cache files are written
with private permissions and atomic replacement where applicable.

## Development checks

Build and test offline from the vendored dependencies:

```bash
make helper
go test -mod=vendor ./...
go test -race -mod=vendor -count=1 ./...
make test-ui
omarchy plugin validate .
```

WhatsApp unit and regression tests use mock clients where possible. Live QR
pairing and send/receive checks require a user-owned phone and account.
