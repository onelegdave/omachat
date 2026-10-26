# Release handoff: OmaChat 0.6.2

Prepared October 26, 2026 from public v0.6.1 at
`ba115ba4140dd7cccd38281320edb7c5ec462612`. This candidate corrects the
[October 25 WebSocket finding](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6403016904)
with a 32 MiB decoded-message limit and production-socket regression tests.
See [WebSocket safety](messenger-websocket-safety.md) and the
[release notes](history/RELEASE-0.6.2.md).

Current state: implementation and release verification pass. The full Go
suite, vendored regression suites, `go vet ./...`, 18 socket cases under the
race detector, 77 Node tests, 63 Python tests, and all 25 native QML fixture
markers passed on October 26, 2026. Plugin validation and the offline helper
build passed; the built helper reports `0.6.2`. The overflow tests fail with
the original unlimited reader. Runtime: installed Go 1.27.3 on Linux, using
vendored dependencies and isolated test data. No live provider account has
been used. The public branch and Marketplace request remain on v0.6.1.

Exact next work: obtain publication approval, then publish the final commit,
v0.6.2 tag and release.
Retarget the existing request #10861 and reply to the latest finding with the
new exact commit and evidence. Keep main fixed during renewed review.

## Previous v0.6.1 handoff (historical)

v0.6.1 contains the decoded Messenger HTTP response limit described in
[release notes](history/RELEASE-0.6.1.md) and [response safety](messenger-response-safety.md).
It builds on public `main` at `fa06f4e1cd222494662e707b87056680ea42e08b`.
Codex implemented the fix and Claude independently reviewed it with no blocking
findings; all optional review suggestions were addressed.

