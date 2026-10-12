# Bounded clipboard paste

The v0.5.0 Marketplace review identified a resource-exhaustion path in Qt's
native Wayland clipboard reader. Thank you to
[HANCORE-linux for the report](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6104289546).
A field's `maximumLength` applies after the native clipboard transfer, so it
cannot bound the amount of data received or the time spent receiving it.
The v0.5.1 review then found a second path, Qt's background clipboard check
on editable text fields, described below. Thank you again for
[that report](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6106501652).

## Entry points

Every editable product field uses [SafeTextField](../SafeTextField.qml):
conversation search, unified search, the composer, attachment captions, emoji
search, Telegram API ID, keyboard-action search, and shortcut editing. The
Telegram API hash uses [SafePasswordField](../SafePasswordField.qml). Both share
[BoundedPaste](../BoundedPaste.qml) for every paste.

- Standard paste keys, including Ctrl+V and Shift+Insert, are consumed before
  native text handling. `Keys.forwardTo` guards them before instance-specific
  shortcut handlers. Shortcut override keeps global actions from stealing paste.
- A middle-button MouseArea consumes both press and release and requests PRIMARY
  through the bounded reader. Native Qt text items handle PRIMARY on release.
- The replacement context menu calls `requestPaste`, with no native
  `PasteAction`, `paste()` call, or `canPaste` binding. Undo, redo, cut, copy,
  delete, and select-all still use normal Qt text operations. The password
  field offers only paste, delete, and select-all.
- These inputs have no DropArea or native text-drop handling. Drag-and-drop text
  is not an alternative paste path.

## Clipboard-change reads

Qt also reads the clipboard without any paste. Every editable `TextInput`
connects `QClipboard::dataChanged()` to `q_canPasteChanged()`, which in Qt
6.11.2 and 6.12.0 calls `mimeData->text()`. On Wayland that buffers the whole
offer through `QWaylandMimeData::readData()`, with a 1-second wait per chunk
and no total byte limit or overall deadline. It runs whenever the clipboard
changes while the shell has keyboard focus. This is
[QTBUG-149610](https://qt-project.atlassian.net/browse/QTBUG-149610), fixed in
[qtdeclarative ef692a5](https://github.com/qt/qtdeclarative/commit/ef692a52e3a2fe133b64384076435ed212c14658)
for Qt 6.12.1, 6.8.10 and 6.13.0. Omarchy's own text fields share it until
then.

OmaChat avoids it without waiting for Qt:

- `SafeTextField` edits with a plain-text `TextEdit`. Its clipboard check,
  `QQuickTextControl::canPaste()`, only asks whether a text format is offered.
  The field keeps TextInput's single-line behavior: Return emits `accepted`,
  Tab moves focus, long text scrolls with the cursor, `maximumLength` refuses
  extra typing, and fields without `allowLineBreaks` turn line breaks and tabs
  into spaces. Input methods still work.
- `SafePasswordField` stays a read-only `TextInput`, so the check stops at
  `readOnly`. It applies typing, Backspace, Delete, and Return itself; cursor
  movement and selection stay native. It has no input method, which a 32-digit
  hex secret does not need.

See the [TextInput handler](https://github.com/qt/qtdeclarative/blob/v6.12.0/src/quick/items/qquicktextinput.cpp),
the [TextEdit check](https://github.com/qt/qtdeclarative/blob/v6.12.0/src/quick/items/qquicktextcontrol.cpp#L1643-L1693),
and the [Wayland reader](https://github.com/qt/qtbase/blob/6.12/src/plugins/platforms/wayland/qwaylanddataoffer.cpp).

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
spaces. Replacing a selection is one undo step.

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
OMACHAT_QML_HEADLESS=1 dbus-run-session -- python3 tests/run-qml.py --fixture safe-fields
```

On a Wayland desktop, `python3 tests/clipboard-change-probe.py` measures the
clipboard-change read directly. It opens a short-lived window per field type,
offers 8 MB of text, and counts the bytes the shell process reads. It replaces
your clipboard, then restores plain text or clears it.

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
A field fixture checks that no guarded field contains an editable `TextInput`,
plus Return, Tab, length limits, line-break rules, scrolling, and the password
field's editing and paste routes. The source check also requires the plain-text
`TextEdit` and the read-only password field.

The local UI checks use Qt 6.11.2's offscreen backend and a fake `wl-paste`.
On Qt 6.11.2 under Hyprland, the probe measured about 8,000,000 bytes read by a
native `TextField` (the control), 200 by `SafeTextField`, and 24 by
`SafePasswordField`. That probe offers a large finished transfer, not an
endless stream, and it was not run on Qt 6.12.0, whose handler code is the same. The user's clipboard, paired profiles, and installed release are not
used by these tests. Fresh exact-commit Marketplace validation and maintainer
review remain required when this fix is published.
