# Development handoff

This is the durable starting point for a new OmaChat development session. Read
it before changing the private `dev` branch, and update it before pushing
substantive development work. Chat history and an installed checkout are useful
evidence, but neither is the source of truth for branch state.

## Start every new session here

1. Read the repository instructions and this file completely. On OneLegDave's
   development machine, also read the local, intentionally untracked
   `AGENTS.md` and `/home/onelegdave/.config/ai-team/POLICY.md`.
2. Inspect before changing anything:

   ```bash
   git status --short --branch
   git branch -vv
   git remote -v
   git fetch --all --prune
   git log --oneline --decorate -20
   ```

3. Confirm the working branch is `dev`, it tracks `private-dev/dev`, and the
   worktree is clean. Do not merge, rebase, cherry-pick, tag, or push merely to
   make the branches look alike.
4. Inspect the active installed checkout separately when runtime behavior
   matters:

   ```bash
   git -C ~/.config/omarchy/plugins/onelegdave.omachat status --short --branch
   git -C ~/.config/omarchy/plugins/onelegdave.omachat rev-parse HEAD
   pgrep -a omachatd
   ```

   A source checkout, an installed checkout, and the running helper can be at
   different revisions. Record all three before and after a live test.

## Repository and branch map

| Remote and branch | Purpose | Current handoff baseline |
| --- | --- | --- |
| `origin/main` | Public stable releases only | `c4dcafa`, release `v0.3.13` |
| `private-dev/dev` | Private integration branch and source of truth for active development | Messenger work through `6d28851`; the current branch tip contains this handoff after it is pushed |
| `public-beta/beta` | Public, opt-in builds for invited testers | `98216fd`, stable code plus beta onboarding |

Develop on `dev` and push it only to `private-dev/dev`. The public beta does
not yet contain the Messenger development series. Promote selected, verified
development work to `public-beta/beta` only when OneLegDave chooses it for beta
testing. Stable promotion to `origin/main`, version changes, tags, releases,
and marketplace updates are separate, explicit release work.

The old `codex/messenger-backend` worktree branch is not the continuation
point. Its single patch is patch-equivalent to work already integrated and
extended on `dev`. Do not merge it into `dev`.

## Current Messenger state

The private `dev` branch contains native personal Messenger support using the
vendored `mautrix-meta` client. It currently includes:

- browser-cookie pairing and an encrypted-device database;
- personal and group conversations, names, avatars, sender names, and recent
  history;
- live incoming messages and conversation updates;
- outbound text, image, and file attachments;
- lazy incoming images, GIFs, stickers, files, video, and voice-message media;
- private conversation/message persistence across helper restarts;
- microsecond timestamps for locally sent messages;
- an inline infinite-loop player for video-backed GIF messages; and
- explicit notices when older encrypted history is unavailable.

The active installed checkout was at `4affe76` when this handoff was written,
with a running helper built from that source. The next `dev` commit, `6d28851`,
only removes a private chat name from a test-fixture note, so there is no known
runtime difference. Recheck this live instead of assuming it remains true.

Older encrypted Messenger history may be unavailable even when recent messages
and the conversation list work. That is a protocol/data-availability limit,
not permission to invent missing messages. Locally cached recent history should
survive clean helper restarts, but pairing resets, provider changes, and cache
migrations can still make re-pairing necessary.

## Exact next work

Do not reimplement features already listed above. Resume with bounded live
verification, using non-sensitive test content and OneLegDave's deliberate
actions:

1. Confirm a video-backed animated Messenger GIF stays inside its message,
   starts when requested, loops, and does not open briefly and close. The QML
   regression test covers the intended inline path, but this specific live fix
   still needs owner confirmation.
2. Confirm new incoming Messenger replies appear without sending a reply or
   manually refreshing. If updates remain slow, trace whether the transport
   event, helper publication, socket event, or QML merge is delayed before
   changing polling behavior.
3. Exercise the already implemented outbound image and file paths with a small,
   non-sensitive test file. Record the exact unsupported type or failure before
   expanding attachment behavior.
4. Add the smallest synthetic regression for any confirmed defect, then rerun
   the full checks below. Never turn a live account observation into a fixture
   containing real names, message text, account IDs, avatars, or media.

Calling, reactions, typing indicators, voice recording, and GIF search remain
unsupported for Messenger. Treat them as future scope, not regressions in the
current parity pass.

## Runtime and data safety

- Stable, beta, and development checkouts all use plugin ID
  `onelegdave.omachat` and the same account-data paths. They cannot run side by
  side under one Linux account.
- The Omarchy shell owns `bin/omachatd` as a child. Do not add or start a
  systemd service.
- A successful build does not prove the running helper changed. Compare the
  source fingerprint/status, process, and visible panel after a rebuild.
- Quickshell crashed once during a hot reload while Messenger fixes were being
  installed. Prefer a clean shell restart after native helper changes, and
  verify an older helper process did not survive before judging the result.
- Messenger credentials and caches live under `~/.local/share/omachat/`; media
  lives under `~/.cache/omachat/`. Never read, copy, attach, or commit personal
  stores merely to diagnose a UI issue. Use the existing sanitized RPC/status
  surfaces and synthetic fixtures first.

## Required verification

Use vendored dependencies and do not download modules or commit an ELF:

```bash
make test
make lint
make validate
make test-ui
go test -race -mod=vendor -count=1 ./...
git diff --check
```

At handoff creation on 2026-09-16, every command above passed on `dev` after
the documentation change. No live account action was performed as part of
those checks.

`make test-ui` uses synthetic content. Save screenshots only to a temporary
artifact directory, inspect them, and remove them after review. A live
Messenger check is separate and must be initiated deliberately by OneLegDave.

## Close every development session

1. Update the current state and exact next work in this file when either has
   changed.
2. Run checks proportional to the change and record any check that could not be
   completed. Passing commands are not a substitute for visible runtime proof.
3. Confirm `git diff --check`, inspect the complete diff, and preserve
   OneLegDave as Git author and committer with the authorized Codex trailer on
   substantive Codex-assisted commits.
4. Push only `dev` to `private-dev/dev`; verify the remote head afterward.
5. Recheck that `origin/main` and `public-beta/beta` did not move unless the
   session explicitly authorized a stable or beta promotion.
6. If the installed checkout or helper was changed for testing, record its
   exact revision and verify the panel/runtime result before ending the session.
