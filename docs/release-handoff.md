# Release handoff: OmaChat 0.6.0 candidate

This is the continuation point for the 0.6.0 release candidate. It records what
the candidate contains, what was verified and how, and what still needs live
acceptance and the maintainer's publication steps. Update it when the
candidate changes. Last updated October 17, 2026.

## Candidate

- Branch `release/omachat-complete-20261017`, based on `main` at v0.5.5
  (`3dbd975f8a0fbf427bbb7c09179664f02a824e53`). The candidate is the branch tip
  that contains this file. Nothing from it is pushed, tagged, or published.
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
- The vendored license inventory in `CREDITS.md` is complete again.
- The two Secret Scanning alerts are classified in
  [vendored API-key alerts](vendored-key-alerts.md). No vendored file changed.

## Verification done

All checks use invented data in isolated temporary folders: no real account,
message, pairing, package installation, paid service, or network call to a
provider. They ran on an Arch Linux machine with Go 1.27.0, Quickshell 0.3.1,
Qt 6.11.2, and Omarchy 4.0.4, from a copy of the candidate outside any
installed plugin. See the [release notes](history/RELEASE-0.6.0.md#verification-and-limits)
for the commands and counts.

Last checked October 17, 2026, after the review round: 61 Python tests, 76
Node tests, the headless QML subset (19 fixtures, 199 checks), and the full
native Wayland QML suite (24 fixtures, 432 checks, nothing excluded) pass, and
`omarchy plugin validate` passes. No Go, module, or vendored file changed in
that round, so the earlier Go, race, vet, and build results still apply.

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
  layout, or test result.

## Not yet verified (needs live acceptance)

1. Install the candidate as an update over v0.5.5 with **Build helper**, and
   confirm one rebuild prompt, a successful build with no plugin reload loop,
   and the "Restart the shell" message until the shell restarts.
2. After the restart, check existing Google Messages, WhatsApp, Telegram, and
   Messenger accounts still connect, previews show, and notifications fire.
3. Confirm WhatsApp no longer lists a status conversation.
4. With a real current `signal-cli`, check that `--ignore-stories` is accepted,
   then link, send and receive text and an image, react, see a remote delete,
   see a disappearing message removed, and confirm signal-cli's attachment
   folder no longer holds deleted or view-once media.
5. Start a direct chat on each service, and a group only where you are
   willing to invite real people. Group creation is real and notifies them.
6. Optionally, on an unusual mount layout (plugin folder or `bin` on its own
   filesystem), confirm the build either succeeds or shows the "cannot install
   its helper safely" message, and the shell does not reload during it.

## Publication steps (maintainer)

These are not done. Keep `main` fixed while a Marketplace review is pending.

1. Review the candidate, then fast-forward or merge it to `main` and push.
2. Create the `v0.6.0` tag and GitHub release from that commit, using the
   release notes.
3. Edit the open Marketplace update request
   ([#10861](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861))
   to target the new full SHA, and reply to the latest review comment.
4. Respond on PRs #4, #6, and #9 and issue #1 with what was included.
5. Decide how to close the two Secret Scanning alerts in the public beta
   repository, using the classification record.
