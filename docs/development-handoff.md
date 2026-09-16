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
| `private-dev/dev` | Private integration branch and source of truth for active development | Messenger implementation and the completed live parity pass; the current branch tip contains this handoff after it is pushed |
| `public-beta/beta` | Public, opt-in builds for invited testers | `766bb84`, tagged and published as prerelease `v0.4.1`, with current `dev` ancestry plus beta onboarding |

Develop on `dev` and push it only to `private-dev/dev`. The public beta now
contains the reviewed development series through `a9658b6`, merged as
`766bb84` while preserving its beta-only branding, onboarding, and privacy-safe
reporting links. Its manifest, README, About page, annotated `v0.4.1` tag, and
public GitHub prerelease all agree on version 0.4.1. Promote later development
work to `public-beta/beta` only when OneLegDave chooses it for beta testing.
Stable promotion to `origin/main`, version changes, tags, releases, and
marketplace updates are separate, explicit release work.

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
- reactions;
- outbound M4A voice notes with optional ffmpeg/ffplay;
- optional GIPHY search using the shared personal API-key setting;
- an inline infinite-loop player for video-backed GIF messages; and
- explicit notices when older encrypted history is unavailable.

The active installed checkout was fast-forwarded to audited feature commit
`ce87bb5` on 2026-09-16. The helper was rebuilt from vendored source, and the
Omarchy shell was cleanly restarted after the Settings, About, manifest, and
documentation audit was installed. The new shell-owned helper (PID `2738608`)
reported source fingerprint
`248ea2d5890603561bde8a905af4e43dcd7d044e43d759c5e8925be6bde45cd2`
through the live owner-only socket. Google Messages, WhatsApp, Telegram, and
Messenger all reported connected after restart. OneLegDave
already live-confirmed the cross-service notification badges and subsequently
confirmed that the cross-platform WhatsApp audio fallback plays successfully
on the previously failing iPhone.

The 2026-09-16 live parity pass confirmed that a video-backed Messenger GIF
stays inline and loops, a new incoming reply appears without sending or
manually refreshing, and small PNG and plain-text attachments send
successfully. OneLegDave confirmed each visible result. No defect was
reproduced, so no new regression code was added. Temporary screenshots,
recording output, and attachment fixtures were removed after verification.

Older encrypted Messenger history may be unavailable even when recent messages
and the conversation list work. That is a protocol/data-availability limit,
not permission to invent missing messages. Locally cached recent history should
survive clean helper restarts, but pairing resets, provider changes, and cache
migrations can still make re-pairing necessary.

## Exact next work

Voice recording, GIF search, and reactions are implemented and live-confirmed
by OneLegDave. The first reaction pass exposed a Messenger-only UI gate that hid
the existing action; `081e75f` fixed it and the visible recheck passed. Typing
indicators were subsequently removed end to end at OneLegDave's request because
the app does not need a typing preview. Calling remains explicitly out of scope
and unsupported.

WhatsApp now exposes the shared optional GIPHY search and routes search and
download requests through the existing hardened daemon implementation. Selected
GIFs are converted with ffmpeg and sent as the MP4 video messages with
`gifPlayback=true` required by WhatsApp. Incoming GIF-playback videos retain
that flag and render as inline looping animations instead of generic video
buttons. The repeat live outbound and inbound WhatsApp GIF check passed.

WhatsApp reactions are now routed through the native protocol backend. The
local cache tracks per-participant reaction state so add, switch, removal, live
incoming aggregation, and restart persistence remain consistent. Conversation
names now upgrade persisted LID/number fallbacks from synced contacts, history,
push names, and business names instead of remaining stuck at "WhatsApp User."
Synthetic backend and QML coverage passes. OneLegDave live-confirmed that a
reaction reaches the phone and that the conversation list shows the saved name.

WhatsApp now records OGG/Opus voice notes and sends them as native PTT audio.
Telegram publishes live conversation and unread-status updates, and Messenger
marks newer ordinary incoming message rows unread while honoring provider read
watermarks. The shared service also derives a safe unread fallback from its
conversation models. The top-bar badge therefore aggregates all enabled
services, and each in-app service tab receives its own red unread badge.
OneLegDave live-confirmed the notification behavior for the affected services.

The first live WhatsApp outbound voice check delivered the message shell, but
the phone rejected playback as unavailable. The transport fix now validates
the complete mono Ogg Opus page stream, derives the duration from its final
48 kHz granule position, and includes both `seconds` and
`mediaKeyTimestamp` alongside the encrypted upload metadata. A repeat live
check showed the correct duration on the phone, proving that the corrected
payload is active, but playback still failed with the same unavailable-media
message. Inbound phone-to-app voice already works. Before changing transport
again, OneLegDave confirmed the failed receiver is an iPhone and that the same
outbound build plays correctly on Android. OmaChat now records WhatsApp audio
as AAC/M4A and sends it as a standard non-PTT audio clip, preserving the mic,
preview, duration, and send workflow while avoiding the iPhone-incompatible
linked-device OGG/Opus PTT path. OneLegDave confirmed the fallback plays on the
previously failing iPhone, closing the reported failure; the earlier Android
playback check had already passed on the native PTT path.

Telegram reactions are now implemented through MTProto. Standard emoji
reactions can be added, switched, and removed; history carries existing counts
and current-user state, and live Telegram reaction updates replace the local
optimistic result. The shared reaction action is enabled in the Telegram inbox.
This backend and QML path has synthetic coverage. OneLegDave then live-confirmed
the Telegram reaction UI and result on the installed build.

The helper connection screen now shows the same rotating glyph and moving
activity bar used during builds, changes its status text to "Connecting to
helper...", and hides Retry while startup or reconnection is in progress. This
prevents repeated starts during an ordinary transient connection.

A full product-claims audit then reconciled Settings, About, the README feature
matrix, dependency guidance, and visible credits with the shipped code. It
added the missing Telegram reaction claim and Messenger `mautrix-meta` credit,
replaced overstated "read receipts" wording with mark-as-read, clarified
Telegram credential network use and protocol limitations, and removed the
unused webcam setting and dependency claim. A focused regression test now
guards the highest-risk capability and credit claims.

The synthetic gate passed `make test`, `make lint`, `make validate`,
`make test-ui`, `go test -race -mod=vendor -count=1 ./...`, and
`git diff --check`. A GIF chosen through search, the corrected reaction action,
the compatible WhatsApp voice fallback passed live. Telegram reactions and the
helper-connection presentation subsequently passed OneLegDave's visible check.
No Messenger feature follow-up is currently selected.

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

At handoff update on 2026-09-16, every command above passed on `dev`, including
the product-claims regression test. The exact audited feature commit was
installed, its helper fingerprint matched the source, and all four services
reported connected. No message or reaction was sent as part of those checks.

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
