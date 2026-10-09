# Keyboard control and searchable emoji: implementation plan

Status: implementation complete; automated and visual review complete; commits pending.
Branch: `merged-and-extended`.

## Integrated baseline

The branch includes the heads of upstream PRs #2–#7, preserving their history
with six local merge commits. Conflicts were resolved without dropping the
Telegram/Messenger empty-thread acknowledgement, new-conversation picker, or
automatic pop-out behavior and regression coverage.

Baseline verification passed:

- `make test`
- `make lint`
- `make helper`
- `make validate`
- `make test-ui`
- `go test -race -mod=vendor -count=1 ./...`

Two uncommitted keyboard-test fixes stop repeating timers while QtTest pumps
nested event loops. These belong in the keyboard-control feature commit.
Nothing has been pushed. No Hyprland bindings were changed. The requested local
plugin install was validated separately; account pairing and messaging were not
exercised by automated tests.

## Agreed product decisions

- Hybrid navigation: Tab/Shift+Tab and arrows, plus Vim-style keys outside text
  fields. No explicit Vim mode and no interception of ordinary typed text.
- Opening either app surface focuses the conversation list and retains its
  existing selected conversation. If there is no available list, focus the
  appropriate setup/recovery control instead.
- Enter on a conversation opens it and focuses its composer when enabled.
  Read-only or disconnected conversations must have a useful focus fallback.
- Escape first dismisses the innermost dialog/picker, then leaves text entry,
  then closes OmaChat. Leaving the composer returns to the conversation list
  without discarding the draft.
- App action shortcuts are editable from the first version.
- `?` outside text input opens a searchable action/help menu that also displays
  shortcuts. In text input it remains a literal question mark.
- Emoji search auto-focuses when opened, filters live, tolerates typos, and
  supports English names/keywords plus GitHub/Discord-style aliases.
- Emoji metadata is bundled locally, pinned and attributed; no runtime network
  requests or new package installation.
- Exactly two feature commits: keyboard control, then emoji search. Preserve
  the existing PR merge history. Keep feature changes uncommitted until local
  verification and the user's visual verification are complete.

## Scope and boundaries

Every currently exposed app operation should be reachable without a mouse in
both the anchored panel and standalone pop-out: service selection, inbox search,
next unread, opening conversations, history, message actions, composition,
attachment staging, voice-note controls, emoji/reactions, new direct/group
conversations, settings, pairing, recovery/build/update controls, and window
layout/return-to-panel actions where available.

This work does not add new protocol features, phone-side pairing automation,
custom Discord server emoji, or external application keyboard behavior.
Provider capabilities and disabled/read-only states remain authoritative.
Native file pickers, browsers, installers, and phone actions retain their own
controls and confirmation steps. No Hyprland bindings/configuration or packaged
Omarchy components will be edited.

## Commit 1 — `feat: add configurable full keyboard control`

### Shared action and focus routing

Introduce a small app-local action registry and keybinding matcher, shared by
both surfaces. Proposed new modules/components should be named to match the
existing QML/JavaScript conventions; they do not exist yet.

The registry is the common source for routing, displayed shortcuts, action-menu
entries, availability checks, and settings. Distinguish text-input, navigation,
and modal contexts. Modifier-based app actions can be explicitly allowed while
editing; bare navigation keys never override text input, selection, or IME
composition. Match printable shortcuts by text so `?` works across layouts.

Replace the unconditional root Escape close with layered dispatch. Dialogs
contain focus, restore it to their opener, and do not leak activation or
navigation into the background. Escape from the composer returns to the list;
Escape from settings first leaves its editor, then returns to chats, then closes
OmaChat. Drafts and selected conversations survive focus changes and surface
transfers.

Use a deliberate Tab order, visible focus indicators, appropriate accessible
names, and scrolling that keeps focused controls visible. Hidden/disabled items
must not be reachable. Essential Tab/arrows/Enter/Escape behavior remains a
reliable baseline even when action shortcuts are customized.

### Proposed default action bindings

All action bindings below are defaults, editable in Settings. Standard control
navigation is not a replacement for the action registry.

