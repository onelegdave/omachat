import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0
  property int callsBefore: 0
  function check(ok, label) {
    if (!ok) throw new Error(label)
    console.log("PASS:", label)
  }
  function readCalls() {
    return fake.calls.filter(function(c) { return c.method === "markRead" })
  }
  QtObject {
    id: fake
    property bool connected: true
    property string state: "connected"
    property string refreshError: ""
    property var calls: []
    property var pending: []
    signal messageReceived(var message, string network)
    property var conversations: [
      {id:"demo-read",name:"Read thread",timestamp:3,unread:false},
      {id:"demo-alpha",name:"Empty alpha",timestamp:2,unread:true},
      {id:"demo-beta",name:"Empty beta",timestamp:1,unread:true}
    ]
    function stateFor(net) { return state }
    function conversationsFor(net) { return conversations }
    function loadConversations(net) {}
    function call(method, params, callback, network) {
      calls.push({method:method,params:params,network:network})
      if (method === "markRead") { pending.push(callback); return }
      if (method === "messages") callback(true,{messages:[]})
    }
  }
  FloatingWindow {
    id: window
    implicitWidth: 920
    implicitHeight: 580
    visible: true
    Item { id: host; property bool opened: true }
    Chat.InboxView {
      id: inbox
      anchors.fill: parent
      host: host
      service: fake
      network: "messenger"
    }
    TestEvent { id: mouse }
    TestResult { id: inspect }
  }
  Timer {
    interval: 100
    running: true
    repeat: true
    onTriggered: {
      try {
        var search = inspect.findChild(inbox, "searchField")
        var unread = inspect.findChild(inbox, "nextUnreadButton")
        var list = inspect.findChild(inbox, "convList")
        if (root.step === 0) {
          inbox.selectConversation("demo-alpha")
          root.check(root.readCalls().length === 1 && root.readCalls()[0].params.messageID === "",
            "empty unread Messenger thread uses conversation acknowledgement")
          fake.pending.pop()(false,"Current read failed")
          root.check(inbox.threadError === "Current read failed", "current read failure remains visible")
          inbox.markThreadRead()
          var old = fake.pending.pop()
          inbox.selectConversation("demo-beta")
          old(false,"Old thread failed")
          root.check(inbox.threadError === "", "late read failure cannot leak into another thread")
          fake.pending = []
          inbox.selectConversation("demo-alpha")
          old = fake.pending.pop()
          inbox.selectConversation("demo-beta")
          inbox.selectConversation("demo-alpha")
          old(false,"Old selection failed")
          root.check(inbox.threadError === "", "returning to the same thread does not revive an old read callback")
          old = fake.pending.pop()
          inbox.network = "telegram"
          inbox.selectConversation("demo-alpha")
          old(false,"Old service failed")
          root.check(inbox.threadError === "", "same-ID thread in another service ignores stale read errors")
          root.check(root.readCalls().slice(-1)[0].network === "telegram", "empty unread Telegram thread is acknowledged")
          fake.pending = []
          inbox.network = "whatsapp"
          inbox.selectConversation("demo-alpha")
          root.callsBefore = root.readCalls().length
          inbox.markThreadRead()
          root.check(root.readCalls().length === root.callsBefore, "empty WhatsApp thread is not acknowledged without a message")
          inbox.network = "gmessages"
          inbox.selectConversation("demo-alpha")
          inbox.messages = [{id:"local-pending",provisional:true}]
          inbox.markThreadRead()
          root.check(root.readCalls().length === root.callsBefore, "Google read acknowledgement never uses a provisional message")
          inbox.network = "messenger"
          inbox.selectConversation("demo-read")
          inbox.messages = [{id:"real-message"}]
          root.callsBefore = root.readCalls().length
          inbox.markThreadRead()
          root.check(root.readCalls().length === root.callsBefore, "already-read thread does not issue another acknowledgement")
          fake.messageReceived({id:"live-incoming",conversationID:"demo-read",text:"Synthetic incoming message",timestamp:10,fromMe:false}, "messenger")
          root.check(root.readCalls().length === root.callsBefore + 1
            && root.readCalls().slice(-1)[0].params.messageID === "live-incoming",
            "visible live message is acknowledged before its unread snapshot arrives")
          search.text = "Read thread"
          root.check(inbox.visibleConversations.length === 1, "search filters the list before next unread")
          root.check(!!unread, "next unread action is exposed for interaction")
          mouse.mouseClick(unread, unread.width / 2, unread.height / 2, Qt.LeftButton, Qt.NoModifier, 0)
          root.step++
        } else if (root.step === 1) {
          root.check(search.text === "" && inbox.selectedConvID === "demo-alpha" && list.currentIndex === 1,
            "next unread clears search and selects the correct visible row")
          root.callsBefore = root.readCalls().length
          mouse.mouseClick(unread, unread.width / 2, unread.height / 2, Qt.LeftButton, Qt.NoModifier, 0)
          root.step++
        } else if (root.step === 2) {
          root.check(inbox.selectedConvID === "demo-alpha" && root.readCalls().length === root.callsBefore + 1,
            "next unread retries an already-selected empty unread thread")
          inbox.network = "gmessages"
          inbox.selectConversation("demo-beta")
          inbox.network = "messenger"
          inbox.selectConversation("demo-read")
          mouse.mouseClick(unread, unread.width / 2, unread.height / 2, Qt.LeftButton, Qt.NoModifier, 0)
          inbox.network = "gmessages"
          root.step++
        } else {
          root.check(inbox.selectedConvID === "demo-beta", "deferred unread action cannot move selection in another service")
          console.log("OMACHAT_UNREAD_PASS")
          Qt.quit()
        }
      } catch (error) {
        console.log("OMACHAT_UNREAD_FAIL", error)
        Qt.quit()
      }
    }
  }
}
