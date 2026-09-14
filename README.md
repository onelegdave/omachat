# OmaChat

Version **0.1.1**. See [release notes](https://github.com/onelegdave/omachat/releases/tag/v0.1.1).

Chat from the Omarchy bar. Google Messages first, with room for more networks later: an unread badge, a conversation list, the latest 60 messages per conversation, inline images, and a composer.

![OmaChat inbox with fake demo contacts](preview.png)

The preview uses invented names. Real numbers, messages, and photos stay off GitHub.

This is a native Omarchy shell plugin. `omarchy plugin add` is the install. The protocol helper is a child process of `omarchy-shell`, not a systemd user unit.

## Install

On an Omarchy desktop with the Quickshell plugin system:

```bash
omarchy plugin add https://github.com/onelegdave/omachat --enable
```

Open the bar icon. If the helper is missing, the panel tells you. Go is not on a default Omarchy install. You install it; OmaChat does not.

The helper is **not** in git and is **not** downloaded. The panel will say so and wait. **Go is not part of a default Omarchy install.** If you want OmaChat to talk to Google Messages, install Go yourself, then press **Build helper**. That compiles `omachatd` from this repo (`vendor/`, no extra module download):

```bash
omarchy pkg add go
```

Package: [extra/go](https://archlinux.org/packages/extra/x86_64/go/). You choose whether to install it. Nothing is installed for you.

## What a default Omarchy install already has

These are in the Omarchy ISO / base set. OmaChat uses them as-is:

| Need | Tool | Default Omarchy |
|------|------|-----------------|
| Browser for pairing | Chromium | Yes |
| File picker | xdg-desktop-portal | Yes |
| Copy | `wl-copy` (`wl-clipboard`) | Yes |
| Open links and files | `xdg-open`, `imv`, `mpv` | Yes |
| QR pairing fallback | `qrencode` | Yes |
| Cookie DB / keyring | `sqlite3`, `secret-tool` | Yes |

You still have to pair: open [https://messages.google.com/web](https://messages.google.com/web) in Chromium at least once, then **Pair with Google** in the panel. That is a Google step, not a package.

## Optional extras (you choose)

**Voice notes** (record and play) need the full `ffmpeg` package (`ffmpeg` + `ffplay`). Omarchy ships `ffmpegthumbnailer` for thumbnails, not the ffmpeg CLI. If you want voice:

```bash
omarchy pkg add ffmpeg
```

Package: [extra/ffmpeg](https://archlinux.org/packages/extra/x86_64/ffmpeg/). Text, photos, GIFs from a file, copy, and reactions work without it.

**GIF search** needs a free GIPHY API key that you create and paste in OmaChat Settings (gear). Get one at [developers.giphy.com](https://developers.giphy.com/dashboard/). You can still pick a GIF to send with no key.

### Pairing

Click the bar icon and press **Pair with Google**. The helper reads Google Messages cookies from a Chromium-family browser profile you are already signed in to. Your phone shows several emoji; tap the one that matches.

You must have opened [https://messages.google.com/web](https://messages.google.com/web) in that browser at least once. Signing in to Google generally is not enough: the `OSID` cookie is issued by messages.google.com itself.

**Use a QR code** remains available for older phones that still have a QR scanner under Device pairing. Scan it only with that scanner. The phone camera opens Google's help page (`support.google.com/messages`) and does not pair. Newer Messages builds removed the scanner; use **Pair with Google** on those.

Credentials land in `~/.local/share/omachat/session.json` (mode `0600`) after pairing actually completes.

## Why this is not a web view

Omarchy's shell is one Quickshell process. It also owns the bar, notifications, and the lock screen. Embedding `messages.google.com` with Qt WebEngine aborts that process. QtMultimedia camera backends have the same class of crash. So:

- UI is QML against Omarchy's `qs.Ui` / `qs.Commons` kit (`Panel`, `BarWidget`, `PanelHero`, live theme colors)
- Protocol work runs in `omachatd`, started and restarted by the plugin `service`
- Webcam and voice capture, when added, stay in child `ffmpeg` processes

## Using it

Click the bar icon. Pick a conversation, read the thread, type, press Enter.

Middle-click the bar icon to refresh. The badge is unread conversations, not a third notification daemon. Your phone already notifies you.

Right-click a bubble to copy it.

Attachment captions appear as a separate message after the attachment. If the
caption cannot be confirmed, OmaChat reports that separately so you can check
the conversation before retrying the text.

## Files

| Path | Contents |
|------|----------|
| `~/.config/omarchy/plugins/onelegdave.omachat/` | Plugin checkout |
| `~/.local/share/omachat/session.json` | Pairing credentials, secret |
| `~/.local/share/omachat/config.json` | Browser profile preference, secret |
| `~/.cache/omachat/media/` | Downloaded attachments |
| `$XDG_RUNTIME_DIR/omachat/daemon.sock` | Plugin to helper socket |

Uninstall with `omarchy plugin remove onelegdave.omachat`. That does not delete credentials. Remove those with:

```bash
rm -rf ~/.local/share/omachat ~/.cache/omachat
```

Also revoke the device on your phone under **Messages → Device pairing**.

## Limits

- Reverse-engineered protocol (`libgm`). Google can break it without notice.
- Pairing uses one Messages-for-web device slot.
- RCS and end-to-end chats relay through your phone. The phone has to stay online.
- Inbox of 50 conversations, with the latest 60 messages in each thread. Older-history pagination is not implemented.
- Incoming GIFs play in the thread. Pick a GIF to send the same way as a photo. Optional GIPHY search needs a personal API key in OmaChat Settings (gear in the header). Get a free key at developers.giphy.com, create an app, paste the key. It is stored in ~/.local/share/omachat/config.json, never shown back to the panel.
- Voice notes: tap Rec to record (ffmpeg, not QtMultimedia), Play to preview, then send. Incoming voice plays with ffplay. Received video opens in the default player. Video calling is not in this release.

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

OneLegDave is the project owner and human maintainer. Codex (OpenAI) is an AI development lead and contributor, working under OneLegDave's direction. agy assisted with focused reviews and regression tests. AI assistance does not imply OpenAI endorsement or change the upstream authorship and license notices.

## License

Created and maintained by [OneLegDave](https://www.onelegdave.dev/). Other Omarchy plugins: [OmaDroid](https://github.com/onelegdave/omadroid), [System QuikView](https://github.com/onelegdave/system-quikview).

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Helper protocol code adapted from Marc Ford remains under MIT with his copyright. mautrix libgm remains theirs.
