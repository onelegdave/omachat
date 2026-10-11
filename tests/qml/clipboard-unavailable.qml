import QtQuick
import Quickshell
import "Chat" as Chat

ShellRoot {
  FloatingWindow {
    implicitWidth: 500; implicitHeight: 100; visible: true
    Chat.SafeTextField { id: field; width: 450; text: "keep" }
  }
  Timer {
    interval: 100; running: true
    onTriggered: { field.forceActiveFocus(); field.selectAll(); field.requestPaste(false) }
  }
  Timer {
    interval: 3800; running: true
    onTriggered: {
      if (field.pasteBusy || field.text !== "keep" || field.pasteError.indexOf("Python 3") < 0)
        console.error("OMACHAT_CLIPBOARD_UNAVAILABLE_FAIL missing Python must fail closed with visible guidance")
      else console.log("OMACHAT_CLIPBOARD_UNAVAILABLE_PASS")
      Qt.quit()
    }
  }
}
