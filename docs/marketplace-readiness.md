# Marketplace readiness

## v0.6.0 update

v0.6.0 is one update covering the remote-content correction below and new
features. The correction was prepared as v0.5.6, which was never published.

### Remote-content correction

Review of v0.5.5 [found](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6137173525)
that an incoming WhatsApp `image/png` document was decoded by Qt inside the
shell, where compressed PNG text chunks are expanded with no aggregate budget.
v0.6.0 makes the helper re-encode every image the panel displays, on every
service, including profile photos, notification icons, and composer
thumbnails. An independent audit of the other routes from remote content into
the shell led to plain-text labels, external video playback, copying through
standard input, bounded panel models and helper replies, withheld Messenger
disappearing messages, and allowlisted cache suffixes. See
[remote content and the shell](shell-content-safety.md).

### New capabilities to review

- **Optional Signal service.** Off by default. When enabled it runs the
  separately installed, GPL-3.0 `signal-cli` as a child of the shell-owned
  helper, over JSON-RPC on standard input and output. Nothing is bundled.
  `signal-cli` stores linked-device keys under OmaChat's private data folder.
  Signal media follows the same preview, download-cap, and suffix rules as the
  other services, and OmaChat deletes signal-cli's saved copies of deleted,
  expired, view-once, and trimmed media.
- **AUR install action.** If `signal-cli` is missing, an explicit user action
  opens a terminal that shows the fixed `yay -S --needed signal-cli` command
  and asks before running it; yay then shows its own transaction prompt. A
  shell restart afterwards is also asked, not automatic. This extends the
  declared `package-manager` and `privilege` capabilities to the AUR.
- **Conversation creation.** Group creation sends a request through each
  service's own protocol client and can invite or notify the chosen contacts.
  It runs only when the user submits the picker.
- **WhatsApp status filter.** Status and story broadcasts are no longer stored
  as chats; this reduces stored remote content.

No installer hook, systemd unit, downloaded executable, or automatic package
installation is added. The helper is still built locally from vendored source.
If no folder outside the plugin shares the helper's filesystem, the in-panel
build now refuses before writing anything in the plugin rather than copying
the helper in.

### Review corrections, October 18, 2026

- **Previous helper after an update.** Until it is rebuilt, a v0.5.x helper
  sends original downloaded profile photos and uses them as notification
  icons, so the same PNG text-chunk route existed through avatars. A current
  helper now reports `contentProtocol` in its build information, and the panel
  requests and accepts conversations, messages, media, pairing, and alerts
  only after the helper on the current connection confirms it. Missing,
  malformed, failed, unanswered, and earlier-connection replies keep content
  closed, and the check does not rely on identifying the installed source.
  See [remote content and the shell](shell-content-safety.md).
