# OmaChat v0.5.4

This release keeps conversation names and message text out of desktop
notifications, following
[Marketplace review by HANCORE-linux](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6128565247).

## Removed: notification previews

**What was removed.** The optional **Show sender and message previews** setting
is gone. It was off by default; when it was on, alerts showed the conversation
name and the start of the message. Alerts now always read "OmaChat · *service*"
and "New message". Contact photos still appear as the alert icon, and clicking
an alert still opens its conversation.

**Why.** Omarchy's notification service saves each alert to its history by
starting a `bash` command with the whole notification, text included, as a
command-line argument. Any account on the same computer, including system
service accounts, can normally read other processes' command lines through
`/proc`, so previews could be read by someone other than you. OmaChat cannot
control what a notification service does with alert text, so it no longer sends
names or message text at all.

**If you still want previews.** The
[`notification-previews`](https://github.com/onelegdave/omachat/tree/notification-previews)
branch is this release with the setting restored, still off by default. It is
not part of the Marketplace listing or its review. Use it only if you accept
that other accounts on your computer may be able to read your message previews.
The [README](https://github.com/onelegdave/omachat#notification-previews-removed-in-054)
explains how to switch to it, update it, and switch back.

## Changes

- Alerts send only the service name and "New message" to the notification
  service. The panel no longer sends conversation names to the helper for
  alerts.
- If you had previews on, they stop when you rebuild the helper or the shell
  next restarts. After a shell restart, the panel tells a helper that has not
  been rebuilt to turn them off before it sends any alert. The rebuilt helper
  deletes the saved setting.

> **Correction:** these notes first said previews stop before the helper is
> rebuilt. OmaChat's panel service stays loaded across plugin updates, so the
> new panel code only runs after a shell restart. Rebuilding the helper on
> v0.5.4 without restarting the shell also stopped all notifications until the
> next restart. [v0.5.5](RELEASE-0.5.5.md) fixes that; use v0.5.5 instead.
- The pairing QR link now reaches `qrencode` on standard input instead of its
  command line.

Clipboard protections from v0.5.1 and v0.5.2, attachment limits from v0.5.3,
contributor features from v0.5.0, vendored dependencies, account storage, and
dependency-installation choices are unchanged.

## Credits

Thank you to HANCORE-linux for identifying the notification exposure. Andrew
Birklid, Alex Vlissidis, Matthias MB, ozz1ee-dev, Marc Ford, and upstream
contributors retain their existing attribution and original authorship. Claude
provided the correction and verification. See [full credits](../../CREDITS.md).

## Verification and limits

The full Go tests, Google Messages client fixtures, race checks, vet, and native
plugin validation pass. The UI gate passes 73 Node tests, 46 Python tests, and
18 isolated headless QML fixtures, and the native notification fixture passes.
New tests cover:

- Generic alert text for every service, even when the panel sends a name and
  message text.
- Deletion of the saved preview setting.
- An outdated helper with previews on: no alerts until it confirms previews
  are off.
- `qrencode` receiving the exact pairing link on standard input, with nothing
  from the link in its arguments.

Live desktop alerts from real conversations were not part of this verification.
UI checks use installed Qt 6.11.2.

## Update and Marketplace

```sh
omarchy plugin update onelegdave.omachat
```

This release changes the Go helper. Rebuild it with the panel's **Build helper**
action when prompted. Existing paired accounts and settings are retained; no
re-pairing or automatic dependency installation is needed. No executable is
shipped in Git.

The open Marketplace update request now targets this release commit. Fresh
exact-commit checks and a complete maintainer review are required; the existing
verified snapshot remains in place until the Marketplace publishes the update.
