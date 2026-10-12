import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  Chat.DesktopNotifications { id: alerts; service: fake; connected: fake.connected }
  Chat.NotificationSettings { id: controls; service: fake; width: 600 }
  Chat.SettingsView { id: settings; service: fake; width: 600; height: 700 }
  QtObject {
    id: fake
    property bool connected: true
    property alias notifications: alerts
    property var requests: []
    function isServiceEnabled(net) { return net === "telegram" }
    function stateFor(net) { return "connected" }
    function conversationsFor(net) { return [{id:"demo-chat",name:"Demo chat"}] }
    function call(method, params, callback, network) { requests.push({method:method,params:params,callback:callback,network:network}) }
    function last(method) {
      var found = requests.filter(function(request) { return request.method === method })
      return found[found.length - 1]
    }
  }
  QtObject {
    id: view
    property bool anySurfaceOpen: false
    property bool reading: false
    property string openedID: ""
    function isReadingConversation(net, id) { return reading && net === "telegram" && id === "demo-chat" }
    function showConversation(net, id) { openedID = net + ":" + id }
    function showNotificationSettings() { openedID = "settings" }
  }
  TestResult { id: inspect }
  Timer {
    interval: 100; running: true
    onTriggered: {
      try {
        alerts.applyConfig({notificationsEnabled:true})
        alerts.registerView(view)
        check(!inspect.findChild(controls, "notificationPreviewsToggle"), "settings offer no message preview option")
        var toggle = inspect.findChild(controls, "desktopNotificationsToggle")
        toggle.clicked()
        var save = fake.last("setNotifications")
        check(save.params.enabled === false && alerts.saving && alerts.enabled, "notification control waits for helper acknowledgement")
        fake.connected = false
        check(!alerts.loaded && !alerts.saving && !toggle.enabled, "disconnect retires pending notification preferences and disables controls")
        fake.connected = true
        alerts.applyConfig({notificationsEnabled:true})
        save.callback(true,{notificationsEnabled:false})
        check(alerts.enabled && !alerts.errorText, "same-service reconnect rejects the old acknowledgement")
        // A helper built before 0.5.4 still reports and applies its preview setting.
        alerts.applyConfig({notificationsEnabled:true,notificationPreviews:true})
        var retire = fake.last("setNotifications")
        check(retire !== save && retire.params.previews === false && alerts.legacyPreviews, "an outdated helper is told to turn previews off")
        var message = {id:"demo-new",conversationID:"demo-chat",text:"Synthetic incoming message",timestamp:Date.now()*1000}
        alerts.handleMessage(message,"telegram",false)
        check(!fake.last("notifyMessage"), "history never requests a desktop alert")
        alerts.handleMessage(message,"telegram",true)
        check(!fake.last("notifyMessage"), "alerts wait until the outdated helper confirms previews are off")
        retire.callback(true,{notificationsEnabled:true,notificationPreviews:false})
        var sent = fake.last("notifyMessage")
        check(sent && sent.network === "telegram" && alerts.sending && !alerts.legacyPreviews, "live message queues a notification on its own service")
        check(JSON.stringify(Object.keys(sent.params)) === '["message"]', "the panel sends no conversation name for alerts")
        alerts.forgetNetwork("telegram")
        sent.callback(false,"Retired account failure")
        check(!alerts.sending && !alerts.errorText, "unpairing retires notification reply errors")
        view.reading = true
        alerts.handleMessage(Object.assign({},message,{id:"demo-reading"}),"telegram",true)
        check(fake.requests.filter(function(request) { return request.method === "notifyMessage" }).length === 1, "the actively read conversation stays quiet")
        alerts.openConversation({network:"telegram",conversationID:"demo-chat"})
        check(view.openedID === "telegram:demo-chat", "notification action preserves service and conversation identity")
        settings.saveWindowPreferences(true,"tiled")
        var windowSave = fake.last("setWindowPreferences")
        fake.connected = false
        check(!settings.savingWindowPreferences, "disconnect releases Settings window preference controls")
        fake.connected = true
        fake.last("config").callback(true,{uiScale:1,alwaysPopout:false,popoutMode:"floating"})
        windowSave.callback(true,{alwaysPopout:true,popoutMode:"tiled"})
        check(!settings.alwaysPopout && settings.popoutMode === "floating", "reconnected Settings ignores a retired window save")
        console.log("OMACHAT_NOTIFICATION_LIFETIMES_PASS")
      } catch(error) { console.error("OMACHAT_NOTIFICATION_LIFETIMES_FAIL",String(error)) }
      Qt.callLater(function() { Qt.quit() })
    }
  }
}
