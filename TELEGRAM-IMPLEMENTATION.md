# Telegram service guide

OmaChat is a Native Omarchy Plugin. Telegram runs in the shell-owned
`omachatd` helper through the pure Go `gotd/td` MTProto client. The helper is a
child process of `omarchy-shell`; OmaChat does not install a systemd unit.

## Pairing and credentials

Create an application at [my.telegram.org](https://my.telegram.org) and enter
its `api_id` and `api_hash` locally. OmaChat never includes these credentials
in the repository, logs, or QML. Run:

```bash
python3 scripts/configure-telegram.py
```

The configuration is stored in `~/.local/share/omachat/config.json` with mode
`0600`. The user chooses whether to install Go, ffmpeg, qrencode, or any other
optional dependency. OmaChat never installs dependencies.

Select Telegram in the panel and choose **Pair with Telegram**. OmaChat shows
a Telegram QR login token and keeps the client session in
`~/.local/share/omachat/telegram.session` with private permissions. Pairing is
explicit and does not start automatically during startup or recovery.

## Supported behavior

- Dialog and message synchronization with peer access-hash resolution.
- Live incoming text and media updates.
- Outbound text, photos with captions, and OGG/Opus or M4A voice notes.
- On-demand inbound photo and audio downloads.
- Static WebP sticker downloads through the normal image renderer.
- Mark-as-read support through Telegram history acknowledgements.

Animated TGS stickers and video stickers remain unsupported. Telegram media
references are live-only and are repopulated by refresh; downloaded files may
remain in the local cache. Telegram self-destructing or TTL media never gets a
download reference or cached copy.

## Storage and isolation

| Data | Location |
| --- | --- |
| Session | `~/.local/share/omachat/telegram.session` |
| Conversation cache | `~/.local/share/omachat/telegram_store.json` |
| Media cache | `~/.cache/omachat/media_telegram/` |
| Configuration | `~/.local/share/omachat/config.json` |

Telegram state is separate from Google Messages (`session.json`, `media/`) and
WhatsApp (`whatsapp.db`, `whatsapp_store.json`, `media_whatsapp/`). Unpairing
Telegram clears only Telegram session, cache, and media data.

Media downloads use private directories, temporary files, size checks, and
atomic rename. Media keys are treated as opaque values and are never used as
untrusted path components.

## Development checks

Build and test offline from the vendored dependencies:

```bash
go build -mod=vendor ./cmd/omachatd
go test -mod=vendor ./...
go test -race -mod=vendor -count=1 ./...
make test-ui
omarchy plugin validate .
```

Telegram unit tests use mock clients and do not contact Telegram. Live pairing
and message tests require the user to pair a device and confirm the result in
the Telegram app.
