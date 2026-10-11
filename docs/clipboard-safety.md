# Bounded clipboard paste

The v0.5.0 Marketplace review identified a resource-exhaustion path in Qt's
native Wayland clipboard reader. Thank you to
[HANCORE-linux for the report](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6104289546).
A field's `maximumLength` applies after the native clipboard transfer, so it
cannot bound the amount of data received or the time spent receiving it.

## Entry points

Every editable product field uses [SafeTextField](../SafeTextField.qml):
conversation search, unified search, the composer, attachment captions, emoji
search, Telegram API ID/hash, keyboard-action search, and shortcut editing.

- Standard paste keys, including Ctrl+V and Shift+Insert, are consumed before
  native text handling. `Keys.forwardTo` guards them before instance-specific
  shortcut handlers. Shortcut override keeps global actions from stealing paste.
- A middle-button MouseArea consumes both press and release and requests PRIMARY
  through the bounded reader. Native TextInput handles PRIMARY on release.
- The replacement context menu calls `requestPaste`, with no native
  `PasteAction`, `paste()` call, or `canPaste` binding. Undo, redo, cut, copy,
  delete, and select-all still use normal Qt text operations.
- These inputs have no DropArea or native text-drop handling. Drag-and-drop text
  is not an alternative paste path.

Qt's internal `canPaste` updates still inspect offered format names. In the
reviewed Qt source, `hasText` checks formats and the Wayland `hasFormat_sys`
implementation uses the offered format list, without receiving text content.
The body-buffering path is `readData`, reached by native `text()` retrieval.
See [Qt TextInput source](https://github.com/qt/qtdeclarative/blob/b8308f340dac1b2f321c85aea54f8444b2c0fd9f/src/quick/items/qquicktextinput.cpp),
[Qt MIME data source](https://github.com/qt/qtbase/blob/e3d283ec067e21d9fcdb62a565acba75b7546989/src/corelib/kernel/qmimedata.cpp),
and [Qt Wayland data-offer source](https://github.com/qt/qtbase/blob/e3d283ec067e21d9fcdb62a565acba75b7546989/src/plugins/platforms/wayland/qwaylanddataoffer.cpp).

## Transfer and lifetime bounds

[The Python reader](../scripts/clipboard.py) invokes `wl-paste` with fixed argv,
without a shell or clipboard contents in arguments. It reads through a
nonblocking pipe and holds at most 65,537 incoming bytes before rejecting an
oversized transfer. A successful transfer contains at most 65,536 bytes.
The 1.5-second deadline covers the whole child transfer and exit, including a
provider that keeps delivering small chunks or closes stdout without exiting.
The QML launcher has a separate 3-second watchdog for a reader that fails to
start or hangs. It stops the reader with SIGTERM, so the reader can kill its
whole provider group, and sends SIGKILL only if it is still running a second
later.

Only complete, successful UTF-8 text reaches the field. Empty, invalid,
oversized, timed-out, and failed transfers leave the existing text and selection
unchanged. Embedded NULs and pastes exceeding the field's own length limit are
rejected. The composer and attachment captions keep pasted line breaks,
normalized to newlines, as Qt's native paste did. Other fields turn them into
spaces. Cursor movement
separates undo groups so pasting does not merge with adjacent typing.

One request per field can run at a time. Changing focus, text, selection,
cursor, conversation context, visibility, enabled/read-only state, or permission
to paste cancels it. Completion checks the captured state again before applying
text. A cancelled result cannot be restored by returning to the same field or
conversation.

The child process group is killed and the child is reaped on completion, rejection, and
cancellation. Linux's parent-death signal kills `wl-paste` if the reader itself
is forcibly killed. That signal does not reach the `cat` child that `wl-paste`
starts to copy the transfer, which is why the watchdog sends SIGTERM first. The child also has a 128 MiB address-space limit, 2-second
CPU limit, and disabled core dumps. Provider stderr is discarded. No clipboard
content is logged or written to disk by this reader. Python 3 and wl-clipboard
are user-installed dependencies; missing tools fail closed with visible guidance.

## Verification and limits

Run the isolated checks with:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p 'clipboard_test.py'
OMACHAT_QML_HEADLESS=1 dbus-run-session -- python3 tests/run-qml.py --fixture clipboard
OMACHAT_QML_HEADLESS=1 dbus-run-session -- python3 tests/run-qml.py --fixture clipboard-unavailable
OMACHAT_QML_HEADLESS=1 dbus-run-session -- python3 tests/run-qml.py --fixture clipboard-watchdog
```

The provider fixture supplies invented text over real subprocess pipes. It
covers exact/over-limit UTF-8, infinite streams, stalls, continuous trickles,
early pipe closure, failure after partial data, missing tools, SIGTERM cleanup
including a forked provider descendant, and forced reader death. A source check rejects unguarded product input types.
Actual QML controls exercise typing, selection replacement, one-step undo/redo,
all four paste routes, duplicate suppression, field limits, and stale-result
cancellation, including instances with their own focus and key handlers.
Qt's in-process test clipboard holds a sentinel during these checks, and an
unguarded control proves native paste would insert it. The offscreen backend
has no PRIMARY selection, so native middle-click paste cannot be exercised
there; the guard consumes the middle button regardless. A watchdog fixture uses
a fake reader that hangs, then checks SIGTERM cleanup and the SIGKILL fallback.

The local UI checks use Qt 6.11.2's offscreen backend and a fake `wl-paste`.
They do not reproduce a foreign streaming provider against Qt 6.12 on a real
Wayland desktop. An isolated headless Hyprland attempt could not initialize its
backend. The user's clipboard, paired profiles, and installed release are not
used by these tests. Fresh exact-commit Marketplace validation and maintainer
review remain required when this fix is published.
