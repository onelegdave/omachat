# OmaChat

A **Native Omarchy Plugin** for Google Messages, WhatsApp, and Telegram.
Read and reply to conversations in a native panel with separate service
sessions, conversation drafts, history, and inline media.

Current release: **0.3.1**. [Release notes](https://github.com/onelegdave/omachat/releases/tag/v0.3.1).

![OmaChat inbox with invented demo contacts](preview.png)

The preview uses fictional contacts. Never include private conversations or
account details in public screenshots or bug reports.

## Install and build

On an Omarchy desktop with the Quickshell plugin system:

```bash
omarchy plugin add https://github.com/onelegdave/omachat --enable
```

Open OmaChat from the bar. All services use one helper, `omachatd`, built
locally from vendored source. Building requires **Go and a C compiler
(gcc or clang)**, even if you only use Google Messages or Telegram.
No helper binary is shipped in Git or downloaded by OmaChat.

**OmaChat never installs dependencies.** If tools are missing, the app explains
what is needed. You decide whether to install them yourself and whether to
enable the feature. For example, if you choose to install the build tools:

```bash
omarchy pkg add go gcc
```

Then select **Build helper** in the panel. This compiles the included source
with `go build -mod=vendor`; it does not install packages or download modules.
The shell owns the helper as a child process. There is no systemd unit,
installer hook, or sudo step in the plugin.

See [dependencies and optional features](docs/dependencies.md) for the full
tool list. Availability varies by desktop; do not assume a tool is installed.

## Connect your services

Each service is optional and pairs only when you request it.

| Service | Setup | Guide |
| --- | --- | --- |
| Google Messages | Sign in to Messages for web in a supported Chromium-family browser, then select **Pair with Google** and confirm the matching emoji on the phone. | [Google Messages](docs/services/google-messages.md) |
| WhatsApp | Select **Use a QR code** in the WhatsApp tab and scan it using the phone's **Linked devices** screen. | [WhatsApp](docs/services/whatsapp.md) |
| Telegram | Configure your own Telegram API credentials, select **Pair with Telegram**, and scan the QR code from Telegram's **Devices** screen. | [Telegram](docs/services/telegram.md) |

The guides include requirements, pairing, supported features, limitations,
storage, and recovery. A service losing authentication does not automatically
start pairing again.

## Features and limits

| Feature | Google Messages | WhatsApp | Telegram |
| --- | --- | --- | --- |
| Conversation list, text, per-chat drafts | Yes | Yes | Yes |
| Photos and captions | Yes | Yes | Yes |
| Send GIF files | Yes | Yes | No dedicated GIF sending support |
| Voice recording and playback | Optional ffmpeg/ffplay | Unavailable | Optional ffmpeg/ffplay |
| GIPHY search | Optional personal API key | Unavailable | Unavailable |
| Reactions | Yes | Unavailable | Unavailable |
| Incoming static WebP stickers | No dedicated sticker support | Yes | Yes |
| Older history | Fetch older pages | Page cached phone-sync history | Fetch older pages |
| Calling | Unavailable | Unavailable | Unavailable |

Threads open with the latest 60 messages; **Load older messages** pages back
while preserving the reading position. WhatsApp cannot currently request
additional phone history beyond what has synced. Telegram animated TGS/video
stickers are unsupported, and media references need refreshing after restart.
WhatsApp view-once/ephemeral and Telegram self-destructing media are not saved
or reopened.

The services use unofficial protocol clients and can stop working when their
providers change. Automated checks do not establish live account compatibility
for every service. See [verification history](docs/history/README.md) for dated
evidence and its limits.

## Everyday use

Switch services with the header tabs, choose a conversation, and press Enter
to send. Drafts stay with their service and conversation. Right-click a bubble
to copy it. Middle-click the bar icon to refresh; the badge counts unread
conversations across active services.

Pick an attachment from the composer. Google Messages sends captions separately
after the attachment; check the conversation before retrying a caption reported
as unconfirmed. WhatsApp and Telegram include captions with media. Failed media
downloads can be retried. Incoming GIFs play inline;
video opens in an external player.

For Google Messages or Telegram voice notes, choose **Rec**, record, **Play**
to preview, then send. Voice needs optional `ffmpeg` and `ffplay`. Google
records M4A and Telegram records OGG/Opus. Google GIF search requires your own
GIPHY API key in Settings; sending a local GIF does not require a key.

## Privacy, storage, and removal

Sessions, chat caches, media, and drafts are separated by service. All three
services share the helper process and configuration file. Private files are
kept locally; see [Security](SECURITY.md) for protections and limitations.

| Location | Contents |
| --- | --- |
| `~/.config/omarchy/plugins/onelegdave.omachat/` | Plugin checkout and locally built helper |
| `~/.local/share/omachat/session.json` | Google Messages credentials |
| `~/.local/share/omachat/whatsapp.db` and `whatsapp_store.json` | WhatsApp credentials and chat cache |
| `~/.local/share/omachat/telegram.session` and `telegram_store.json` | Telegram credentials and chat cache |
| `~/.local/share/omachat/config.json` | Browser selection, GIPHY key, Telegram API credentials |
| `~/.cache/omachat/media/`, `media_whatsapp/`, `media_telegram/` | Service-specific media caches |
| `$XDG_RUNTIME_DIR/omachat/daemon.sock` | Private plugin/helper control socket |

Before uninstalling, select **Unpair this desktop** on each connected service
tab. This clears that service's local session and cached content. Follow any
cleanup or remote-revocation warning, and check the phone's linked-device list
for each service. Then run:

```bash
omarchy plugin remove onelegdave.omachat
```

Uninstalling does not erase account data. If you want to remove all remaining
OmaChat data, including API keys, delete `~/.local/share/omachat/` and
`~/.cache/omachat/` yourself after unpairing. This affects all three services.

## Development and documentation

The QML UI uses Omarchy's `qs.Ui` and `qs.Commons` components. Protocol work
runs in the shell-owned Go helper; media capture runs in external processes.

```bash
make helper
make test
make lint
make validate
make test-ui
```

See [Contributing](CONTRIBUTING.md) for prerequisites and verification,
[Security](SECURITY.md) for reporting, and the [documentation index](docs/README.md)
for service guides, release notes, and historical reviews.

## Credits and license

Created and maintained by [OneLegDave](https://www.onelegdave.dev/).
Codex contributes as an AI development lead under OneLegDave's direction,
with AI assistance from Antigravity and Grok on project development and review.

The helper is adapted from [Marc Ford's gmessages-omarchy-plugin](https://github.com/MarcFord/gmessages-omarchy-plugin).
Google Messages uses [mautrix libgm](https://github.com/mautrix/gmessages),
WhatsApp uses [whatsmeow](https://github.com/tulir/whatsmeow), and Telegram uses
[gotd/td](https://github.com/gotd/td).

MIT for OmaChat. Preserve upstream licenses and notices, including vendored
whatsmeow's MPL-2.0 license. See [LICENSE](LICENSE) and [NOTICE](NOTICE).

Other plugins: [OmaDroid](https://github.com/onelegdave/omadroid) and
[System QuikView](https://github.com/onelegdave/system-quikview).