| Action | Proposed default/context |
| --- | --- |
| Searchable actions/help | `?` outside editors; `Ctrl+Shift+P` as an editing-safe alternative |
| Search conversations | `/` outside editors; `Ctrl+F` |
| Open highlighted conversation | Enter from conversation list |
| Focus composer | `i` outside editors |
| Focus message history | `m` outside editors |
| Move current list/grid cursor | Arrows; `j`/`k` outside editors; `h`/`l` in spatial navigation |
| First/last list item | Home/End; page movement with PageUp/PageDown |
| Switch service | Existing `1`–`5` outside editors; `Ctrl+1`–`Ctrl+5` alternatives |
| Refresh | Existing `r` outside editors; `Ctrl+R` alternative |
| Next unread | `u` outside editors |
| New conversation | `Ctrl+N` |
| Open settings | `Ctrl+,` |
| Open emoji picker | `Ctrl+E`, when a writable conversation is selected |
| Attach file | `Ctrl+O`, when attachment action is available |
| Actions for selected message | Enter from focused message history |
| Send text | Preserve Enter in the current single-line composer |

Avoid adding accidental send/unpair/install shortcuts. These actions still
require explicit activation and retain existing confirmation requirements.
Voice, window layout, updates/build, and other less common actions can be
reachable through the action menu and Tab navigation without default bindings.

### Cover the currently mouse-only paths

- Turn next-unread into an accessible keyboard-activatable control.
- Add message-history cursor navigation, skipping day headings. Identify the
  selected message by stable ID through live updates and history prepends.
- Expose existing copy-text, links/copy targets, media/playback, reactions,
  reaction removal, and applicable retry/actions through keyboard controls.
  Do not invent unsupported reply/edit/delete functionality.
- Make the new-conversation picker fully keyboard operable: search, list cursor,
  direct selection, multi-recipient group selection with Space, group name,
  create/cancel controls, modal focus containment, and error recovery.
- Audit settings, pairing, service choices, toggles/dropdowns, and recovery
  controls for focusability and scrolling.

Current integration anchors: `Panel.qml:591`, `InboxView.qml:1042`,
`InboxView.qml:1071`, `InboxView.qml:1111`, `InboxView.qml:1351`,
`InboxView.qml:1697`, `InboxView.qml:2144`, and `SettingsView.qml:61`.
Line numbers describe the merged baseline and will move during implementation.

### Editable shortcut preferences

Add a keyboard section to Settings with an action list, current bindings,
keyboard-only recording/editing, per-action reset, and reset-all. Users can
assign/clear action shortcuts without disabling baseline focus navigation.
Reject unsupported chords, reserved control keys, and conflicts in overlapping
contexts; show the reason before saving. Recording must not execute the recorded
action. Reset and failure recovery must remain keyboard reachable.

Store validated overrides via the existing atomic configuration store; missing
preferences use defaults. Preserve unrelated preferences and credentials. Save
through an explicit helper RPC and return only panel-safe preferences. Ensure
live settings changes reach the router and persist across helper/app restarts.
Do not store arbitrary QML, JavaScript, or commands in preferences.

Existing paths: `internal/store/config.go`, `internal/daemon/config.go`,
`internal/daemon/server.go`, `internal/wire/wire.go`, `Service.qml`,
`Panel.qml`, and `SettingsView.qml`.

### Verification

Write failing tests for each behavior slice before implementation. Cover:

- Initial focus, arrows/Vim keys, Enter-to-compose, drafts and surface transfer.
- Literal shortcut characters including `?`, numbers, and Vim keys in every
  editor, including shortcut recording and IME/text composition handling.
- Layered Escape, modal containment, focus restoration, nested action/emoji
  pickers, and disabled/read-only/disconnected states.
- Searchable action menu, context-sensitive actions, and message cursor
  stability through refresh, new messages, and pagination.
- Direct/group picker keyboard flows and multi-selection.
- Shortcut save/reload/reset, conflicts, invalid configuration, atomic writes,
  unknown action IDs, and failed-save recovery.
- A keyboard-only route through settings, setup, media/voice, and window controls.

