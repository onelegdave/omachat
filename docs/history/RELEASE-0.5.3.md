# OmaChat v0.5.3 candidate

This patch closes the attachment-download limit bypass reported during
[Marketplace review by HANCORE-linux](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6122021629).
The candidate is prepared locally. Publication and the Marketplace request
update are awaiting the maintainer's decision.

## Changes

- WhatsApp and encrypted Messenger downloads enforce the 25 MiB limit while
  writing, including the optimized copy route used by the protocol library.
- Retry overwrites, in-place decryption, positional writes, and truncation remain
  supported within the limit. Negative and overflowing offsets are refused.
- Messenger profile images receive the same protection at their existing 5 MiB
  limit. Oversized old attachment-cache files are refused without deletion.
- Bounded download failures do not publish a partial file. Vendored hash,
  decryption, and cancellation behavior remains intact.

See [download safety and tests](../media-download-safety.md).

The clipboard corrections from v0.5.1 and v0.5.2, contributor features from
v0.5.0, vendored dependencies, account storage, and dependency-installation
choices remain intact. Google Messages live acceptance remains deferred because
the maintainer does not currently use that service.

## Credits

Thank you to HANCORE-linux for identifying the bypass. Andrew Birklid, Alex
Vlissidis, Matthias MB, ozz1ee-dev, Marc Ford, and upstream contributors retain
their existing attribution and original authorship. Codex provided AI assistance
with this correction and verification; historical Claude assistance remains
credited. See [full credits](../../CREDITS.md).

## Verification and limits

The full Go tests, Google Messages client fixtures, race checks, vet, and native
plugin validation pass. The UI gate passes 72 Node tests, 46 Python tests, and
17 isolated headless QML fixtures. Four isolated native Wayland fixtures also
pass: shell, review, unified inbox, and emoji inbox. These exercise the earlier
input, media, unread, history, notification, profile, build, and update behavior.

New Go tests cover the actual vendored download/decrypt core, exact-limit
encrypted data, invalid hashes, understated/absent size metadata, continuous
streams, retry overwrites, cancellation, file-growth methods, and backend
cleanup. Restoring the old copy bypass causes the new regressions to fail.

UI checks use installed Qt 6.11.2, not Qt 6.12.0. The complete native pop-out,
panel-keyboard, and pagination suites were not rerun in this helper-only patch.
Live-provider attachment acceptance and Google Messages remain distinct from
fixture verification. Marketplace approval and publication remain pending.

## Update and Marketplace

This patch changes the Go helper, so installing the final source requires a
fresh helper build and verification of the running revision. Existing paired
accounts and settings are retained; no re-pairing or automatic dependency
installation is needed. No executable is shipped in Git.

The Marketplace request must be updated only after the maintainer approves the
final candidate. Fresh compatibility and baseline results for its exact public
SHA, complete maintainer review, and confirmed catalog publication are required.
Earlier released tags remain immutable.
