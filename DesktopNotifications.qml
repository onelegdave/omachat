import QtQuick
import "Notifications.js" as Policy

Item {
  id: root
  property var service: null
  property bool connected: false
  property bool loaded: false
  property bool enabled: false
  property bool previews: false
  property bool saving: false
  property string errorText: ""
  property double connectedAt: 0
  property var seen: []
  property var views: []
  property var queue: []
  property bool sending: false

  onConnectedChanged: {
    if (connected) { connectedAt = Date.now() * 1000 - 1000000; errorText = "" }
    else queue = []
  }

  function applyConfig(config) {
    if (saving || !config || typeof config.notificationsEnabled !== "boolean" || typeof config.notificationPreviews !== "boolean") return false
    loaded = true
    enabled = config.notificationsEnabled
    previews = config.notificationPreviews
    if (!enabled) queue = []
    return true
  }

  function savePreferences(values) {
    if (!service || !connected || !loaded || saving) return
    saving = true
    errorText = ""
    service.call("setNotifications", values, function(ok, res) {
      root.saving = false
      if (ok) root.applyConfig(res)
      else root.errorText = String(res)
    }, "gmessages")
  }

  function test() {
    if (!service || !connected || !enabled) return
    errorText = ""
    service.call("testNotification", null, function(ok, res) {
      root.errorText = ok ? "" : String(res)
    }, "gmessages")
  }

  function registerView(view) {
    if (views.indexOf(view) < 0) views = views.concat([view])
  }

  function forgetNetwork(network) {
    seen = seen.filter(function(key) { return JSON.parse(key)[0] !== network })
    queue = queue.filter(function(item) { return item.network !== network })
  }

  function unregisterView(view) {
    views = views.filter(function(candidate) { return candidate !== view })
  }

  function handleMessage(message, network, live) {
    if (!live || !Policy.isIncoming(message, connectedAt, Date.now() * 1000)) return
    var key = Policy.messageKey(network, message)
    if (seen.indexOf(key) >= 0) return
    seen = seen.concat([key]).slice(-512)
    if (!loaded || !enabled || !connected || !service || !service.isServiceEnabled(network)
        || service.stateFor(network) !== "connected") return
    if (views.some(function(view) { return view && view.isReadingConversation(network, message.conversationID) })) return
    var name = ""
    var conversations = service.conversationsFor(network)
    for (var i = 0; i < conversations.length; i++) {
      if (conversations[i].id === message.conversationID) { name = conversations[i].name || ""; break }
    }
    queue = queue.concat([{network:network, params:{message:message, conversationName:name}}]).slice(-32)
    flush()
  }

  function flush() {
    if (sending || !queue.length || !enabled || !connected || !service) return
    var next = queue[0]
    queue = queue.slice(1)
    if (views.some(function(view) { return view && view.isReadingConversation(next.network, next.params.message.conversationID) })) {
      Qt.callLater(flush)
      return
    }
    sending = true
    service.call("notifyMessage", next.params, function(ok, res) {
      root.sending = false
      root.errorText = ok ? "" : String(res)
      root.flush()
    }, next.network)
  }

  function openConversation(target) {
    if (!target || !views.length) return
    if (target.conversationID && (!service || !service.isServiceEnabled(target.network)
        || service.stateFor(target.network) === "unpaired")) return
    var view = views.filter(function(candidate) { return candidate && candidate.anySurfaceOpen })[0] || views[0]
    if (!view) return
    if (target.conversationID) view.showConversation(target.network, target.conversationID)
    else view.showNotificationSettings()
  }
}
