# Telegram service guide

Enable Telegram in **Settings > Services** before pairing. Disabling it keeps
credentials but stops its client after the shared helper restarts.

[Overview](../../README.md) · [Dependencies](../dependencies.md)

OmaChat is a Native Omarchy Plugin. Telegram runs in the shell-owned
`omachatd` helper through the pure Go `gotd/td` MTProto client. The helper is a
child process of `omarchy-shell`; OmaChat does not install a systemd unit.

## Pairing and credentials

Create an application at [my.telegram.org](https://my.telegram.org) and enter
its `api_id` and `api_hash` locally. OmaChat never includes these credentials
in the repository, logs, or QML. Run:

```bash
cd ~/.config/omarchy/plugins/onelegdave.omachat
python3 scripts/configure-telegram.py
omarchy restart shell
```

The helper reads configuration at startup. Restart the shell immediately after
saving credentials, before changing OmaChat Settings, so the running helper
loads the new values. Restarting briefly reloads the whole Omarchy shell.

Run the setup script as your normal user, without `sudo`. Enter the numeric
`api_id` first. At the `api_hash` prompt, typing or pasting displays no characters
or asterisks: the input looks blank by design. Enter the full hash and press
Enter. This is your application API hash, not your Telegram login password.
The `config.json` path is a storage location, not a terminal command.
Saving these credentials does not pair your account automatically; restart,
then choose **Pair with Telegram** as described below.

The configuration is stored in `~/.local/share/omachat/config.json` with mode
`0600`. The user chooses whether to install Go, ffmpeg, qrencode, or any other
optional dependency. OmaChat never installs dependencies automatically.

Select Telegram in the panel and choose **Pair with Telegram**. OmaChat shows
a Telegram QR login token and keeps the client session in
`~/.local/share/omachat/telegram.session` with private permissions. Pairing is
explicit and does not start automatically during startup or recovery.

In the Telegram phone app, open **Settings > Devices > Link Desktop Device**
and scan the displayed QR code. Keep API credentials private. Python 3 is
needed only for the configuration script; the shared helper needs Go and a
C compiler to build. See the dependency guide before building.

If credentials are missing, run the configuration script and restart the shell
before retrying.
If pairing fails or the code expires, retry from the Telegram tab. If media
is unavailable after a restart, refresh the conversation to renew references.
Use **Unpair this desktop** on the Telegram tab to clear local session and
cached content, and check **Settings > Devices** on your phone to revoke any
remaining session. API application credentials remain in the shared config.

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
make helper
go test -mod=vendor ./...
go test -race -mod=vendor -count=1 ./...
make test-ui
omarchy plugin validate .
```

Telegram unit tests use mock clients and do not contact Telegram. Live pairing
and message tests require the user to pair a device and confirm the result in
the Telegram app.
