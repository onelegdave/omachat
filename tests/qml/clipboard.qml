import QtQuick
import QtQuick.Controls as Controls
import QtTest
import Quickshell
import Quickshell.Io
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0
  property int ticks: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function configure(value) { provider.setText(JSON.stringify(value)) }
  FileView { id: provider; path: Quickshell.env("OMACHAT_CLIPBOARD_FIXTURE"); atomicWrites: true }
  FloatingWindow {
    id: window
    implicitWidth: 600; implicitHeight: 250; visible: true
    Chat.SafeTextField {
      id: field
      width: 550; x: 20; y: 25
      pasteContext: "conversation-a"
      maximumLength: 100
      objectName: "testPasteField"
      // These instance handlers must not replace the shared security guards.
      onActiveFocusChanged: {}
      onEnabledChanged: {}
      Keys.onPressed: function(event) { if (event.key === Qt.Key_F2) event.accepted = true }
    }
    Chat.SafeTextField { id: other; width: 550; x: 20; y: 100 }
    TestEvent { id: keyboard }
  }
  Timer {
    running: true; repeat: true; interval: 140
    onTriggered: {
      stop()
      try {
        if (++root.ticks > 100) throw new Error("fixture timed out")
        if (field.pasteBusy) { restart(); return }
        switch (root.step++) {
        case 0:
          field.forceActiveFocus()
          keyboard.keyClick(Qt.Key_A, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_B, Qt.NoModifier, 0)
          check(field.text === "ab", "normal typing remains native")
          field.select(0, 1)
          keyboard.keyClick(Qt.Key_V, Qt.ControlModifier, 0)
          break
        case 1:
          check(field.text === "PASTEb" && field.cursorPosition === 5, "Ctrl+V replaces selection through bounded reader despite instance key handler")
          keyboard.keyClick(Qt.Key_Z, Qt.ControlModifier, 0)
          check(field.text === "ab", "one Undo restores text replaced by paste")
          keyboard.keyClick(Qt.Key_Z, Qt.ControlModifier | Qt.ShiftModifier, 0)
          check(field.text === "PASTEb", "Redo restores bounded paste")
          field.text = ""; field.cursorPosition = 0
          keyboard.keyClick(Qt.Key_Insert, Qt.ShiftModifier, 0)
          keyboard.keyClick(Qt.Key_Insert, Qt.ShiftModifier, 0)
          break
        case 2:
          check(field.text === "PASTE", "Shift+Insert uses bounded reader and ignores duplicate requests")
          field.text = ""
          field.Controls.ContextMenu.menu.popup()
          break
        case 3:
          var menu = field.Controls.ContextMenu.menu
          check(menu.itemAt(5).objectName === "boundedPasteAction", "context menu replaces native PasteAction")
          menu.itemAt(5).triggered(); menu.close()
          break
        case 4:
          check(field.text === "PASTE", "menu paste uses bounded reader")
          field.text = ""
          keyboard.mouseClick(field, 15, field.height / 2, Qt.MiddleButton, Qt.NoModifier, 0)
          break
        case 5:
          check(field.text === "PRIMARY", "middle-click uses bounded PRIMARY reader")
          configure({text: "x".repeat(65537)})
          break
        case 6:
          field.text = "keep"; field.selectAll(); field.requestPaste(false)
          break
        case 7:
          check(field.text === "keep" && field.pasteError.indexOf("too large") >= 0, "oversized paste leaves selection and text unchanged")
          configure({mode: "trickle"})
          break
        case 8:
          field.requestPaste(false)
          break
        case 9:
          check(field.text === "keep" && field.pasteError.indexOf("timed out") >= 0, "continuous slow provider hits total deadline without freezing QML")
          configure({text: "late", delay: 0.4})
          break
        case 10:
          field.requestPaste(false); field.pasteContext = "conversation-b"
          break
        case 11:
          check(field.text === "keep", "conversation change cancels pending paste")
          field.requestPaste(false); field.text = "new draft"
          break
        case 12:
          check(field.text === "new draft", "typing cancels pending paste")
          field.requestPaste(false); other.forceActiveFocus()
          break
        case 13:
          check(field.text === "new draft" && other.text === "", "focus change cancels pending paste despite instance focus handler")
          field.forceActiveFocus(); field.requestPaste(false); field.enabled = false
          break
        case 14:
          check(field.text === "new draft", "disabling cancels pending paste despite instance enabled handler")
          field.enabled = true; field.forceActiveFocus(); field.requestPaste(false); field.visible = false
          break
        case 15:
          check(field.text === "new draft", "hiding cancels pending paste")
          field.visible = true; field.forceActiveFocus(); field.requestPaste(false); field.readOnly = true
          break
        case 16:
          check(field.text === "new draft", "read-only change cancels pending paste")
          field.readOnly = false; field.forceActiveFocus(); field.maximumLength = 5
          field.text = "keep"; field.selectAll()
          configure({text: "too long"})
          break
        case 17:
          field.requestPaste(false)
          break
        case 18:
          check(field.text === "keep" && field.pasteError.indexOf("too long") >= 0, "field limit rejects whole paste without destructive truncation")
          field.pasteAllowed = false
          keyboard.keyClick(Qt.Key_V, Qt.ControlModifier, 0)
          check(!field.pasteBusy && field.text === "keep", "disabled paste is consumed without native fallback")
          console.log("OMACHAT_CLIPBOARD_PASS")
          Qt.quit(); return
        }
        restart()
      } catch (error) { console.error("OMACHAT_CLIPBOARD_FAIL", error); Qt.quit() }
    }
  }
}
