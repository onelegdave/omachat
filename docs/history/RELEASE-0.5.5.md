# OmaChat v0.5.5

This release keeps desktop notifications working while you update to the
notification privacy change from [v0.5.4](RELEASE-0.5.4.md). It includes
everything in v0.5.4.

## Fixed

OmaChat's background service in the shell stays loaded across plugin updates
and keeps running the previous release's code until the shell restarts. That
code ignores a helper configuration without the old notification preview
setting. On v0.5.4, rebuilding the helper after updating, without restarting
the shell, therefore stopped all notifications until the next restart. The
helper now always reports previews as off, so the old code keeps working until
the shell restarts.

The documentation now says when previews stop after updating: when you rebuild
the helper, which the panel asks you to do, or when the shell next restarts. The
[README](https://github.com/onelegdave/omachat#notification-previews-removed-in-054)
also adds a shell restart to the steps for switching to or from the
`notification-previews` branch.

## Notification previews (removed in v0.5.4)

Alerts read only "OmaChat · *service*" and "New message". The optional sender
and message preview setting was removed because Omarchy's notification history
passes alert text on a `bash` command line that other local accounts can read
through `/proc`. The README explains what was removed, why, and how to use the
unlisted `notification-previews` branch if you accept that risk.

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
A new Go test checks that the helper's configuration keeps the fields earlier
panel services require. The v0.5.3 panel code was also run against the
helper's configuration: it rejects the v0.5.4 configuration and accepts v0.5.5.

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

The open Marketplace update request now targets this release commit, replacing
v0.5.4. Fresh exact-commit checks and a complete maintainer review are required;
the existing verified snapshot remains in place until the Marketplace publishes
the update.
