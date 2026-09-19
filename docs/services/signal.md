# Signal setup and current scope

OmaChat's Signal integration is an unofficial linked-device client built on the
separately installed [signal-cli](https://github.com/AsamK/signal-cli). Signal,
signal-cli, and OmaChat are independent projects. Signal support is opt-in and
does not start a process until Signal is enabled in Settings > Services.

## Setup

1. Install a current `signal-cli` release by following its upstream README. On
   Arch/Omarchy, the community AUR package can be installed with
   `yay -S --needed signal-cli`; review its PKGBUILD and transaction before confirming.
2. Verify `signal-cli --version` succeeds in a terminal.
3. In OmaChat, open Settings > Services, enable Signal, and apply the change.
   If `signal-cli` is missing, OmaChat asks before opening a terminal installer.
   Canceling that prompt leaves Signal disabled and installs nothing.
4. Open the Signal tab and select **Link Signal**.
5. On the primary phone, open Signal > Settings > Linked devices > Link a new
   device and scan OmaChat's QR code.

OmaChat launches `signal-cli --data-dir ~/.local/share/omachat/signal-cli
--output=json jsonRpc` as a child of the shared helper. Disabling Signal stops
the child and hides the tab without deleting keys or local messages.

## First milestone

Implemented: optional service lifecycle, QR provisioning, direct and group text
send/receive, contact discovery, and a locally accumulated conversation store.

Not implemented yet: phone-history import, attachments, reactions, upstream
read receipts, typing indicators, calls, disappearing-message expiry,
safety-number UI, complete group metadata, or in-app device revocation.

Use the phone's Linked devices screen to revoke OmaChat. Only after revocation,
remove `~/.local/share/omachat/signal-cli/` if you also want to delete the local
device keys. OmaChat's local inbox is
`~/.local/share/omachat/signal_store.json`.

## Security and maintenance

`signal-cli` decrypts messages locally and stores Signal device keys on disk.
OmaChat's cache is also local and is not an encrypted archive. Use full-disk
encryption and normal user-account protections where local confidentiality
matters. Keep `signal-cli` current as Signal's service changes, and do not use
this integration for bulk or automated messaging.
