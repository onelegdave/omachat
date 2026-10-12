# OmaChat v0.5.3

This release closes an attachment-download limit bypass found during
[Marketplace review by HANCORE-linux](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6122021629).

## Changes

- WhatsApp and encrypted Messenger downloads enforce the 25 MiB limit while
  writing, including the optimized copy route used by the protocol library.
  Previously, an attachment with understated size metadata could write past
  the limit before hash verification and fill the disk.
- Retry overwrites, in-place decryption, positional writes, and truncation remain
  supported within the limit. Negative and overflowing offsets are refused.
- Messenger profile images receive the same protection at their existing 5 MiB
  limit. Oversized old attachment-cache files are refused without deletion.
- Bounded download failures do not publish a partial file. Vendored hash,
  decryption, and cancellation behavior remains intact.

See [download safety and tests](../media-download-safety.md).

The clipboard corrections from v0.5.1 and v0.5.2, contributor features from
v0.5.0, vendored dependencies, account storage, and dependency-installation
choices are unchanged. No QML or JavaScript changed in this release.

## Credits

Thank you to HANCORE-linux for identifying the bypass. Andrew Birklid, Alex
Vlissidis, Matthias MB, ozz1ee-dev, Marc Ford, and upstream contributors retain
their existing attribution and original authorship. Codex provided the
correction; Codex and Claude provided reviews and verification. See
[full credits](../../CREDITS.md).

## Verification and limits

The full Go tests, Google Messages client fixtures, race checks, vet, and native
plugin validation pass. The UI gate passes 72 Node tests, 46 Python tests, and
17 isolated headless QML fixtures. Four isolated native Wayland fixtures also
pass: shell, review, unified inbox, and emoji inbox.

New Go tests run the actual vendored download and decryption code against an
offline fixture server. They cover exact-limit encrypted data, invalid hashes,
understated or absent size metadata, continuous streams, retry overwrites,
cancellation, file-growth methods, and backend cleanup. Restoring the old copy
bypass makes the new regressions fail.

Live WhatsApp and Messenger attachment downloads were not part of this
verification, and Google Messages live acceptance remains deferred. UI checks
use installed Qt 6.11.2, not Qt 6.12.0.

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
