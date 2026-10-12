import QtQuick
import QtQuick.Controls as Controls
import qs.Ui as Ui

// Masked field for short secrets such as the Telegram api_hash.
//
// Through Qt 6.12.0 an editable TextInput reads the whole clipboard on every
// clipboard change (QTBUG-149610); a read-only one does not. TextEdit has no
// password mode, so this field stays read-only and applies key presses itself.
// It has no input method, which ASCII secrets do not need, and pastes only
// through BoundedPaste.
Ui.TextField {
  id: root
  property bool pasteAllowed: true
  readonly property bool pasteBusy: paste.busy
  property alias pasteError: paste.error

  password: true
  readOnly: true
  cursorVisible: activeFocus
  inputMethodHints: Qt.ImhHiddenText | Qt.ImhSensitiveData | Qt.ImhNoPredictiveText

  function replaceRange(start, end, value) {
    var next = text.slice(0, start) + value + text.slice(end)
    if (next.length > maximumLength) return
    text = next
    cursorPosition = start + value.length
  }

  function requestPaste(primary) {
    if (paste.busy || !paste.allowed) return
    forceActiveFocus()
    paste.request(primary)
  }

  Keys.priority: Keys.BeforeItem
  Keys.forwardTo: [secretKeys]
  Keys.onShortcutOverride: function(event) {
    if (event.matches(StandardKey.Paste)) event.accepted = true
  }
  // Runs before instance Keys handlers, like SafeTextField's paste guard.
  Item {
    id: secretKeys
    Keys.onPressed: function(event) {
      var start = root.selectionStart, end = root.selectionEnd
      var mods = event.modifiers & (Qt.ControlModifier | Qt.AltModifier | Qt.MetaModifier)
      if (event.matches(StandardKey.Paste)) {
        event.accepted = true
        root.requestPaste(false)
      } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
        event.accepted = true
        root.accepted()
        root.editingFinished()
      } else if (event.key === Qt.Key_Backspace || event.key === Qt.Key_Delete) {
        event.accepted = true
        if (end > start) root.replaceRange(start, end, "")
        else if (event.key === Qt.Key_Backspace && start > 0) root.replaceRange(start - 1, start, "")
        else if (event.key === Qt.Key_Delete && start < root.length) root.replaceRange(start, start + 1, "")
      } else if (!mods && event.text.length > 0 && !/[\x00-\x1f\x7f]/.test(event.text)) {
        event.accepted = true
        root.replaceRange(start, end, event.text)
      }
      // Navigation and selection stay native; read-only TextInput handles them.
    }
  }

  BoundedPaste {
    id: paste
    allowed: root.enabled && root.visible && root.pasteAllowed
    focused: root.activeFocus
    maximumLength: root.maximumLength
    currentText: root.text
    cursorPosition: root.cursorPosition
    selectionStart: root.selectionStart
    selectionEnd: root.selectionEnd
    onReady: function(start, end, value) { root.replaceRange(start, end, value) }
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

  Controls.ContextMenu.menu: Controls.Menu {
    popupType: Controls.Popup.Item
    Controls.MenuItem {
      objectName: "boundedPasteAction"
      text: "Paste"
      enabled: root.enabled && root.pasteAllowed && !root.pasteBusy
      onTriggered: root.requestPaste(false)
    }
    Controls.MenuItem { text: "Delete"; enabled: root.selectedText.length > 0; onTriggered: root.replaceRange(root.selectionStart, root.selectionEnd, "") }
    Controls.MenuSeparator {}
    Controls.MenuItem { text: "Select all"; enabled: root.text.length > 0; onTriggered: root.selectAll() }
  }

  Controls.ToolTip.text: pasteError
  Controls.ToolTip.visible: pasteError !== "" && (activeFocus || hovered)
  Controls.ToolTip.timeout: 6000
}
