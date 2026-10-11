import QtQuick
import QtQuick.Controls as Controls
import Quickshell
import Quickshell.Io
import qs.Ui as Ui

// Qt's native Wayland paste buffers the provider before maximumLength applies.
// Route every user paste entry point through the bounded external reader.
Ui.TextField {
  id: root
  property bool pasteAllowed: true
  // Message fields keep pasted line breaks, as Qt's native paste did.
  property bool allowLineBreaks: false
  property string pasteContext: ""
  readonly property bool pasteBusy: _pasteSnapshot !== null || clipboardProcess.running
  property string pasteError: ""
  property var _pasteSnapshot: null

  function cancelPaste() {
    _pasteSnapshot = null
    if (clipboardProcess.running) clipboardProcess.running = false
    else pasteWatchdog.stop()
  }

  function requestPaste(primary) {
    if (pasteBusy || !enabled || !visible || readOnly || !pasteAllowed) return
    forceActiveFocus()
    pasteError = ""
    _pasteSnapshot = { text: text, cursor: cursorPosition,
      start: selectionStart, end: selectionEnd, context: pasteContext }
    var script = decodeURIComponent(Qt.resolvedUrl("scripts/clipboard.py").toString().replace(/^file:\/\//, ""))
    clipboardProcess.command = ["python3", script]
    if (primary) clipboardProcess.command = clipboardProcess.command.concat(["--primary"])
    pasteWatchdog.restart()
    clipboardProcess.running = true
  }

  function separateUndo() {
    // Cursor movement separates Qt's undo groups. Without these boundaries,
    // public remove/insert calls can merge a paste with earlier/later typing.
    if (text.length === 0) return
    var cursor = cursorPosition, start = selectionStart, end = selectionEnd
    cursorPosition = cursor === 0 ? text.length : 0
    cursorPosition = cursor
    if (end > start) select(cursor === start ? end : start, cursor === start ? start : end)
  }

  function finishPaste(code, output) {
    var saved = _pasteSnapshot
    _pasteSnapshot = null
    if (!saved || !activeFocus || !enabled || !visible || readOnly || !pasteAllowed
        || text !== saved.text || cursorPosition !== saved.cursor
        || selectionStart !== saved.start || selectionEnd !== saved.end
        || pasteContext !== saved.context) return
    var reply
    try { reply = JSON.parse(output) } catch (_) {
      pasteError = "Paste could not start. Check Python 3 and wl-clipboard in Settings."
      return
    }
    if (code !== 0 || reply.ok !== true || typeof reply.text !== "string") {
      pasteError = reply.error || "Clipboard text could not be read."
      return
    }
    // Other fields flatten line breaks; no field accepts embedded NULs.
    var value = reply.text.replace(/\r\n?/g, "\n")
    if (!allowLineBreaks) value = value.replace(/[\n\u2028\u2029]/g, " ")
    if (value.indexOf("\u0000") >= 0) {
      pasteError = "Clipboard text contains an unsupported character."
      return
    }
    if (text.length - (saved.end - saved.start) + value.length > maximumLength) {
      pasteError = "Paste is too long for this field."
      return
    }
    if (value.length === 0) return
    separateUndo()
    remove(saved.start, saved.end)
    insert(saved.start, value)
    cursorPosition = saved.start + value.length
    separateUndo()
  }

  // Connections survive instance-specific focus/enabled handlers.
  Connections {
    target: root
    function onActiveFocusChanged() { if (!root.activeFocus) root.cancelPaste() }
    function onEnabledChanged() { if (!root.enabled) root.cancelPaste() }
    function onVisibleChanged() { if (!root.visible) root.cancelPaste() }
    function onReadOnlyChanged() { if (root.readOnly) root.cancelPaste() }
    function onPasteAllowedChanged() { if (!root.pasteAllowed) root.cancelPaste() }
    function onPasteContextChanged() { root.cancelPaste() }
    function onTextChanged() { if (root._pasteSnapshot) root.cancelPaste() }
    function onCursorPositionChanged() { if (root._pasteSnapshot) root.cancelPaste() }
    function onSelectionStartChanged() { if (root._pasteSnapshot) root.cancelPaste() }
    function onSelectionEndChanged() { if (root._pasteSnapshot) root.cancelPaste() }
  }
  Component.onDestruction: cancelPaste()

  // forwardTo runs before instance-specific Keys handlers (composer shortcuts,
  // emoji navigation, etc.), so those handlers cannot reopen native paste.
  Keys.priority: Keys.BeforeItem
  Keys.forwardTo: [pasteKeys]
  Keys.onShortcutOverride: function(event) {
    if (event.matches(StandardKey.Paste)) event.accepted = true
  }
  Item {
    id: pasteKeys
    Keys.onPressed: function(event) {
      if (event.matches(StandardKey.Paste)) {
        event.accepted = true
        root.requestPaste(false)
      }
    }
  }

  // Consume both middle-button press and release; Qt pastes PRIMARY on release.
  MouseArea {
    anchors.fill: parent
    acceptedButtons: Qt.MiddleButton
    onPressed: function(mouse) {
      root.forceActiveFocus()
      root.cursorPosition = root.positionAt(mouse.x, mouse.y)
    }
    onClicked: root.requestPaste(true)
  }

  // Do not use Qt's PasteAction or bind to editor.canPaste. No native payload
  // read is needed to offer paste; the bounded reader handles empty/nontext data.
  Controls.ContextMenu.menu: Controls.Menu {
    popupType: Controls.Popup.Item
    Controls.MenuItem { text: "Undo"; enabled: root.canUndo; onTriggered: root.undo() }
    Controls.MenuItem { text: "Redo"; enabled: root.canRedo; onTriggered: root.redo() }
    Controls.MenuSeparator {}
    Controls.MenuItem { text: "Cut"; enabled: !root.readOnly && root.selectedText.length > 0 && !root.password; onTriggered: root.cut() }
    Controls.MenuItem { text: "Copy"; enabled: root.selectedText.length > 0 && !root.password; onTriggered: root.copy() }
    Controls.MenuItem {
      objectName: "boundedPasteAction"
      text: "Paste"
      enabled: root.enabled && !root.readOnly && root.pasteAllowed && !root.pasteBusy
      onTriggered: root.requestPaste(false)
    }
    Controls.MenuItem { text: "Delete"; enabled: !root.readOnly && root.selectedText.length > 0; onTriggered: root.remove(root.selectionStart, root.selectionEnd) }
    Controls.MenuSeparator {}
    Controls.MenuItem { text: "Select all"; enabled: root.text.length > 0; onTriggered: root.selectAll() }
  }

  Controls.ToolTip.text: pasteError
  Controls.ToolTip.visible: pasteError !== "" && (activeFocus || hovered)
  Controls.ToolTip.timeout: 6000

  Process {
    id: clipboardProcess
    stdout: StdioCollector { id: pasteOutput; waitForEnd: true }
    // Provider stderr is discarded by the reader; never show clipboard data or
    // credentials in logs. The reader kills its child group on SIGTERM too.
    onExited: function(code) {
      pasteWatchdog.stop()
      pasteKill.stop()
      root.finishPaste(code, pasteOutput.text)
    }
  }
  Timer {
    id: pasteWatchdog
    interval: 3000
    onTriggered: {
      var pending = root._pasteSnapshot !== null
      var started = clipboardProcess.running
      root.cancelPaste()
      // SIGTERM lets the reader kill its whole provider group. An immediate
      // SIGKILL would orphan wl-paste's own copy child, so escalate only if
      // the reader ignores SIGTERM.
      if (started) pasteKill.restart()
      if (pending) root.pasteError = started ? "Paste timed out. Try copying the text again."
        : "Paste could not start. Check Python 3 and wl-clipboard in Settings."
    }
  }
  Timer {
    id: pasteKill
    interval: 1000
    onTriggered: if (clipboardProcess.running) clipboardProcess.signal(9)
  }
}
