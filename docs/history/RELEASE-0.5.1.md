# OmaChat v0.5.1

This release fixes a clipboard resource-exhaustion issue found during
Marketplace review of v0.5.0. Thank you to
[HANCORE-linux for the report](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6104289546).

## Highlights

- Every editable field now pastes through a bounded reader that runs outside
  the shell. Qt's native Wayland paste read the whole clipboard before field
  limits applied, so a hostile clipboard provider could stall the shell or
  exhaust its memory.
- Ctrl+V, Shift+Insert, the context menu, and middle-click PRIMARY paste use
  `wl-paste` with a 64 KiB limit and a 1.5-second deadline. Failed, oversized,
  or stale pastes leave the field unchanged. Typing, selection, copy, and
  undo/redo work as before.
- The composer and attachment captions keep pasted line breaks; other fields
  turn them into spaces.
- The dependency checklist now checks `wl-paste` as well as `wl-copy`; both come
  from the existing wl-clipboard dependency.

See [clipboard safety](../clipboard-safety.md) for entry points, limits, and
test coverage.

## Credits

HANCORE-linux is credited for the report. Contributor credits from v0.5.0, Marc
Ford's MIT attribution, and vendored notices are unchanged. Codex and Claude
provided the fix, reviews, and verification. See [full credits](../../CREDITS.md).

## Verification and limits

Go, race, vet, and plugin validation checks pass, along with 72 Node tests, 46
Python tests, and 16 isolated headless QML fixtures. Hostile-provider fixtures
cover infinite streams, stalls, slow trickles, forked provider children, and
forced reader death. The UI fixtures use Qt's offscreen backend and a fake
`wl-paste`. A foreign streaming provider was not reproduced against Qt 6.12 on
a live Wayland desktop.

## Update and marketplace

```sh
omarchy plugin update onelegdave.omachat
```

Rebuild the helper with the panel's **Build helper** action if prompted. The
helper code is unchanged, settings and paired credentials are retained, and
no re-pairing is needed. No dependency is installed automatically and no
executable is shipped in Git.

The v0.5.0 Marketplace update request now targets this release commit. Fresh
exact-commit checks and explicit maintainer approval are required; the
existing verified snapshot remains in place until the Marketplace publishes
the update.