Include the existing timer re-entry fixes in `tests/qml/panel-keyboard.qml`
and `tests/qml/review.qml`. Run desktop keyboard tests serially because they
share compositor focus. Use synthetic data and isolated helper/runtime paths.

## Commit 2 — `feat: add offline fuzzy emoji search`

### Dataset and licensing

Use a pinned Emojibase data release as the candidate source for English names,
keywords, and shortcode presets. Its documentation supplies localized datasets
and GitHub plus Discord-style/JoyPixels shortcode presets.[1][2]

Before importing, verify the chosen release's files and every included data
source's redistribution/attribution terms. Emojibase's repository license is
MIT, but that alone is not a substitute for reviewing incorporated sources.[4]
Include notices, source URLs, version/commit, checksums, and a reproducible
manual generation script. Vendor only normalized data needed for the picker;
do not add npm dependencies or load a CDN at runtime.

Merge aliases by Unicode identity, accounting for presentation selectors, while
preserving the actual glyph emitted. Retain alternative labels rather than
choosing one platform vocabulary. Alias collisions may yield multiple results;
Discord-style names are approximate, not a claim of official Discord parity.[1]
Include the existing Omarchy keyword vocabulary where useful and keep a small
fallback if loading fails. Render native glyphs, not third-party emoji artwork.

### Matching and interaction

- Search field above the grid, auto-focused for insertion and reaction use.
- Search English label, keyword tags, shortcode aliases, and pasted emoji.
- Normalize case, surrounding colons, spaces, underscores, and hyphens without
  collapsing meaningful aliases such as `+1` and `-1`.
- Rank exact aliases/names first, then prefix and substring/token matches,
  followed by bounded edit-distance/transposition matches. Limit fuzzy matching
  for very short queries to avoid flooding results.
- Precompute the normalized index once; keep result ordering deterministic and
  filtering responsive. No per-keystroke network work.
- Support multiword searches, display accessible names/tooltips, and show an
  explicit no-results state. Keep an empty query browsable.
- Down/Tab moves from search to results. Arrows navigate the grid; Enter from
  search chooses the best current result, and Enter in the grid chooses its
  highlighted result. Up returns to search only from the first grid row; other
  Up presses continue grid navigation. Clear the query on reopen by default, with
  a Settings preference to retain and reapply it.
- Escape dismisses the picker and restores its opener. Insertion uses the
  composer's caret/selection rather than always appending. Reactions retain the
  intended message/conversation and cannot act on a stale selection.
- No automatic `:shortcode:` replacement in composer text in this version.

Current anchors: `InboxView.qml:125`, `InboxView.qml:481`,
`InboxView.qml:490`, `InboxView.qml:2052`, and the emoji `FileView` loading path.
The search/scoring module and generation/data files will be new.

### Verification

Use a pure-JavaScript suite for normalization and ranking, plus real QML tests
for focus, typed search, grid navigation, insertion, reactions, and Escape.
Representative cases: `thumbs up`, `thumbsup`, `:+1:`, `heart`, `love`, `tada`,
`:joy:`, `smiel`, case/separator differences, multiword queries, duplicate
aliases, Unicode variants, empty/no-match queries, and dataset-load fallback.
Validate glyphs, count/deduplicate records in code, verify attribution and
checksums, and measure search responsiveness using the full bundled dataset.

## Delivery and acceptance

For each feature commit:

1. Implement incrementally with failing tests first.
2. Run `make test test-ui lint helper validate` and appropriate race checks.
3. Offer a synthetic native-app demo for the user's keyboard/visual review,
   after automated tests pass. Do not access personal conversations.
4. Address feedback, then commit only that feature's reviewed files.

The first feature must work with the existing unfiltered emoji picker. The
second adds fuzzy search on top of the keyboard infrastructure. Together they
must permit every app-owned operation in scope without a mouse in both surfaces.
No pushes, upstream PR merges, desktop binding edits, or real installs are part
of this plan.

## Sources

[1] https://emojibase.dev/docs/shortcodes
[2] https://emojibase.dev/docs/datasets
[4] https://raw.githubusercontent.com/milesj/emojibase/master/LICENSE
