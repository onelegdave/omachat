import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:",label) }
  function incoming(id, chat) { return {id:id,conversationID:chat || "demo-a",text:"Synthetic notification",timestamp:Date.now()*1000} }
  Chat.DesktopNotifications { id: notifications; service:fake; connected:true }
  QtObject {
    id: fake
    property alias notifications: notifications
    property bool connected: true
    property string currentNetwork: "gmessages"
    property var enabledServices: ["gmessages","telegram"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string refreshError: ""
    property var status: ({state:"connected",phoneOK:true})
    property var conversations: [{id:"demo-a",name:"Demo Alice"},{id:"demo-b",name:"Demo Bob"}]
    property var calls: []
    property var saves: []
    function statusFor(net) { return status }
    function stateFor(net) { return "connected" }
    function isServiceEnabled(net) { return enabledServices.indexOf(net) >= 0 }
    function conversationsFor(net) { return conversations }
    function unreadFor(net) { return 0 }
    function loadConversations(net) {}
    function config() { return {uiScale:1,enabledServices:enabledServices,notificationsEnabled:notifications.enabled,notificationPreviews:notifications.previews} }
    function call(method, params, callback, network) {
      calls.push({method:method,params:params,network:network})
      if (method === "setNotifications") { saves.push(callback); return }
      if (!callback) return
      if (method === "config") callback(true,config())
      else if (method === "messages") callback(true,{messages:[]})
      else callback(true,{})
    }
    function alertCount() { return calls.filter(function(call) { return call.method === "notifyMessage" }).length }
  }
  QtObject {
    id: reading
    property bool active: false
    property bool anySurfaceOpen: false
    function isReadingConversation(net,id) { return active && net === "telegram" && id === "demo-a" }
  }
  Chat.Panel { id: panel; service:fake; manageIpc:false }
  TestResult { id: inspect }
  Timer {
    interval:200
    running:true
    repeat:false
    onTriggered: {
      try {
        if (root.step === 0) {
          notifications.applyConfig({notificationsEnabled:true,notificationPreviews:false})
          notifications.registerView(reading)
          var first = incoming("first")
          notifications.handleMessage(first,"telegram",true)
          notifications.handleMessage(first,"telegram",true)
          notifications.handleMessage(incoming("history"),"telegram",false)
          notifications.handleMessage(Object.assign(incoming("old"),{timestamp:Date.now()*1000-10000000}),"telegram",true)
          notifications.handleMessage(Object.assign(incoming("outgoing"),{fromMe:true}),"telegram",true)
          check(fake.alertCount() === 1,"new incoming message notifies once; history, duplicates, and outgoing messages stay quiet")
          notifications.handleMessage(first,"gmessages",true)
          check(fake.alertCount() === 2,"identical message IDs remain isolated by service")
          reading.active = true
          notifications.handleMessage(incoming("reading"),"telegram",true)
          notifications.handleMessage(incoming("other-chat","demo-b"),"telegram",true)
          check(fake.alertCount() === 3,"only the actively read conversation is suppressed")
          notifications.applyConfig({notificationsEnabled:false,notificationPreviews:false})
          notifications.handleMessage(incoming("disabled"),"telegram",true)
          check(fake.alertCount() === 3,"disabled notifications stay quiet")
          notifications.applyConfig({notificationsEnabled:true,notificationPreviews:false})
          reading.active = false
          notifications.handleMessage(incoming("disabled"),"telegram",true)
          check(fake.alertCount() === 3,"re-enabling does not replay suppressed messages")
          notifications.savePreferences({enabled:false})
          check(notifications.enabled && notifications.saving,"preference toggle waits for the helper acknowledgement")
          fake.saves.pop()(false,"Could not save")
          check(notifications.enabled && !notifications.saving && notifications.errorText === "Could not save","failed preference save preserves the current setting")
          notifications.savePreferences({previews:true})
          fake.saves.pop()(true,{notificationsEnabled:true,notificationPreviews:true})
          check(notifications.previews,"successful preference save applies the acknowledged setting")
          notifications.unregisterView(reading)
          notifications.openConversation({network:"telegram",conversationID:"demo-b"})
        } else if (root.step === 1) {
          var loader = inspect.findChild(panel,"inboxLoader")
          check(panel.opened && panel.activeService === "telegram" && loader.item.selectedConvID === "demo-b","notification click opens the correct service and conversation")
          var before = fake.alertCount()
          notifications.handleMessage(incoming("visible-chat","demo-b"),"telegram",true)
          check(fake.alertCount() === before,"the real focused chat suppresses its notification")
          loader.item.sendingMedia = true
          notifications.openConversation({network:"telegram",conversationID:"demo-a"})
        } else if (root.step === 2) {
          var busyInbox = inspect.findChild(panel,"inboxLoader").item
          check(busyInbox.selectedConvID === "demo-b" && panel.pendingConversationID === "demo-a","notification navigation waits for an in-flight upload")
          busyInbox.sendingMedia = false
        } else if (root.step === 3) {
          check(inspect.findChild(panel,"inboxLoader").item.selectedConvID === "demo-a","notification destination opens after the upload finishes")
          notifications.openConversation({network:"",conversationID:""})
        } else if (root.step === 4) {
          check(panel.settingsOpen && inspect.findChild(panel,"desktopNotificationsToggle"),"test notification click opens notification settings")
          inspect.findChild(panel,"testNotificationButton").clicked()
          check(fake.calls.some(function(call) { return call.method === "testNotification" }),"Settings exposes an explicit test notification")
          panel.close()
          check(notifications.views.length === 1,"notification view registration is deduplicated")
          console.log("OMACHAT_NOTIFICATIONS_PASS")
          Qt.quit()
          return
        }
        root.step++
        restart()
      } catch (error) { console.error("OMACHAT_NOTIFICATIONS_FAIL",error); Qt.quit() }
    }
  }
}
