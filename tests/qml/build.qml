import QtQuick
import QtTest
import Quickshell
import Quickshell.Io
import "Chat" as Chat
import "Build" as Build

ShellRoot {
 id: root
 property bool requested: false
 property bool refreshRequested: false
 Build.Service { id: service }
 Chat.Panel { id: panel; service: service }
 TestResult { id: inspect }
 Process {
  command: ["sleep", "0.1"]
  running: true
  onExited: {
   if (!root.requested && service.goPresent && service.helperState === "missing") {
    var button=inspect.findChild(panel,"buildHelperButton")
    if (!button || !button.enabled) { console.error("OMACHAT_BUILD_FAIL missing button");Qt.quit();return }
    root.requested=true
    button.clicked()
   }
   if (root.requested && service.helperError) {
    console.error("OMACHAT_BUILD_FAIL",service.helperError);Qt.quit();return
   }
   if (root.requested && service.connected && service.state === "unpaired") {
    if (!root.refreshRequested) {
     root.refreshRequested=true
     service.refreshConversations()
     running=true;return
    }
    if (service.refreshing) { running=true;return }
    if (service.refreshError.indexOf("not connected to Google Messages") < 0) {
     console.error("OMACHAT_BUILD_FAIL refresh did not reach daemon",service.refreshError);Qt.quit();return
    }
    service.stopHelper()
    console.log("OMACHAT_BUILD_PASS compiled helper, connected, and checked refresh RPC against isolated unpaired daemon")
    Qt.quit();return
   }
   running=true
  }
 }
}
