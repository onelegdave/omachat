# Dependencies and user choice

[OmaChat](../README.md) never installs dependencies automatically. Settings
includes read-only availability checks with **Review source**, **Install**, and **Recheck**
actions. Install is offered for missing tools and opens a terminal, showing the
exact command before asking for confirmation. Pacman then shows its transaction
and asks for confirmation too. Cancelling either prompt installs nothing.

Only fixed, known Arch package names can be passed to this action. It does not
run install hooks, download scripts, use AUR helpers, refresh package databases,
or upgrade your system. You can always install tools manually instead. Source
buttons open the [Arch packaging repositories](https://wiki.archlinux.org/title/Arch_Build_System),
where PKGBUILDs link to upstream source. No package source is executed by reviewing it.

Choose **Recheck** after installation. Available means the required executable
was found (and Go meets the minimum version), not that accounts, devices, portals,
or keyrings are configured. Python 3 is needed to run the checklist; if unavailable,
the app reports a check failure and you can install Python manually.

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
The first build can take several minutes and may produce no output while it
works. Do not restart the Omarchy shell during compilation. The helper starts
automatically when the build succeeds; failures appear in the panel.
After installing Go, choose **Retry** on the helper screen to recheck it.

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

Desktop packages vary. Accounts, API credentials, pairing, and optional features
are always the user's choice. A missing tool is not a reason to install every
listed package. These actions do not enable unsupported protocol features.
