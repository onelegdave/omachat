# Marketplace readiness

## v0.5.1 clipboard correction

Marketplace review of v0.5.0 found that Qt's native Wayland paste could buffer
unbounded clipboard data in the shell. v0.5.1 routes every paste through a
bounded reader outside the shell; see [clipboard safety](clipboard-safety.md).
Following the Marketplace rule to correct an existing request rather than open
a duplicate, the open v0.5.0 update request was edited to target the v0.5.1
commit. Fresh exact-commit checks and maintainer approval are required.

## v0.5.0 update preparation

The release includes the reviewed build, unread/read-state, pop-out, restoration,
notification, keyboard/emoji, bounded WhatsApp history/photo, and optional unified
inbox contributions. The All-tab badge summarizes enabled services, so a disabled
service cannot make a connected inbox appear disconnected. Fresh screenshots use
fictional conversations in an isolated mock gallery.

The maintainer reports successful fresh-install use with Google Messages disabled.
Google Messages was not live-tested in this acceptance pass. Model, isolated QML,
and helper tests supplement that feedback without proving every provider or
recovery scenario. See the [release notes](history/RELEASE-0.5.0.md) and
[credits](../CREDITS.md).

Publication of this release requires a new exact-commit Marketplace update review.
Signal and conversation creation are excluded. No installer hook, new systemd unit,
automatic package installation, or executable binary is shipped.

## Current listing and update requirements, checked October 9, 2026

