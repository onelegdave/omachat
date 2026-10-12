import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

// Unpair must stay reachable whenever a pairing still exists, including after
// the phone stops answering. Fictional data only, no account access.
ShellRoot {
  id: root
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function useState(value) {
    fake.state = value
    fake.status = {state: value, phoneOK: value === "connected"}
  }
  QtObject {
    id: fake
    property bool connected: true
    property string currentNetwork: "gmessages"
    property var enabledServices: ["gmessages", "whatsapp"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string servicesError: ""
    property string refreshError: ""
    property bool refreshing: false
    property string state: "connected"
    property var browserProfiles: []
    property var status: ({state:"connected", phoneOK:true})
    property var requests: []
    signal messageReceived(var message, var net)
    signal conversationUpdated(var conversation, var net)
    signal paired(var net)
    function statusFor(net) { return status }
    function stateFor(net) { return state }
    function conversationsFor(net) { return [] }
    function unreadFor(net) { return 0 }
    function loadConversations(net) {}
    function refreshConversations(net) {}
    function loadProfiles() {}
    function took(method) { return requests.filter(function(r) { return r.method === method }) }
    function call(method, params, callback, network) {
      requests.push({method: method, network: network})
      if (!callback) return
      if (method === "config") callback(true, {uiScale: 1, enabledServices: enabledServices})
      else if (method === "messages") callback(true, {hasMore: false, messages: []})
      else callback(true, null)
    }
  }
  Chat.Panel { id: panel; service: fake; manageIpc: false }
  TestResult { id: inspect }
  Timer {
    interval: 200; running: true; repeat: true
    onTriggered: {
      try {
        if (root.step++ === 0) { panel.open(); return }
        var button = inspect.findChild(panel, "unpairButton")
        check(button !== null, "panel header contains the unpair button")

        // A stalled pairing still exists and is exactly what needs revoking.
        var revocable = ["connected", "connecting", "disconnected"]
        for (var i = 0; i < revocable.length; i++) {
          root.useState(revocable[i])
          check(button.visible, "unpair stays reachable while " + revocable[i])
        }
        // Nothing to revoke, or the pairing view already offers the next step.
        var hidden = ["unpaired", "pairing", "gaiaPairing", "error"]
        for (var j = 0; j < hidden.length; j++) {
          root.useState(hidden[j])
          check(!button.visible, "unpair is hidden while " + hidden[j])
        }

        root.useState("disconnected")
        fake.requests = []
        button.clicked()
        check(fake.took("unpair").length === 1, "unpair button revokes a stalled pairing")
        check(fake.took("unpair")[0].network === "gmessages", "unpair targets the active service")
        console.log("OMACHAT_UNPAIR_PASS")
        stop(); Qt.quit()
      } catch (e) { console.error("OMACHAT_UNPAIR_FAIL", e); stop(); Qt.quit() }
    }
  }
  Timer { running: true; interval: 6000; onTriggered: { console.error("OMACHAT_UNPAIR_FAIL timeout"); Qt.quit() } }
}
