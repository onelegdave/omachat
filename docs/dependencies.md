# Dependencies and user choice

[OmaChat](../README.md) never installs dependencies automatically. Settings
includes read-only availability checks with **Review source**, **Install**, and **Recheck**
actions. Install is offered for missing tools and opens a terminal, showing the
exact command before asking for confirmation. Pacman—or Yay for the optional
Signal AUR package—then shows its transaction and asks for confirmation too.
Cancelling either prompt installs nothing.

Only fixed, known Arch package names can be passed to this action. It does not
run arbitrary commands, accept user-supplied package names, refresh package
databases, or upgrade your system. The Signal action uses Yay for the fixed
`signal-cli` AUR package; all other actions use Pacman with fixed official Arch
package names. You can always install tools manually instead. Source buttons
open the Arch or AUR package page, where the packaging recipe and upstream
source can be reviewed. No package source is executed by reviewing it.

Choose **Recheck** after installation. Available means the required executable
was found (and Go meets the minimum version), not that accounts, devices, portals,
or keyrings are configured. Python 3 is needed to run the checklist; if unavailable,
the app reports a check failure and you can install Python manually.

## Shared requirements

An Omarchy desktop with its Quickshell plugin system provides the native UI.
All five services share a locally compiled helper:

| Requirement | Purpose | Your choice |
| --- | --- | --- |
| Go 1.27.0 or newer (see `go.mod`) | Compile the helper from `vendor/` | Install Go yourself if you want to build and use OmaChat |
| Python 3 | Offline helper builds, source checks, release checks, and setup tools | Required for the in-app build and update workflow |
| C compiler (`gcc` or `clang`) | CGO SQLite in the shared helper | Required at build time for every service |
| `qrencode` | Render pairing QR codes | Needed for WhatsApp/Telegram QR pairing and Google's QR fallback |

Example build-tool installation, only if you choose to run it:

```bash
omarchy pkg add go gcc python
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
| Signal | Current `signal-cli` executable; Signal phone app and `qrencode` for linked-device pairing |
| Voice notes on all services | `ffmpeg` to record and `ffplay` to play audio |
| WhatsApp GIF sending | `ffmpeg` to convert GIF files to WhatsApp's MP4 playback format |
| File selection | Working desktop portal and its file-picker backend |
| Copy message | `wl-copy` from wl-clipboard |
| Open links or downloaded files | `xdg-open` and an appropriate installed browser/image/video application |

For example, if you want voice notes, you can run `omarchy pkg add ffmpeg`
yourself. Text and photos do not need voice tools. Installing a tool does not
enable an unsupported service feature.

### Signal's optional service

Signal support uses the independent [signal-cli](https://github.com/AsamK/signal-cli)
project through its JSON-RPC interface. It is not an official Signal client and
is not bundled with OmaChat. When Signal is enabled and `signal-cli` is missing,
OmaChat first asks whether to install it. Choosing **Cancel** leaves Signal
disabled and runs nothing. Choosing **Install and enable** opens a terminal;
review and confirm the fixed `yay -S --needed signal-cli` command there. The
package manager still asks before changing the system. You may instead install
a current release using the upstream instructions, confirm
`signal-cli --version` works, then enable Signal
under Settings > Services. OmaChat starts `signal-cli` as its own child process;
disabling Signal stops that process while retaining the linked-device keys.

Select **Link Signal**, then scan the QR code from Signal on your phone under
Settings > Linked devices > Link a new device. OmaChat stores chat history
locally from the time it is linked; it does not promise an import of the
phone's earlier history. Remove the OmaChat device from the phone before
deleting `~/.local/share/omachat/signal-cli/`.

Keep `signal-cli` updated. Its maintainers warn that older releases can stop
working when Signal's service changes. Signal's own linked-device limits and
inactivity rules also apply.

Desktop packages vary. Accounts, API credentials, pairing, and optional features
are always the user's choice. A missing tool is not a reason to install every
listed package. These actions do not enable unsupported protocol features.