- **Go 1.27.2 minimum.** [GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611)
  (CVE-2026-78669, fixed in [Go 1.27.2](https://go.dev/doc/devel/release#go1.27.2))
  lets a malicious HTTP/2 peer drive up CPU in a client or server. The helper's
  standard HTTP clients speak HTTP/2 to Google, WhatsApp, and Meta servers, so
  the advisory applies, although exploiting it requires one of those servers or
  a certificate the system trusts. Fetches of sender-chosen URLs (group avatars,
  Messenger media) use custom dialers, which keep Go on HTTP/1.1. `go.mod`, the
  dependency checklist, and the build all require Go 1.27.2 with
  `GOTOOLCHAIN=local`, so an older Go fails instead of downloading a toolchain.
  Go 1.27.2 also fixes every other standard-library advisory in the scan below.
- **Dependency scan.** An OSV query of the 71 vendored module versions and Go
  1.27.0 returned 15 advisories. Thirteen are standard library, all fixed in
  Go 1.27.2. GO-2026-5676 (quic-go HTTP/3) and GO-2026-5932 (`x/crypto/openpgp`)
  name packages the helper does not import. The vendored `golang.org/x/net`
  0.58.0 `http2` package matches five HTTP/2 advisories fixed in 0.60.0, but
  the helper uses it only to recognize `http2.StreamError`; its transports are
  the standard library's, and the helper runs no HTTP server. This is a version
  and import review, not a `govulncheck` call-graph result.

Local evidence for v0.6.0 (October 18, 2026) includes the full native Wayland
QML suite with mock services and isolated data, alongside the Go, Python, Node,
and headless QML checks listed in the
[release notes](history/RELEASE-0.6.0.md#verification-and-limits). They ran
with Go 1.27.2, Qt 6.12.0, and Quickshell 0.3.2 on the release's own `go.mod`.
That run also found that Qt 6.12's new `QtQuick` `Color` singleton hid the
Omarchy theme's `Color` from OmaChat's unqualified uses; OmaChat now qualifies
it as Omarchy's own shell does. The evidence does not cover real provider
accounts, a real `signal-cli`, an update of an installed copy, Qt 6.12's
clipboard-change handler with a real clipboard, or the official baseline and
maintainer review of the final SHA.

## v0.5.5 update compatibility correction

Local verification after publishing v0.5.4 found that OmaChat's `keepLoaded`
panel service keeps running the previous release's code until the shell
restarts. That code ignores a config without `notificationPreviews`, so
rebuilding the helper on v0.5.4 without a shell restart stopped all
notifications. v0.5.5 makes the helper always report the field as `false`, and
a test pins this contract. It also corrects the v0.5.4 documentation: previews
stop when the helper is rebuilt or the shell restarts, and switching to or from
the `notification-previews` branch needs a shell restart. The v0.5.4 commit is
superseded; the same open update request was edited to target v0.5.5.

## v0.5.4 notification-text correction

Review of v0.5.3 [found](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6128565247)
that optional message previews sent conversation names and message text to
Omarchy's notification service, whose history writer passes them to `bash` as
a command-line argument readable through `/proc`. v0.5.4 removes the preview
setting: alerts always read "OmaChat · *service*" and "New message", the panel
no longer sends conversation names, an outdated helper is told to turn previews
off before any alert is sent, and the rebuilt helper deletes the saved setting.
The pairing link also moved from `qrencode`'s arguments to its standard input.
See the [release notes](history/RELEASE-0.5.4.md). The README documents the
removal and an unlisted `notification-previews` branch for users who accept the
risk; that branch is not part of this request. The same open update request was
edited again to target the v0.5.4 commit. Fresh exact-commit checks and a
complete maintainer review are required.

## v0.5.3 attachment-limit correction

Review of v0.5.2 [found](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6122021629)
a write-limit bypass in WhatsApp and encrypted Messenger downloads. v0.5.3
replaces both file-embedding wrappers with a shared bounded file interface,
covers all copy/write/growth routes, and preserves normal decryption and
retries. See [download safety](media-download-safety.md) and the
[release notes](history/RELEASE-0.5.3.md). The same open update request was
edited again to target the v0.5.3 commit. Fresh exact-commit checks and a
complete maintainer review are required.

## v0.5.2 clipboard-change correction

Review of v0.5.1 found that Qt 6.12.0 and earlier read the whole clipboard
into every editable TextInput when the clipboard changes, without a paste
(QTBUG-149610, fixed for Qt 6.12.1). v0.5.2 edits with a plain-text TextEdit
and keeps the API hash in a read-only TextInput; see
[clipboard safety](clipboard-safety.md#clipboard-change-reads). The same open
update request was edited again to target the v0.5.2 commit. Fresh
exact-commit checks and a complete maintainer review are required.

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
Signal and conversation creation were excluded from v0.5.0; v0.6.0 includes them. No installer hook, new systemd unit,
automatic package installation, or executable binary is shipped.

## Current listing and update requirements, checked October 9, 2026

OmaChat is listed at exact snapshot
`dd77f2ca16e7c701851450578fd9ec51b7eba1a2` (0.4.4). The marketplace registry
records publication on September 19, 2026 and a maintainer-reviewed baseline
with no findings and the `package-manager`, `privilege`, `service-management`,
and `installer` capabilities. See the [publication confirmation](https://github.com/omacom/omarchy-plugin-marketplace/issues/7193#issuecomment-5741298865)
and [registry](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/registry.json).
This approval covers that snapshot, not v0.5.0 through v0.6.0.

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
are never forwarded to a replacement notification server. Text previews were
off by default; v0.5.4 removed them. Cached contact photos can still identify a conversation; this
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
At the October 9 checkpoint, Signal and conversation creation were separate
review candidates. They are included in the combined v0.6.0 update above.

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
ID. OmaChat uses `onelegdave.omachat`. The root `preview.png` is a promotional
overview: generated artwork around a capture of the actual QML panel, with
fictional data and simulated states, labeled as such in the README. The
[theme gallery](screenshots/README.md) keeps plain captures of the actual QML
components with fictional data.

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
