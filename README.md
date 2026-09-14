# OmaChat

Version **0.3.1**. See the [GitHub release notes](https://github.com/onelegdave/omachat/releases/tag/v0.3.1) or the local [release notes](RELEASE-0.3.1.md).

OmaChat is a Native Omarchy Plugin for Google Messages, WhatsApp, and Telegram. It provides conversation lists, paginated history, inline media, voice notes, static WebP stickers, isolated service sessions, and a keyboard-friendly composer.

![OmaChat inbox with fake demo contacts](preview.png)

The preview uses invented names. Real numbers, messages, and photos stay off GitHub.

This is a Native Omarchy Plugin. `omarchy plugin add` is the install. The protocol helper is a child process of `omarchy-shell`, not a systemd user unit.

Service-specific setup and implementation notes are in the [Telegram service
guide](TELEGRAM-IMPLEMENTATION.md) and [WhatsApp service guide](WHATSAPP-IMPLEMENTATION.md).
Google Messages setup is documented below because it is the original service.

## Features

- **Three independent services:** Switch between Google Messages, WhatsApp, and Telegram with dedicated tabs in one plugin panel.
- **Telegram:** Pair with Telegram through its MTProto QR flow, sync chats and messages, send text, photos, captions, and voice notes, and receive photos, voice notes, and static WebP stickers.
- **Service isolation:** Independent session stores, credentials, media directories, and composer drafts. Actions or unpairing on one service never affect another.
- **QR pairing for WhatsApp:** Explicit QR code pairing directly in the panel using WhatsApp Linked Devices on your phone.
- **Inbound stickers:** Incoming WhatsApp stickers render inline as image attachments and persist in local storage.
- **Telegram media limits:** Animated TGS/video stickers are reported as unsupported. Telegram self-destructing media is never cached.
- **Persisted media:** Downloaded attachments and message metadata persist locally with bounded cache management and retryable downloads across restarts.
- **Deliberate view-once behavior:** View-once and ephemeral media are intentionally not saved, cached, or reopened, protecting sender privacy with a visible placeholder.
- **Message history and pagination:** Threads open with the latest 60 messages and page older history on demand while preserving your scroll position.
- **Independent unpair and cleanup:** Local credentials and caches are wiped cleanly, with actionable warnings if remote device revocation fails.

## Install

On an Omarchy desktop with the Quickshell plugin system:

```bash
omarchy plugin add https://github.com/onelegdave/omachat --enable
```

Open the plugin from the Omarchy bar. If the helper is missing, the panel explains what is needed. OmaChat does not install dependencies or run package-manager commands for you.

The helper is **not** in git and is **not** downloaded. The panel will say so and wait. **Go is not part of a default Omarchy install.** If you choose to build the helper for any service, install Go yourself, then press **Build helper**. That compiles `omachatd` from this repo (`vendor/`, with no module download). WhatsApp linking also needs a C compiler (`gcc` or `clang`) because the helper uses CGO sqlite:

```bash
omarchy pkg add go
```

Package: [extra/go](https://archlinux.org/packages/extra/x86_64/go/). This is an optional user choice. OmaChat never installs it.

## What a default Omarchy install already has

These are commonly available on an Omarchy desktop. OmaChat uses them as-is and does not install or update them:

| Need | Tool | Default Omarchy |
|------|------|-----------------|
| Browser for pairing | Chromium | Yes |
| File picker | xdg-desktop-portal | Yes |
| Copy | `wl-copy` (`wl-clipboard`) | Yes |
| Open links and files | `xdg-open`, `imv`, `mpv` | Yes |
| QR pairing fallback | `qrencode` | Yes |
| Cookie DB / keyring | `sqlite3`, `secret-tool` | Yes |

You still choose, install, and pair each service yourself. For Google, open [https://messages.google.com/web](https://messages.google.com/web) in Chromium at least once, then **Pair with Google** in the panel. For WhatsApp, open the WhatsApp tab and **Use a QR code**, then scan it from **Linked devices** on your phone. For Telegram, configure your own API credentials, then choose **Pair with Telegram** and scan its QR code. The helper does not pair automatically.

| Service | User-provided setup | Tools used by that service |
| --- | --- | --- |
| Google Messages | Chromium-family browser signed in to Messages; phone online | `sqlite3` and `secret-tool` for browser cookies |
| WhatsApp | Phone with **Linked devices** available; phone online | `gcc` or `clang` for the CGO SQLite build |
| Telegram | `api_id` and `api_hash` from [my.telegram.org](https://my.telegram.org); Telegram app for QR linking | `ffmpeg` and `ffplay` for voice notes |

The panel reports a missing tool and waits for you to decide whether to install
it. OmaChat never installs, updates, or downloads a dependency on your behalf.

## Optional extras (you choose)

**Voice notes** (record and play) need the full `ffmpeg` package (`ffmpeg` + `ffplay`). Omarchy ships `ffmpegthumbnailer` for thumbnails, not the ffmpeg CLI. If you want voice:

```bash
omarchy pkg add ffmpeg
```

Package: [extra/ffmpeg](https://archlinux.org/packages/extra/x86_64/ffmpeg/). Text, photos, GIFs from a file, copy, and reactions work without it.

**GIF search** needs a free GIPHY API key that you create and paste in OmaChat Settings (gear). Get one at [developers.giphy.com](https://developers.giphy.com/dashboard/). You can still pick a GIF to send with no key.

### Pairing

Open the plugin and press **Pair with Google**. The helper reads Google Messages cookies from a Chromium-family browser profile you are already signed in to. Your phone shows several emoji; tap the one that matches.

You must have opened [https://messages.google.com/web](https://messages.google.com/web) in that browser at least once. Signing in to Google generally is not enough: the `OSID` cookie is issued by messages.google.com itself.

**Use a QR code** remains available for older phones that still have a QR scanner under Device pairing. Scan it only with that scanner. The phone camera opens Google's help page (`support.google.com/messages`) and does not pair. Newer Messages builds removed the scanner; use **Pair with Google** on those.

On the WhatsApp tab, **Use a QR code** is the pairing path. Open WhatsApp on your phone, **Linked devices**, **Link a device**, and scan the code shown in the panel. Do not scan it with the ordinary camera app.

On the Telegram tab, first create an API application at
[my.telegram.org](https://my.telegram.org). In a terminal in this checkout,
run `python3 scripts/configure-telegram.py` and enter the `api_id` and
`api_hash` yourself. OmaChat stores them in the private config file, then the
**Pair with Telegram** button shows a QR code. In Telegram, open **Settings >
Devices > Link Desktop Device** and scan it. A Telegram session is separate
from the Google and WhatsApp sessions.

Google credentials land in `~/.local/share/omachat/session.json` (mode `0600`) after pairing actually completes. WhatsApp device keys land in `~/.local/share/omachat/whatsapp.db` (mode `0600` from creation) plus a private chat cache `whatsapp_store.json`.

OmaChat never starts pairing automatically during startup or authentication recovery. If Google invalidates the session, the panel asks you to select **Pair with Google** again. If WhatsApp unlinks the desktop, the panel asks you to scan a QR code again. A reconnect stays **Connecting** until an authenticated session succeeds.

## Why this is not a web view

Omarchy's shell is one Quickshell process. It also owns the bar, notifications, and the lock screen. Embedding `messages.google.com` with Qt WebEngine aborts that process. QtMultimedia camera backends have the same class of crash. So:

- UI is QML against Omarchy's `qs.Ui` / `qs.Commons` kit (`Panel`, `BarWidget`, `PanelHero`, live theme colors)
- Protocol work runs in `omachatd`, started and restarted by the plugin `service`. All three services share that helper process but never share sessions, credentials, or inboxes.
- Webcam and voice capture, when added, stay in child `ffmpeg` processes

## Using it

Open the plugin. Switch between Google Messages, WhatsApp, and Telegram using the header tabs. Pick a conversation, read the thread, type, and press Enter.

Middle-click the bar icon to refresh. The badge shows unread conversations across active services. Your phone already notifies you.

Right-click a bubble to copy it.

Drafts are preserved per conversation and isolated between networks. Typing a message in Google Messages remains saved when switching to the WhatsApp tab or navigating other chats.

Inbound WhatsApp stickers appear inline in the thread as image attachments. Downloaded media files are stored locally in the cache and can be retried if a download initially fails.

Attachment captions appear as a separate message after the attachment. If the
caption cannot be confirmed, OmaChat reports that separately so you can check
the conversation before retrying the text.

The **Unpair this desktop** button clears local credentials and cached media for
the active network without affecting the other network. On Google Messages, it
also requests device revocation from Google; if that request fails, OmaChat shows
a warning and asks you to remove the device in **Google Messages > Device pairing**
on your phone. On WhatsApp, it logs out the session and removes local stores.
A local cleanup failure is reported separately.

## Files

| Path | Contents |
|------|----------|
| `~/.config/omarchy/plugins/onelegdave.omachat/` | Plugin checkout |
| `~/.local/share/omachat/session.json` | Google pairing credentials, secret |
| `~/.local/share/omachat/whatsapp.db` | WhatsApp device store (sqlite, 0600) |
| `~/.local/share/omachat/whatsapp_store.json` | Cached WhatsApp chats and downloadable media metadata |
| `~/.local/share/omachat/telegram.session` | Telegram session credentials, secret (when paired) |
| `~/.local/share/omachat/telegram_store.json` | Cached Telegram chats (when paired) |
| `~/.local/share/omachat/config.json` | Browser profile, GIPHY key, and Telegram API credentials, secret |
| `~/.cache/omachat/media/` | Google downloaded attachments |
| `~/.cache/omachat/media_whatsapp/` | WhatsApp downloaded attachments |
| `~/.cache/omachat/media_telegram/` | Telegram downloaded attachments (when paired) |
| `$XDG_RUNTIME_DIR/omachat/daemon.sock` | Plugin to helper socket |

Uninstall with `omarchy plugin remove onelegdave.omachat`. That does not delete credentials. Remove those with:

```bash
rm -rf ~/.local/share/omachat ~/.cache/omachat
```

Also revoke the device on your phone under **Messages > Device pairing** and, if you used WhatsApp, **WhatsApp > Linked devices**.

## Limits

- Reverse-engineered protocol (`libgm`). Google can break it without notice.
- Pairing uses one Messages-for-web device slot.
- RCS and end-to-end chats relay through your phone. The phone has to stay online.
- Inbox of 50 conversations. Threads open with the latest 60 messages; **Load older messages** fetches earlier pages while keeping your reading position. Refresh retains loaded history. Switching conversations starts again with the latest page.
- WhatsApp in this version pages cached companion history (initial phone sync plus live messages). whatsmeow can request on-demand phone history with `BuildHistorySyncRequest`; OmaChat does not send that request yet.
- Telegram support integrates the pure Go gotd MTProto client behind a testable abstraction and context-safe QR login flow (`StartPairing`), with isolated dialog/message synchronization through `telegram_store.json`. Text, image, caption, voice-note, photo, audio, and static WebP sticker flows are supported. Animated TGS/video stickers remain unsupported, and media references are live-only until refreshed.
- Deliberate view-once behavior: view-once and ephemeral media are intentionally not stored, cached, or reopened, presenting a placeholder in the thread to respect sender privacy.
- WhatsApp voice notes, GIF search, reactions, and calling are currently disabled with clear UI notices rather than falling through to Google Messages.
- Incoming GIFs play in the thread. Pick a GIF to send the same way as a photo. Optional GIPHY search needs a personal API key in OmaChat Settings (gear in the header). Get a free key at developers.giphy.com, create an app, paste the key. It is stored in ~/.local/share/omachat/config.json, never shown back to the panel.
- To configure Telegram API credentials locally, run `python3 scripts/configure-telegram.py` from the project checkout. The script prompts locally and stores the values with mode 0600.
- Voice notes: tap Rec to record (ffmpeg, not QtMultimedia), Play to preview, then send. Google Messages records M4A; Telegram records OGG/Opus. Incoming voice plays with ffplay. Received video opens in the default player. Video calling is not in this release.

## Development

```bash
make helper     # go build -mod=vendor
make test       # go test -mod=vendor ./...
make validate   # omarchy plugin validate .
```

The helper can be driven by hand:

```bash
./bin/omachatd --log-level debug --socket /tmp/gm.sock
printf '{"id":"1","method":"status"}\n' | socat - UNIX-CONNECT:/tmp/gm.sock
```

## Credits

Protocol work is the [mautrix](https://github.com/mautrix/gmessages) project's. The helper is adapted from [Marc Ford's gmessages-omarchy-plugin](https://github.com/MarcFord/gmessages-omarchy-plugin) (MIT). The Omarchy service lifecycle and panel are mine.

OneLegDave is the project owner and human maintainer.

## License

Created and maintained by [OneLegDave](https://www.onelegdave.dev/). Other Omarchy plugins: [OmaDroid](https://github.com/onelegdave/omadroid), [System QuikView](https://github.com/onelegdave/system-quikview).

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Helper protocol code adapted from Marc Ford remains under MIT with his copyright. mautrix libgm remains theirs. Vendored whatsmeow remains MPL-2.0.
