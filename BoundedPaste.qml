import QtQuick
import Quickshell.Io

// Reads clipboard text through scripts/clipboard.py, which caps size and time
// outside the shell. The owning field binds its state here and applies the
// result from `ready`; any change to that state cancels a pending paste.
Item {
  id: root
  // False while the field is disabled, hidden, read-only or paste is blocked.
  property bool allowed: true
  property bool focused: false
  property string context: ""
  property int maximumLength: 32767
  // Message fields keep pasted line breaks, as Qt's native paste did.
  property bool allowLineBreaks: false
  property string currentText: ""
  property int cursorPosition: 0
  property int selectionStart: 0
  property int selectionEnd: 0
  readonly property bool busy: _snapshot !== null || clipboardProcess.running
  property string error: ""
  property var _snapshot: null
  signal ready(int start, int end, string value)

  function cancel() {
    _snapshot = null
    if (clipboardProcess.running) clipboardProcess.running = false
    else pasteWatchdog.stop()
  }

  // The owner focuses its editor first, so the snapshot sees the final state.
  function request(primary) {
    if (busy || !allowed || !focused) return
    error = ""
    _snapshot = { text: currentText, cursor: cursorPosition,
      start: selectionStart, end: selectionEnd, context: context }
    var script = decodeURIComponent(Qt.resolvedUrl("scripts/clipboard.py").toString().replace(/^file:\/\//, ""))
    clipboardProcess.command = ["python3", script]
    if (primary) clipboardProcess.command = clipboardProcess.command.concat(["--primary"])
    pasteWatchdog.restart()
    clipboardProcess.running = true
  }

  function finish(code, output) {
    var saved = _snapshot
    _snapshot = null
    if (!saved || !focused || !allowed
        || currentText !== saved.text || cursorPosition !== saved.cursor
        || selectionStart !== saved.start || selectionEnd !== saved.end
        || context !== saved.context) return
    var reply
    try { reply = JSON.parse(output) } catch (_) {
      error = "Paste could not start. Check Python 3 and wl-clipboard in Settings."
      return
    }
    if (code !== 0 || reply.ok !== true || typeof reply.text !== "string") {
      error = reply.error || "Clipboard text could not be read."
      return
    }
    // Other fields flatten line breaks; no field accepts embedded NULs.
    var value = reply.text.replace(/\r\n?/g, "\n")
    if (!allowLineBreaks) value = value.replace(/[\n\u2028\u2029]/g, " ")
    if (value.indexOf("\u0000") >= 0) {
      error = "Clipboard text contains an unsupported character."
      return
    }
    if (currentText.length - (saved.end - saved.start) + value.length > maximumLength) {
      error = "Paste is too long for this field."
      return
    }
    if (value.length > 0) ready(saved.start, saved.end, value)
  }

  // Bindings from the owner land here, so instance handlers on the owner
  // cannot remove these cancellation guards.
  onFocusedChanged: if (!focused) cancel()
  onAllowedChanged: if (!allowed) cancel()
  onContextChanged: cancel()
  onCurrentTextChanged: if (_snapshot) cancel()
  onCursorPositionChanged: if (_snapshot) cancel()
  onSelectionStartChanged: if (_snapshot) cancel()
  onSelectionEndChanged: if (_snapshot) cancel()
  Component.onDestruction: cancel()

  Process {
    id: clipboardProcess
    stdout: StdioCollector { id: pasteOutput; waitForEnd: true }
    // Provider stderr is discarded by the reader; never show clipboard data or
    // credentials in logs. The reader kills its child group on SIGTERM too.
    onExited: function(code) {
      pasteWatchdog.stop()
      pasteKill.stop()
      root.finish(code, pasteOutput.text)
    }
  }
  Timer {
    id: pasteWatchdog
    interval: 3000
    onTriggered: {
      var pending = root._snapshot !== null
      var started = clipboardProcess.running
      root.cancel()
      // SIGTERM lets the reader kill its whole provider group. An immediate
      // SIGKILL would orphan wl-paste's own copy child, so escalate only if
      // the reader ignores SIGTERM.
      if (started) pasteKill.restart()
      if (pending) root.error = started ? "Paste timed out. Try copying the text again."
        : "Paste could not start. Check Python 3 and wl-clipboard in Settings."
    }
  }
  Timer {
    id: pasteKill
    interval: 1000
    onTriggered: if (clipboardProcess.running) clipboardProcess.signal(9)
  }
}
