# Dependencies and user choice

[OmaChat](../README.md) never installs, updates, or downloads dependencies for
you. Missing requirements are described in the panel or reported when you
request the feature. You decide whether to install tools using your own package
manager. The plugin does not run package-manager commands, sudo, or install hooks.

## Shared requirements

An Omarchy desktop with its Quickshell plugin system provides the native UI.
All three services share a locally compiled helper:

| Requirement | Purpose | Your choice |
| --- | --- | --- |
| Go 1.27.0 or newer (see `go.mod`) | Compile the helper from `vendor/` | Install Go yourself if you want to build and use OmaChat |
| C compiler (`gcc` or `clang`) | CGO SQLite in the shared helper | Required at build time for every service |
| `qrencode` | Render pairing QR codes | Needed for WhatsApp/Telegram QR pairing and Google's QR fallback |

Example build-tool installation, only if you choose to run it:

```bash
omarchy pkg add go gcc
```

Then choose **Build helper**. Source dependencies are already in `vendor/`;
the button compiles them with module downloads disabled. It does not install a
toolchain. A build error can also indicate an incompatible toolchain or source
problem; read the displayed build log before retrying.

## Service and feature requirements

| Service or feature | Requirement |
| --- | --- |
| Google browser pairing | Supported Chromium-family browser signed in at Messages for web; `sqlite3` and `secret-tool` (libsecret) for cookie access; unlocked desktop keyring |
| WhatsApp pairing | WhatsApp phone app with Linked devices; `qrencode` |
| Telegram setup | Personal `api_id` and `api_hash`; Python 3 for `scripts/configure-telegram.py`; Telegram phone app and `qrencode` |
| Google/Telegram voice notes | `ffmpeg` to record and `ffplay` to play audio |
| Webcam photo capture | `ffmpeg` and an accessible camera device |
| File selection | Working desktop portal and its file-picker backend |
| Copy message | `wl-copy` from wl-clipboard |
| Open links or downloaded files | `xdg-open` and an appropriate installed browser/image/video application |
| Google GIPHY search | Your own GIPHY API key entered in Settings |

For example, if you want voice notes, you can run `omarchy pkg add ffmpeg`
yourself. Text and photos do not need voice tools. WhatsApp voice notes remain
unavailable even when those tools are installed. Installing a tool does not
enable an unsupported service feature.

Desktop packages vary. OmaChat uses available tools without installing or
updating them. Accounts, API credentials, pairing, and optional features are
always the user's choice.