Publish the final release commit to `main`, create a new immutable `v0.6.1`
tag and stable GitHub release, then edit the existing Marketplace
[request #10861](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861)
to that exact 40-character HEAD SHA. Reply to the
[Messenger response finding](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6314053857)
with implementation and verification evidence. Keep `main` fixed during review.
Fresh compatibility validation, the Automated Security Baseline, an authorized
maintainer's `approved-and-verified` decision, and catalog deployment are
separate from the GitHub release. The linked public records show their current
status; no old approval applies to the new commit.

Users must rebuild the helper after updating. No session migration or re-pairing
is introduced. Live provider accounts and an installed-profile upgrade have
not been exercised for this correction; older evidence below is historical.

## Previous v0.6.0 handoff (historical)

This records the 0.6.0 changes, verification, live acceptance limits, and
publication procedure. The public release and Marketplace request linked
below are the sources for current publication status. Last updated October
18, 2026.

## Release record

- Branch `release/omachat-complete-20261017`, based on `main` at v0.5.5
  (`3dbd975f8a0fbf427bbb7c09179664f02a824e53`). The release candidate is the
  branch tip that contains this file.
- [GitHub release v0.6.0](https://github.com/onelegdave/omachat/releases/tag/v0.6.0)
  records release publication. [Marketplace request #10861](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861)
  records the separate exact-commit review and catalog promotion. A GitHub
  release does not by itself mean Marketplace approval.
- One release, 0.6.0. The remote-content work prepared as v0.5.6 was never
  published and is part of 0.6.0; its notes became
  [RELEASE-0.6.0](history/RELEASE-0.6.0.md).
- `manifest.json` is 0.6.0. The helper takes its version from the manifest at
  build time and identifies its source by the hash in `scripts/updates.py`, so
  the panel asks to rebuild after the update.

## Included work

| Source | What | How it was brought in |
| --- | --- | --- |
| Saved `fix/shell-safe-content` (7 commits from `facb588` to `35eb1af`) | Images re-encoded in the helper, plain-text labels, ffmpeg clip previews, stdin copy, bounded models and replies, Messenger disappearing messages and file suffixes | Already the branch base; audited again, unchanged |
| PR #9, Matthias MB `d09fe48` | WhatsApp status and story broadcasts filtered from live events and history | Cherry-picked with authorship (`c8e88cc`); upgrade cleanup and both-route tests added (`4404d6f`) |
| PR #9, Matthias MB `b04c2e1` | gofmt of two test files | Cherry-picked (`b832195`); its `daemon.go` hunk was already applied |
| PR #4, Andrew Birklid `365156f` to `3aad116` | Optional Signal through signal-cli | Seven commits cherry-picked with authorship; `92c2f6e` was already in `main` |
| Review branch `fix/signal-review` `405b5cc` | Signal lifecycle and cache hardening | Cherry-picked (`cabd299`) |
| PR #6, Andrew Birklid `5e0b65f` | Direct and group conversation creation | Cherry-picked with authorship (`be9d442`) |
| Review branch `fix/new-conversations-review` `13ed7ad` | Request and picker lifetime hardening | Cherry-picked (`050f1c4`) |
| Issue #1 | In-panel build | Existing staging verified; remaining gaps fixed (`d15ed75`); the unsafe cross-filesystem copy it added was replaced by staging on the helper's filesystem or refusing first (review round) |

The rest of PR #9 is not included: its author confirmed only `d09fe48` needed
attention, and its avatar symlink fix is already in `main`. PRs #4 and #6 were
not merged as branches; their commits were applied onto the current code and
adapted in follow-up commits.

Follow-up fixes made during integration:

- Signal joined the 0.5.x rules: previews beside its cache and cleared on
  unpair, desktop alerts, a 32 MiB download cap with a bounded RPC reader,
  `--ignore-stories`, the fixed cache-suffix list, and removal of the helper's
  previews and signal-cli's saved copies for deleted, expired, view-once, and
  trimmed media. An unused Signal writes no inbox file.
- New conversations: the picker uses the clipboard-safe field and plain text,
  its contact list is bounded, Signal chats are keyed by UUID like received
  messages, and Google Messages creates an RCS group only when the phone asks.
- The signal-cli installer asks before restarting the shell.
- A long-standing scroll bug: a jump into older history could snap back to the
  newest message. The pagination fixture caught it; it also failed on v0.5.5.
- Independent review found that the cross-filesystem fallback in `d15ed75`
  copied the helper into `bin` (and created an empty `bin` on a first install)
  before it was complete, so a shell reload could still interrupt the build.
  The fallback is gone. Staging now matches the filesystem of the helper's own
  folder, a cache folder inside the plugin is skipped before it is created
  (symlinks included), and a layout with no suitable folder is refused before
  any plugin write, leaving the old helper in place.
- Text fields: assigned text (restored drafts, returned captions, emoji
  search) left the cursor at the start, so typing went before it. This dated
  from the TextEdit change in 0.5.2. The keyboard fixture caught it.
- Opening the panel with no chat selected focuses the conversation list, as
  documented, and search only when the list is empty.
- Second review (October 18): a 0.6.0 panel trusted whatever helper was
  running. After an update and a shell restart before rebuilding, a v0.5.x
  helper sent original profile photos, which the panel decoded, and turned
  them into notification icons. The helper now reports `contentProtocol` in
  `buildInfo`, and the panel requests and accepts remote content and sends
  alerts only once the helper on the current connection reports it. A
  disconnect or reconnect asks again without showing a rebuild notice in the
  meantime. (The third review round found that an ordinary disconnect still
  showed the notice until the socket reconnected; the stale-helper fixture now
  checks that moment, and it failed before the fix.)
- Third review round, on Qt 6.12.0 and Quickshell 0.3.2: Qt 6.12 exports its
  own `QtQuick` `Color` singleton, which took precedence over Omarchy's
  `qs.Commons` `Color` wherever OmaChat used it unqualified, so every theme
  color read as undefined. The headless readability fixture caught it. Files
  that use the theme's colors now also import `qs.Commons as Commons` and
  write `Commons.Color`, as Omarchy 4.0.5's own shell code does. `Style`,
  `Border`, and `Util` do not collide and are unchanged. The published v0.5.5
  uses the same unqualified `Color`, so by its source it is affected on Qt
  6.12 too (not run).
- Second review: Go 1.27.2 is now the minimum, for
  [GO-2026-6611](https://pkg.go.dev/vuln/GO-2026-6611). `go.mod`, the Tools
  checklist, and the Makefile agree, all with `GOTOOLCHAIN=local`. See
  [Marketplace readiness](marketplace-readiness.md#review-corrections-october-18-2026)
  for the reachability assessment and the dependency scan.
- Signal's live conversation list is bounded at 1,000 like the other services.
- The Signal pairing test assumed pairing could not finish before it read the
  status; under `-race` it failed about 1 in 75 runs. It now checks the
  published sequence: the link, then connected.
- The vendored license inventory in `CREDITS.md` is complete again.
- The two Secret Scanning alerts are classified in
  [vendored API-key alerts](vendored-key-alerts.md). No vendored file changed.

## Verification done

All checks use invented data in isolated temporary folders: no real account,
message, pairing, package installation, paid service, or network call to a
provider. They ran on an Arch Linux machine with Go 1.27.2, Quickshell 0.3.2,
Qt 6.12.0, and the Omarchy 4.0.5 package, from a copy of the candidate, with
its own `go.mod`, outside any installed plugin. See the
[release notes](history/RELEASE-0.6.0.md#verification-and-limits) for the
commands and counts.

Last checked October 18, 2026, after the third review round: the full Go
suite and `-race` (625 tests each, two intended Signal skips), `make test`,
`make lint`, `make helper`, vet, the libgm fixture, and an offline build;
63 Python tests; 77 Node tests; the headless QML subset (20 runs, 263
checks); and the full native Wayland QML suite (25 fixtures, 491 checks,
nothing excluded) pass, as do `omarchy plugin validate` and the emoji data
check. The earlier helper's route was reproduced first: the reviewer's probe,
run on 662763c, decoded an original profile photo and requested an alert,
and does neither on the candidate.

Earlier rounds ran on Go 1.27.0, Quickshell 0.3.1, and Qt 6.11.2, with
`go.mod` lowered to 1.27.0 in a test copy only; those runs do not qualify the
required compiler or Qt 6.12, and the counts above replace them. The headless
count rose from 255 to 263 with seven cursor-editing checks in the safe-fields
fixture and one disconnect check in the stale-helper fixture.

For a later candidate, repeat on the exact final commit: `make test`,
`GOTOOLCHAIN=local GOPROXY=off go test -race -mod=vendor -count=1 ./...`,
`make lint`, `make helper`, `make test-ui` on Hyprland, and
`omarchy plugin validate .`. Then update these counts if they change.

The first native run caught a problem in the new helper check: after the
helper restarted for a service change, the panel briefly treated it as
outdated. That was fixed before the final runs. Two other runs (one headless,
one native) stopped at the helper build with "disk quota exceeded" from the
test machine's `/tmp`, filled by the test's own Go build cache; after clearing
it, the same builds passed. In the third round, the first two Go runs failed
one test, `TestStaleSocketDoesNotOwnDataAndIsRecovered`, with "bind: invalid
argument". The test's Unix socket path ran past the 108-byte limit because the
test environment pointed `TMPDIR`, then `GOTMPDIR` (which `go test` passes to
tests), at a long cache folder. With the default `/tmp`, it passes.

Notes from the native runs:

- The native driver needs the compositor's `hyprctl` socket to place the
  test's own pop-out window. A runtime folder isolated for the fixtures hides
  it, so the review used a small wrapper that gives only `hyprctl` the
  session's runtime folder and instance signature. Everything else stays
  isolated.
- The keyboard fixture also failed on v0.5.5. Two causes were product
  bugs, both fixed (list focus with no chat selected, and the cursor after
  assigned text). One was an assertion that skipped a documented step:
  returning from Settings restores the writable chat's composer, so Escape
  leaves it before closing the panel. The fixture now checks every step.
- The chat-view check after returning from the pop-out failed intermittently,
  also without this round's changes. Focus was already on the composer; the
  compositor activates the panel window about 20 ms after the fixture's tick.
  The fixture now waits, with a bound, for activation, then makes the same
  focus check.
- Qt logs "Binding loop detected" for the text field editor's height, and once
  for the Telegram API ID field's two-way draft binding. The height warning
  also appears on v0.5.5. Both settle on the next pass and change no focus,
  layout, or test result. Qt 6.12.0 logs the height warning the same number
  of times as Qt 6.11.2 in the headless run.

## Not yet verified (needs live acceptance)

1. Install the candidate as an update over v0.5.5 with **Build helper**, and
   confirm one rebuild prompt, a successful build with no plugin reload loop,
   and the "Restart the shell" message until the shell restarts.
2. After the restart, check existing Google Messages, WhatsApp, Telegram, and
   Messenger accounts still connect, previews show, notifications fire, and
   the panel uses your theme's colors on Qt 6.12.
3. Confirm WhatsApp no longer lists a status conversation.
4. With a real current `signal-cli`, check that `--ignore-stories` is accepted,
   then link, send and receive text and an image, react, see a remote delete,
   see a disappearing message removed, and confirm signal-cli's attachment
   folder no longer holds deleted or view-once media.
5. Start a direct chat on each service, and a group only where you are
   willing to invite real people. Group creation is real and notifies them.
6. Update with the shell restarted before rebuilding: the panel should show no
   conversations or alerts and ask to rebuild the helper; after the rebuild
   everything returns without re-pairing or lost settings.
7. Optionally, on an unusual mount layout (plugin folder or `bin` on its own
   filesystem), confirm the build either succeeds or shows the "cannot install
   its helper safely" message, and the shell does not reload during it.

## Publication steps (maintainer)

These steps were pending when the candidate was prepared. Check the linked
public records for their current state. Keep `main` fixed while a Marketplace
review is pending.

1. Review the candidate, then fast-forward or merge it to `main` and push.
2. Create the `v0.6.0` tag and GitHub release from that commit, using the
   release notes.
3. Edit the open Marketplace update request
   ([#10861](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861))
   to target the new full SHA, and reply to the latest review comment.
4. Respond on PRs #4, #6, and #9 and issue #1 with what was included.
5. Decide how to close the two Secret Scanning alerts in the public beta
   repository, using the classification record.
