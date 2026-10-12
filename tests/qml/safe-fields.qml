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
  property int acceptedCount: 0
  property int finishedCount: 0
  property int secretAccepted: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  // Put text on Qt's in-process offscreen clipboard, never the desktop's.
  function seedNative(value) { nativeProbe.text = value; nativeProbe.selectAll(); nativeProbe.copy(); nativeProbe.text = "" }
  function editors(item, found) {
    found = found || []
    if (item.echoMode !== undefined || (item.textFormat !== undefined && item.cursorRectangle !== undefined)) found.push(item)
    for (var i = 0; i < item.children.length; i++) editors(item.children[i], found)
    return found
  }
  // QTBUG-149610: an editable TextInput reads the clipboard on every change.
  function noEditableTextInput(item) {
    var all = editors(item)
    for (var i = 0; i < all.length; i++)
      if (all[i].echoMode !== undefined && !all[i].readOnly) return false
    return all.length > 0
  }
  // Earlier fixtures in the same run may leave other provider settings.
  FileView { id: provider; path: Quickshell.env("OMACHAT_CLIPBOARD_FIXTURE"); atomicWrites: true }
  Component.onCompleted: provider.setText(JSON.stringify({text: "PASTE", primary: "PRIMARY", delay: 0.1}))
  FloatingWindow {
    id: window
    implicitWidth: 600; implicitHeight: 260; visible: true
    Chat.SafeTextField {
      id: field
      width: 200; x: 20; y: 20
      onAccepted: root.acceptedCount++
      onEditingFinished: root.finishedCount++
    }
    Chat.SafeTextField { id: next; width: 200; x: 20; y: 70 }
    Chat.SafeTextField { id: message; width: 200; x: 20; y: 120; allowLineBreaks: true }
    Chat.SafePasswordField {
      id: secret
      width: 200; x: 260; y: 20
      maximumLength: 8
      onAccepted: root.secretAccepted++
    }
    // Unguarded control that shows what Qt's native paste would insert.
    Controls.TextField { id: nativeProbe; width: 200; x: 260; y: 120 }
    TestEvent { id: keyboard }
  }
  Timer {
    running: true; repeat: true; interval: 140
    onTriggered: {
      stop()
      try {
        if (++root.ticks > 100) throw new Error("fixture timed out")
        if (field.pasteBusy || secret.pasteBusy) { restart(); return }
        switch (root.step++) {
        case 0:
          check(noEditableTextInput(field) && noEditableTextInput(message), "text fields contain no editable TextInput")
          check(noEditableTextInput(secret) && secret.readOnly, "password field stays a read-only TextInput")
          var editor = editors(field)[0]
          check(editor.textFormat === TextEdit.PlainText, "field edits plain text only")
          field.forceActiveFocus()
          keyboard.keyClick(Qt.Key_A, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_B, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_Return, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_Return, Qt.ShiftModifier, 0)
          check(field.text === "ab" && root.acceptedCount === 2 && root.finishedCount >= 2, "Return accepts like TextInput and never inserts a line break")
          keyboard.keyClick(Qt.Key_Tab, Qt.NoModifier, 0)
          check(next.activeFocus && field.text === "ab", "Tab moves focus instead of inserting a tab")
          keyboard.keyClick(Qt.Key_Backtab, Qt.ShiftModifier, 0)
          check(field.activeFocus, "Shift+Tab moves focus back")
          field.maximumLength = 3; field.cursorPosition = 1
          keyboard.keyClick(Qt.Key_C, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_D, Qt.NoModifier, 0)
          check(field.text === "acb", "typing stops at maximumLength without dropping existing text")
          field.maximumLength = 32767
          field.text = "one\ntwo\tthree"
          check(field.text === "one two three", "assigned line breaks and tabs flatten in single-line fields")
          message.text = "one\ntwo"
          check(message.text === "one\ntwo", "message fields keep assigned line breaks")
          field.text = "x".repeat(200); field.cursorPosition = field.length
          break
        case 1:
          var view = editors(field)[0]
          var cursorX = view.cursorRectangle.x + view.x
          check(view.x < 0 && cursorX >= 0 && cursorX <= view.parent.width, "long text scrolls to keep the cursor visible")
          keyboard.keyClick(Qt.Key_Home, Qt.NoModifier, 0)
          check(field.cursorPosition === 0 && view.x === 0, "Home scrolls back to the start")
          secret.forceActiveFocus()
          keyboard.keyClick(Qt.Key_A, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_B, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_C, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_Left, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_Backspace, Qt.NoModifier, 0)
          check(secret.text === "ac" && secret.cursorPosition === 1 && secret.readOnly, "password field types, moves and deletes while read-only")
          keyboard.keyClick(Qt.Key_Delete, Qt.NoModifier, 0)
          check(secret.text === "a", "Delete removes the next character")
          keyboard.keyClick(Qt.Key_Return, Qt.NoModifier, 0)
          check(root.secretAccepted === 1, "Return accepts the password field")
          for (var i = 0; i < 10; i++) keyboard.keyClick(Qt.Key_Z, Qt.NoModifier, 0)
          check(secret.text.length === 8, "password field stops at maximumLength")
          secret.text = ""
          root.seedNative("NATIVE_SENTINEL")
          keyboard.keyClick(Qt.Key_V, Qt.ControlModifier, 0)
          break
        case 2:
          check(secret.text === "PASTE", "Ctrl+V in the password field uses the bounded reader, not native paste")
          secret.text = ""
          keyboard.mouseClick(secret, 15, secret.height / 2, Qt.MiddleButton, Qt.NoModifier, 0)
          break
        case 3:
          check(secret.text === "PRIMARY", "middle-click in the password field uses the bounded PRIMARY reader")
          secret.text = "keep"; secret.selectAll()
          secret.Controls.ContextMenu.menu.popup()
          break
        case 4:
          var menu = secret.Controls.ContextMenu.menu
          check(menu.itemAt(0).objectName === "boundedPasteAction", "password context menu offers only bounded paste")
          menu.itemAt(0).triggered(); menu.close()
          break
        case 5:
          check(secret.text === "PASTE", "password menu paste replaces the selection through the bounded reader")
          console.log("OMACHAT_SAFE_FIELDS_PASS")
          Qt.quit(); return
        }
        restart()
      } catch (error) { console.error("OMACHAT_SAFE_FIELDS_FAIL", error); Qt.quit() }
    }
  }
}
