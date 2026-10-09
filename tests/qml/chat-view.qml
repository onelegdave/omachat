import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

// Fictional conversations and an in-memory settings store; no account access.
ShellRoot {
  id: root
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }

  QtObject {
    id: fake
    property bool connected: true
    property string currentNetwork: "gmessages"
    property var enabledServices: ["gmessages", "telegram"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string refreshError: ""
    property var conversations: [{id:"demo-a", name:"Demo Google"}]
    property var conversationsTG: []
    property var status: ({state:"connected", phoneOK:true})
    property var saved: ({lastService:"telegram", sidebarCollapsed:true, lastConversations:{telegram:"demo-b"}})
    property var pending: []
    property var messageRequests: []
    signal paired(string network)
    function stateFor(net) { return "connected" }
    function statusFor(net) { return status }
    function unreadFor(net) { return 0 }
    function conversationsFor(net) { return net === "telegram" ? conversationsTG : conversations }
    function loadConversations(net) {}
    function call(method, params, callback, net) {
      if (method === "config") {
        callback(true, Object.assign({uiScale:1, enabledServices:enabledServices}, JSON.parse(JSON.stringify(saved))))
      } else if (method === "setChatView") {
        pending.push({params:params, network:net, callback:callback})
      } else if (method === "messages") {
        messageRequests.push({network:net, id:params.conversationID})
        callback(true, {messages:[], hasMore:false})
      }
    }
    function drain() {
      while (pending.length) {
        var change = pending.shift()
        var params = change.params
        if (params.lastService !== undefined) saved.lastService = params.lastService
        if (params.sidebarCollapsed !== undefined) saved.sidebarCollapsed = params.sidebarCollapsed
        if (params.conversationID !== undefined) {
          if (params.conversationID) saved.lastConversations[change.network] = params.conversationID
          else delete saved.lastConversations[change.network]
        }
        change.callback(true, null)
      }
    }
  }

  Loader { id: panelLoader; sourceComponent: Component { Chat.Panel { service:fake; manageIpc:false } } }
  TestResult { id: inspect }
  TestEvent { id: keyboard }
  Timer {
    interval: 180
    running: true
    repeat: false
    onTriggered: {
      try {
        var panel = panelLoader.item
        var loader = panel ? inspect.findChild(panel, "inboxLoader") : null
        var inbox = loader ? loader.item : null
        var composer = inbox ? inspect.findChild(inbox, "composer") : null
        var messageList = inbox ? inspect.findChild(inbox, "messageList") : null
        var conversationList = inbox ? inspect.findChild(inbox, "convList") : null
        var sidebar = inbox ? inspect.findChild(inbox, "conversationSidebar") : null
        var thread = inbox ? inspect.findChild(inbox, "threadPane") : null
        if (root.step === 0) {
          panel.open()
        } else if (root.step === 1) {
          check(panel.activeService === "telegram", "opening restores the saved service")
          check(inbox.selectedConvID === "" && sidebar.visible, "sidebar stays reachable while conversations load")
          check(fake.messageRequests.length === 0, "restoration waits for a real conversation")
          fake.conversationsTG = [{id:"demo-a", name:"Demo Alice"}, {id:"demo-b", name:"Demo Bob"}]
        } else if (root.step === 2) {
          check(inbox.selectedConvID === "demo-b", "late conversation list restores the last chat")
          check(!sidebar.visible && Math.abs(thread.width - inbox.width) < 1, "collapsed sidebar gives the thread full width")
          check(!composer.activeFocus && messageList.activeFocus, "collapsed restored chat opens on its message list, not the composer")
          inspect.findChild(panel, "sidebarToggleButton").clicked()
          conversationList.currentIndex = 1
          conversationList.forceActiveFocus()
          keyboard.keyClick(Qt.Key_Return, Qt.NoModifier, 0)
          check(composer.activeFocus, "Enter from the retained conversation list focuses the composer")
          keyboard.keyClick(Qt.Key_R, Qt.NoModifier, 0)
          keyboard.keyClick(Qt.Key_1, Qt.NoModifier, 0)
          check(composer.text === "r1" && panel.activeService === "telegram", "typing immediately does not trigger panel shortcuts")
        } else if (root.step === 3) {
          check(sidebar.visible && thread.width < inbox.width, "toggle expands the sidebar")
          inspect.findChild(inbox, "searchField").forceActiveFocus()
          fake.conversationsTG = fake.conversationsTG.concat([{id:"demo-c", name:"Demo Carol"}])
        } else if (root.step === 4) {
          check(inspect.findChild(inbox, "searchField").activeFocus, "background list updates do not steal search focus")
          panel.close()
        } else if (root.step === 5) {
          panel.open()
        } else if (root.step === 6) {
          check(inbox.selectedConvID === "demo-b" && conversationList.activeFocus && composer.text === "r1", "reopening keeps the chat and draft with list focus")
          panel.openPopout()
        } else if (root.step === 7) {
          check(panel.contentInPopout && conversationList.activeFocus, "pop-out keeps conversation-list focus")
          panel.returnToPanel()
        } else if (root.step === 8) {
          check(panel.opened && conversationList.activeFocus, "returning to the panel keeps conversation-list focus")
          inbox.selectConversation("demo-a")
          panel.setActiveService("gmessages")
        } else if (root.step === 9) {
          inbox.selectConversation("demo-a")
          check(fake.pending.length === 1 && panel.pendingChatViewChanges.length > 0, "preference writes are serialized")
          fake.drain()
          check(fake.saved.lastConversations.telegram === "demo-a" && fake.saved.lastConversations.gmessages === "demo-a", "same conversation IDs remain isolated by service")
          panel.toggleSidebar()
          fake.drain()
          panel.close()
          panelLoader.active = false
        } else if (root.step === 10) {
          panelLoader.active = true
        } else if (root.step === 11) {
          panel.open()
        } else if (root.step === 12) {
          check(panel.activeService === "gmessages" && inbox.selectedConvID === "demo-a", "new panel restores saved service and selection")
          check(panel.sidebarCollapsed && !sidebar.visible && messageList.activeFocus, "new panel restores layout and thread focus")
          panel.setActiveService("telegram")
        } else if (root.step === 13) {
          check(inbox.selectedConvID === "demo-a", "service switching restores its own last conversation")
          fake.paired("gmessages")
          fake.drain()
          check(!fake.saved.lastConversations.gmessages && fake.saved.lastConversations.telegram === "demo-a", "account reset forgets only its own conversation")
          panel.settingsOpen = true
        } else if (root.step === 14) {
          check(!loader.visible && !composer.activeFocus, "Settings does not leave the hidden composer focused")
          panel.settingsOpen = false
        } else if (root.step === 15) {
          check(conversationList.activeFocus || messageList.activeFocus, "returning from Settings restores non-editor focus")
          fake.conversationsTG = [{id:"demo-a", name:"Demo read-only", readOnly:true}]
        } else if (root.step === 16) {
          check(!composer.enabled && !composer.activeFocus, "read-only conversations cannot receive typing focus")
          panel.close()
          panelLoader.active = false
          fake.saved.lastConversations.telegram = "missing-chat"
        } else if (root.step === 17) {
          panelLoader.active = true
        } else if (root.step === 18) {
          panel.open()
        } else if (root.step === 19) {
          check(inbox.selectedConvID === "" && sidebar.visible, "missing saved chat leaves the chooser accessible")
          check(!fake.messageRequests.some(function(req) { return req.id === "missing-chat" }), "missing saved chat never opens a phantom recipient")
          panel.close()
          console.log("OMACHAT_CHAT_VIEW_PASS")
          stop()
          Qt.quit()
          return
        }
        root.step++
        restart()
      } catch (error) {
        console.error("OMACHAT_CHAT_VIEW_FAIL", error)
        stop()
        Qt.quit()
      }
    }
  }
}