OmaChat is listed at exact snapshot
`dd77f2ca16e7c701851450578fd9ec51b7eba1a2` (0.4.4). The marketplace registry
records publication on September 19, 2026 and a maintainer-reviewed baseline
with no findings and the `package-manager`, `privilege`, `service-management`,
and `installer` capabilities. See the [publication confirmation](https://github.com/omacom/omarchy-plugin-marketplace/issues/7193#issuecomment-5741298865)
and [registry](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/registry.json).
This approval covers that snapshot, not v0.5.0 or v0.5.1.

Future releases use the [newer-upstream verification and publication workflow](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/VERIFICATION.md#promoting-a-plugin-update):
provide the existing plugin ID, repository root, and full SHA of the final public
HEAD. Compatibility validation and the deterministic baseline must complete for
that SHA, followed by explicit marketplace maintainer approval. Keep HEAD fixed
during review. Only confirmed catalog publication completes the update.

The first local contribution batch changes builds and unread handling without
adding runtime dependencies, downloads, credential URLs, or privileged install
actions. Native validation passed locally; an official fresh marketplace scan
and maintainer decision remain required on the eventual public release SHA.
Keep the vendored build, shell-owned helper, allowlisted user-confirmed package
actions, license notices, and fictional-data screenshots intact. New Signal,
history, notification, and installer changes need their own capability review.

### Locally integrated inbox, notifications, and pop-out changes

The October 9 local review preserves Alex Vlissidis's original PR #8 commits
and Andrew Birklid's original PR #7 commits. This batch adds no package or
privileged install action. Desktop alerts use the session D-Bus notification
service; action signals must come from its current unique owner, and alert IDs
are never forwarded to a replacement notification server. Text previews remain
off by default. Cached contact photos can still identify a conversation; this
is disclosed in Settings and the security documentation.

Profile downloads retain Telegram's account epoch, bounded worker count, byte
limit, private cache, and symlink refusal. Pop-out commands target the mapped
window owned by the current shell PID, validate its address, and retire old
layout replies. They do not install a persistent compositor rule. The in-app
build launcher now invokes the staged builder directly, without creating
`bin/` before the completed executable is ready.

Vendored Go/race checks, fixture tests, and native manifest validation are local
evidence. Full native Wayland focus/window behavior, installed-profile/provider
acceptance, and the official marketplace baseline and maintainer decision for
the final public SHA remain release requirements.

### Locally integrated PR #9 extensions

The selected original commits from PR #9 retain Matthias MB's authorship and
source-SHA references. Shared Andrew Birklid/Alex Vlissidis contributions remain
credited to their original authors. The optional unified inbox defaults off,
combines enabled services only, and keeps service-qualified identities, drafts,
notification suppression, and message requests separate.

Keyboard/emoji changes add no dependency installation or runtime metadata
fetch. Emojibase 17.0.0 metadata, source checksums, and MIT/Unicode/CLDR/JoyPixels
non-artwork notices ship locally. The manual generator reads a pinned local
archive and verifies its contents before producing JSON; no downloaded code or
third-party artwork is executed or included.

WhatsApp history adds bounded private SQLite/index storage separate from device
credentials. Local unpair and phone logout close/detach the old history handle
and clear account-specific anchors, requests, and photo state before cleanup.
A new explicit pairing opens a fresh cache. Photo downloads and redirects allow
HTTPS WhatsApp domains only, validate bounded decoded images, and store private
local paths. Fetch errors exclude provider-issued signed URLs. The eventual
manual marketplace review must cover this provider transport and cache behavior
alongside the existing chat/media and notification capabilities.

The alternate local installer and early-bin/cache staging patch from PR #9 are
excluded. Installation remains `omarchy plugin add`, with no installer hook,
automatic package installation, new systemd unit, or tracked executable binary.
Signal and conversation creation remain separate review candidates.

Local test/manifest evidence does not cover the official marketplace baseline,
maintainer approval, native Wayland focus/rendering, or real provider accounts.
The final public full SHA must be held fixed through the fresh compatibility,
baseline, and authorized-maintainer update workflow described above.

The sections below retain historical submission evidence from before approval.

The owner completed the scoped [oldsmaru acceptance check](oldsmaru-checklist.md)
and authorized stable release preparation on September 16, 2026. The owner
submitted OmaChat to the marketplace the same day as
[issue 7193](https://github.com/omacom/omarchy-plugin-marketplace/issues/7193).

On September 17, 2026 the maintainer's security review added `needs-fixes` for
one finding: the GIPHY API key traveled as the `api_key` query parameter. GIPHY
offers no header or token form (confirmed with a real key; header variants
return 401). Commit `e2f0550b724a58b54d34e1e8023812b4c521b24c` hardened the
surrounding error and log handling and cited two earlier listings with the same
constraint. On September 18 the maintainer rejected that mitigation: a fixed
error string cannot keep a URL credential out of request metadata or GIPHY-side
access logs, and the review required removing the credentialed integration or
replacing it with one whose credential travels outside the URL. No mainstream
GIF search API meets that bar, so OmaChat 0.4.4 removes GIPHY search entirely:
the daemon endpoints, the stored key, the Settings section, and the picker are
gone, and the helper deletes a leftover `giphyApiKey` from `config.json` on
start. Local GIF attachments are unchanged. Publishing a GitHub release does
not submit, list, approve, or verify the plugin in the marketplace, and neither
does passing automated validation.

## Repository requirements

The [current submission guide](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/SUBMISSION.md)
requires a public repository, a root manifest for one plugin, installation and
removal documentation, license and dependency information, and a unique plugin
ID. OmaChat uses `onelegdave.omachat`. The root `preview.png` and
[theme gallery](screenshots/README.md) use fictional data in actual QML components.

Suggested listing category: **Productivity**. Suggested tags: **bar**, **quickshell**,
**media**. These were submitted as suggestions and have not been confirmed by a
maintainer at the time of submission. The current registry lists Productivity
with the bar, quickshell, and media tags.

## Capabilities that deserve explicit review

- The helper is built locally from this repository's vendored Go source. There
  is no downloaded executable, automatic toolchain installation, installer hook,
  or systemd unit.
- Settings can open a terminal for an allowlisted package installation only
  after an explicit user action. The terminal asks for consent before `sudo`,
  and pacman retains its own transaction prompt. This is a privilege and package
  management capability, even though it is optional.
- Google pairing reads browser cookies and may refresh them while paired.
  All services retain credentials and chat/media caches locally. These are
  sensitive capabilities, not sandboxed or isolated from other same-user code.
- Vendored generated Telegram files contain setup-related names, and an upstream
  development Makefile contains tool-install commands. These can trigger static
  capability review even though OmaChat does not execute those development targets.
- Disabling services retains credentials; unpairing and deleting stored data are
  separate, documented actions.

## Verification evidence and limits

### Historical submission before approval, commit `dd77f2c`

The results here are bound to `dd77f2ca16e7c701851450578fd9ec51b7eba1a2`,
OmaChat 0.4.4 with GIPHY search removed, resubmitted on September 18, 2026.
Marketplace validation passed all five checks at that commit, and the security
baseline completed at 2026-09-18T23:43:48Z with `findings: []`,
`outcome: review-required`, and the four expected capabilities
(`package-manager`, `privilege`, `service-management`, `installer`). The
`needs-fixes` label was removed; `validated` and `security-review-required`
remain, and the manual maintainer review of this snapshot is pending.

### Superseded: commit `e2f0550`

Marketplace validation passed at `e2f0550` (OmaChat 0.4.3) and the security
baseline completed at 2026-09-18T08:07:43Z with `findings: []`,
`outcome: review-required`, and the same four capabilities as `d102ae3`. The
manual review then rejected the GIPHY mitigation as described above.

### Previous snapshot, commit `d102ae3`

The results below are bound to `d102ae393902412995b27cd2d802dcb4514d5e24`, the
default-branch HEAD at the time of checking. They describe that exact snapshot
and nothing later.

Marketplace validation passed on all five checks: public and reachable
repository, one valid uniquely identified manifest, root README and license,
Quattro compatibility, and an optional root preview. It identified
`onelegdave.omachat` at OmaChat 0.4.3.

The security baseline completed at 2026-09-17T04:28:46Z with `findings: []` and
`outcome: review-required`, in `selective` enforcement mode. The outcome follows
from four declared capabilities, `package-manager`, `privilege`,
`service-management`, and `installer`, and not from any defect. An empty
findings list is not an approval and `review-required` is not a clean security
certification.

**A moved default branch invalidates a pending submission.** On September 16 a
maintainer rejected the submission because validation and the baseline were
bound to `0f4f2a264396676a3036a532b416e0150b51aede` while the default-branch
HEAD had advanced to `d102ae3` through an intentional documentation cleanup. No
plugin code had changed, and it was still refused. The owner edited the issue to
retrigger both checks; the first retrigger failed with
`security-baseline-unavailable` and the second succeeded. While a submission is
pending, do not push to the default branch, including documentation-only
commits.

### Superseded: September 14, 2026

On that date the marketplace's compatibility inspector accepted v0.3.4
(`55e099e12480ff94456ce50f6b929d732947c052`). Its static baseline returned
`review-required` with no findings and the `installer`, `package-manager`, and
`privilege` capabilities. The marketplace tooling checkout used was
`73498d58716438c466617846cf040ff289463392`. This record is retained for history
only; `service-management` was added to the declared capabilities by the time of
the current submission.

### Dependency scan, September 14, 2026

This scan has not been rerun against `d102ae3`. An OSV query of the 53 vendored
module versions returned
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), which concerns the unmaintained
`golang.org/x/crypto/openpgp` packages. Those packages are absent from the vendor
tree and from `go list -mod=vendor -deps ./cmd/omachatd`. This module-level query
does not cover every potential vulnerability, the Go standard library, native
libraries, or the user's installed tools. `govulncheck` was not installed or run.

### Standing limits

The [marketplace security policy](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/SECURITY.md)
requires exact-commit checks and explicit maintainer approval. Capability review
is expected. Local tests, static checks, and this focused source review do not
guarantee security, approval, or error-free operation. Do not suppress genuine
capabilities or describe `review-required` as a clean security certification.

Before any future submission or update verification, rerun the repository test
suite and exact-commit marketplace checks, inspect the public screenshots and
release metadata, and obtain the owner's explicit approval of the completed
submission body.
