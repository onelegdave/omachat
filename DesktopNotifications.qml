import QtQuick
import "Notifications.js" as Policy

Item {
  id: root
  property var service: null
  property bool connected: false
  property bool loaded: false
  property bool enabled: false
  // A helper built before 0.5.4 may still have message previews on; alerts wait
  // until it confirms they are off. Previews were removed because notification
  // servers can put alert text in command lines other local accounts can read.
  property bool legacyPreviews: false
  property bool saving: false
  property string errorText: ""
  property double connectedAt: 0
  property var seen: []
  property var views: []
  property var queue: []
  property bool sending: false
  property int generation: 0
  property int configRevision: 0
  property int sendRequest: 0
  property var networkEpochs: ({})

  function retireConnection() {
    generation++
    configRevision++
    sendRequest++
    loaded = false
    enabled = false
    legacyPreviews = false
    saving = false
    sending = false
    queue = []
    errorText = ""
    if (connected) connectedAt = Date.now() * 1000 - 1000000
  }
  onConnectedChanged: retireConnection()
  onServiceChanged: { retireConnection(); seen = [] }

  function applyConfig(config, revision) {
    if (revision !== undefined && revision !== configRevision) return false
    if (saving || !config || typeof config.notificationsEnabled !== "boolean") return false
    loaded = true
    enabled = config.notificationsEnabled
    legacyPreviews = config.notificationPreviews === true
    if (!enabled) queue = []
    if (legacyPreviews) savePreferences({previews: false})
    return true
  }

  function savePreferences(values) {
    if (!service || !connected || !loaded || saving) return
    var source = service
    var lifetime = generation
    configRevision++
    saving = true
    errorText = ""
    service.call("setNotifications", values, function(ok, res) {
      if (source !== root.service || lifetime !== root.generation) return
      root.saving = false
      if (!ok) root.errorText = String(res)
      else if (!root.applyConfig(res)) root.errorText = "Notification preferences could not be confirmed. Refresh Settings before retrying."
      root.flush()
    }, "gmessages")
  }

  function test() {
    if (!service || !connected || !enabled) return
    var source = service
    var lifetime = generation
    errorText = ""
    service.call("testNotification", null, function(ok, res) {
      if (source !== root.service || lifetime !== root.generation) return
      root.errorText = ok ? "" : String(res)
    }, "gmessages")
  }

  function registerView(view) {
    if (views.indexOf(view) < 0) views = views.concat([view])
  }

  function forgetNetwork(network) {
    var epochs = Object.assign({}, networkEpochs)
    epochs[network] = (epochs[network] || 0) + 1
    networkEpochs = epochs
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
    queue = queue.concat([{network:network, params:{message:message}}]).slice(-32)
    flush()
  }

  function flush() {
    if (sending || legacyPreviews || !queue.length || !enabled || !connected || !service) return
    var next = queue[0]
    queue = queue.slice(1)
    if (views.some(function(view) { return view && view.isReadingConversation(next.network, next.params.message.conversationID) })) {
      Qt.callLater(flush)
      return
    }
    var source = service
    var lifetime = generation
    var epoch = networkEpochs[next.network] || 0
    var request = ++sendRequest
    sending = true
    service.call("notifyMessage", next.params, function(ok, res) {
      if (source !== root.service || lifetime !== root.generation || request !== root.sendRequest) return
      root.sending = false
      if (epoch === (root.networkEpochs[next.network] || 0)) root.errorText = ok ? "" : String(res)
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
