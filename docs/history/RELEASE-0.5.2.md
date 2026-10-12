# OmaChat v0.5.2

This release completes the clipboard fix from v0.5.1. Marketplace review found
that Qt itself still read the clipboard in the background, without any paste.
Thank you to
[HANCORE-linux for the report](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6106501652)
and for [suggesting the approach](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6117859574).

## Highlights

- Text fields no longer read the clipboard when it changes. Through Qt 6.12.0,
  every editable Qt text field read the whole clipboard on each change to
  decide whether paste was possible
  ([QTBUG-149610](https://qt-project.atlassian.net/browse/QTBUG-149610), fixed
  for Qt 6.12.1). OmaChat's fields now use an editor that only checks which
  formats are offered.
- Fields look and behave as before: Return sends or confirms, Tab moves focus,
  long text scrolls, length limits apply while typing, and input methods are
  still supported.
- The Telegram API hash field is a read-only masked field that handles typing
  itself, so it skips the background read too.
- Every paste still goes through the bounded reader from v0.5.1: 64 KiB and
  1.5 seconds at most.

See [clipboard safety](../clipboard-safety.md#clipboard-change-reads) for the
Qt details and test coverage.

## Credits

HANCORE-linux is credited for the report and the suggested approach.
Contributor credits from v0.5.0, Marc Ford's MIT attribution, and vendored
notices are unchanged. Codex and Claude provided the fix, reviews, and
verification. See [full credits](../../CREDITS.md).

## Verification and limits

Go, race, vet, and plugin validation checks pass, along with 72 Node tests, 46
Python tests, and 17 isolated headless QML fixtures. A new fixture covers field
behavior and checks that no OmaChat field contains an editable TextInput. On a
live Hyprland desktop with Qt 6.11.2, an 8 MB clipboard change made a plain Qt
text field read 8,000,208 bytes, while OmaChat's text field read 200 and the
password field 24. That probe used a large finished transfer, not an endless
stream, and did not run on Qt 6.12.0, whose handler code is the same.

## Update and marketplace

```sh
omarchy plugin update onelegdave.omachat
```

Rebuild the helper with the panel's **Build helper** action if prompted. The
helper code is unchanged, settings and paired credentials are retained, and no
re-pairing is needed. No dependency is installed automatically and no
executable is shipped in Git.

The open Marketplace update request now targets this release commit. Fresh
exact-commit checks and a complete maintainer review are required; the existing
verified snapshot remains in place until the Marketplace publishes the update.
