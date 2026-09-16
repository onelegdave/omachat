import QtQuick
import Quickshell
import Quickshell.Io

// Headless singleton. Owns the protocol helper as a child of omarchy-shell
// and speaks NDJSON to it over a Unix socket. The bar widget looks this up
// with bar.shell.serviceFor("onelegdave.omachat").
Item {
  id: root

  property var shell: null
  property var manifest: null

  readonly property string pluginId: "onelegdave.omachat"
  readonly property string pluginDir: {
    var url = String(Qt.resolvedUrl("."))
    if (url.indexOf("file://") === 0)
      url = decodeURIComponent(url.substring("file://".length))
    if (url.length > 1 && url.charAt(url.length - 1) === "/")
      url = url.substring(0, url.length - 1)
    return url
  }
  readonly property string helperPath: pluginDir + "/bin/omachatd"
  readonly property string socketPath: {
    var runtime = Quickshell.env("XDG_RUNTIME_DIR")
    if (runtime && runtime.length > 0) return runtime + "/omachat/daemon.sock"
    var cache = Quickshell.env("XDG_CACHE_HOME")
    if (cache && cache.length > 0) return cache + "/omachat/daemon.sock"
    return Quickshell.env("HOME") + "/.cache/omachat/daemon.sock"
  }

  property alias updates: updateManager
  UpdateManager { id:updateManager; pluginDir:root.pluginDir }
  property string runningSourceID: ""
  property bool helperBuildChecked: false
  property bool restartingBuild: false
  readonly property bool helperNeedsRebuild: helperBuildChecked && updateManager.expectedSourceID !== "" && runningSourceID !== updateManager.expectedSourceID
  function checkRunningBuild() {
    var generation=connectionGeneration
    call("buildInfo", null, function(ok,res) {
      if (!root.connected || generation !== root.connectionGeneration) return
      root.runningSourceID=ok && res ? (res.sourceID || "") : ""
      root.helperBuildChecked=true
    }, "gmessages")
  }

  property bool helperPresent: false
  property bool goPresent: false
  property bool building: false
  property bool connected: false
  property string helperError: ""
  property string helperState: "checking"
  property string buildLog: ""

  property string currentNetwork: "gmessages"
  property var enabledServices: []
  property bool servicesConfigLoaded: false
  property bool serviceSelectionRequired: false
  property bool savingServices: false
  property string servicesError: ""
  property int servicesGeneration: 0
  property bool restartingServices: false
  property var pendingServiceChoice: null
  property var _selectionCallback: null
  property string _restartingProcessId: ""
  property int connectionGeneration: 0
  property int restartConnectionGeneration: 0

  function finishServiceSelection(ok, result) {
    restartGrace.stop()
    restartDeadline.stop()
    var callback=_selectionCallback
    _selectionCallback=null
    restartingServices=false
    savingServices=false
    pendingServiceChoice=null
    if (!ok) servicesError=String(result)
    if (callback) callback(ok,result)
  }

  function awaitServiceRestart(choice, callback) {
    pendingServiceChoice=choice.slice()
    if (callback) _selectionCallback=callback
    restartingServices=true
    restartConnectionGeneration=connectionGeneration
    _restartingProcessId=String(helperProc.processId)
    savingServices=true
    servicesError=""
    restartGrace.restart()
    restartDeadline.restart()
  }

  function isServiceEnabled(net) {
    return servicesConfigLoaded && enabledServices.indexOf(net) >= 0
  }

  function applyServiceConfig(config) {
    if (config && config.restartRequired === true) return false
    if (!config || !Array.isArray(config.enabledServices)) {
      servicesError = "Rebuild the helper to use service selection."
      return false
    }
    var next = config.enabledServices.filter(function(net) { return ["gmessages", "whatsapp", "telegram"].indexOf(net) >= 0 })
    if (next.length !== config.enabledServices.length || next.some(function(net,index) { return next.indexOf(net) !== index })) { servicesError = "Invalid service configuration from helper."; return false }
    if (JSON.stringify(next) !== JSON.stringify(enabledServices)) servicesGeneration++
    enabledServices = next
    serviceSelectionRequired = config.serviceSelectionRequired === true
    servicesConfigLoaded = true
    servicesError = ""
    if (!isServiceEnabled("gmessages")) { conversations = []; browserProfiles = [] }
    if (!isServiceEnabled("whatsapp")) conversationsWA = []
    if (!isServiceEnabled("telegram")) conversationsTG = []
    return true
  }

  function loadServiceConfig() {
    if (!connected || (savingServices && !restartingServices)) return
    var generation = servicesGeneration
    call("config", null, function(ok, res) {
      if ((savingServices && !restartingServices) || generation !== servicesGeneration) return
      if (!ok) { servicesError = String(res); return }
      if (res && res.restartRequired === true) return
      if (restartingServices && connectionGeneration <= restartConnectionGeneration) return
      var expected=pendingServiceChoice
      var applied=applyServiceConfig(res)
      if (applied) enabledServices.forEach(function(net) { root.loadConversations(net) })
      if (restartingServices && applied) {
        var matches=expected && JSON.stringify(expected.slice().sort()) === JSON.stringify(enabledServices.slice().sort())
        finishServiceSelection(!!matches,matches ? res : "The helper restarted with different service choices. Review the current selection.")
      }
    }, "gmessages")
  }

  function setEnabledServices(selected, callback) {
    if (savingServices || !servicesConfigLoaded) return
    savingServices = true
    servicesError = ""
    call("setEnabledServices", {enabledServices:selected}, function(ok, res) {
      if (!ok && root.restartingServices) { root._selectionCallback=callback; return }
      if (ok && res && res.restartRequired === true) {
        root.awaitServiceRestart(selected,callback)
        return
      }
      root.savingServices = false
      if (ok) ok = root.applyServiceConfig(res)
      if (!ok) root.servicesError = typeof res === "string" ? res : "Service selection could not be saved."
      else root.enabledServices.forEach(function(net) { root.loadConversations(net) })
      if (callback) callback(ok, ok ? res : root.servicesError)
    }, "gmessages")
  }

  Timer {
    id: restartGrace
    interval:10000
    onTriggered: {
      // Only the helper process owned by this plugin can be signaled here.
      if (helperProc.running && String(helperProc.processId) === root._restartingProcessId) helperProc.signal(9)
      else root.servicesError="Waiting for the helper to restart and confirm your choices."
    }
  }
  Timer {
    id: restartDeadline
    interval:30000
    onTriggered: root.finishServiceSelection(false,"Choices were saved, but helper restart could not be confirmed. Restart the Omarchy shell, then review Settings. No send was retried.")
  }

  property var status: ({ state: "disconnected", unread: 0, phoneOK: false, qrURL: "", error: "" })
  property var statusWA: ({ state: "disconnected", unread: 0, phoneOK: true, qrURL: "", error: "" })
  property var statusTG: ({ state: "unpaired", unread: 0, phoneOK: true, qrURL: "", error: "" })
  readonly property string state: status && status.state ? status.state : "disconnected"
  readonly property int unread: unreadFor("gmessages") + unreadFor("whatsapp") + unreadFor("telegram")
  property var conversations: []
  property var conversationsWA: []
  property var conversationsTG: []
  property var browserProfiles: []
  property bool refreshing: false
  property string refreshError: ""

  function statusFor(net) {
    if (net === "whatsapp") return root.statusWA
    if (net === "telegram") return root.statusTG
    return root.status
  }
  function stateFor(net) {
    var s = statusFor(net)
    return s && s.state ? s.state : "disconnected"
  }
  function unreadFor(net) {
    if (!isServiceEnabled(net)) return 0
    var s = statusFor(net)
    return s && s.unread ? s.unread : 0
  }
  function conversationsFor(net) {
    if (net === "whatsapp") return root.conversationsWA
    if (net === "telegram") return root.conversationsTG
    return root.conversations
  }

  signal messageReceived(var message, string network)
  signal conversationUpdated(var conversation, string network)
  signal paired(string network)
  signal transportError(string message)

  property int _nextId: 1
  property var _pending: ({})
  property int _restartMs: 1000
  property bool _startingHelper: false

  function call(method, params, callback, network) {
    var s = sockLoader.item
    if (!s || !s.connected) {
      if (callback) callback(false, "not connected to omachatd")
      return
    }
    var id = String(_nextId++)
    if (callback) _pending[id] = callback
    var net = network || "gmessages"
    var frame = { id: id, method: method, network: net }
    if (params !== undefined && params !== null) frame.params = params
    s.write(JSON.stringify(frame) + "\n")
    s.flush()
  }

  function loadConversations(network) {
    var net = network || "gmessages"
    if (!isServiceEnabled(net)) return
    var generation = servicesGeneration
    call("conversations", { count: 50 }, function(ok, res) {
      if (ok && res && generation === servicesGeneration && root.isServiceEnabled(net)) {
        if (net === "whatsapp") root.conversationsWA = res
        else if (net === "telegram") root.conversationsTG = res
        else root.conversations = res
      }
    }, net)
  }

  function refreshConversations(network) {
    var net = network || "gmessages"
    if (!isServiceEnabled(net)) return
    if (refreshing) return
    refreshing = true
    refreshError = ""
    call("refresh", null, function(ok, res) {
      root.refreshing = false
      if (!ok) { root.refreshError = String(res); return }
      root.loadConversations(net)
    }, net)
  }

  function failPending(reason) {
    var pending = _pending
    _pending = ({})
    for (var id in pending) pending[id](false, reason)
  }

  function loadProfiles() {
    if (!isServiceEnabled("gmessages")) return
    call("listProfiles", null, function(ok, res) {
      if (ok && res) root.browserProfiles = res
    }, "gmessages")
  }

  // Quickshell Socket is single-use, and toggling a Loader's active flag in
  // the same frame is a no-op. Tear down, then build a new Socket next tick.
  function rebuildSocket() {
    sockLoader.active = false
    Qt.callLater(function() { sockLoader.active = true })
  }

  function checkHelper() {
    helperError = ""
    helperState = "checking"
    testHelper.running = false
    testHelper.running = true
  }

  function checkGo() {
    testGo.running = false
    testGo.running = true
  }

  function buildHelper() {
    if (building) return
    if (!goPresent) {
      helperError = "Go is not installed. Install it yourself with: omarchy pkg add go"
      helperState = "missing"
      return
    }
    helperError = ""
    buildLog = ""
    building = true
    helperState = "building"
    mkdirBin.running = false
    mkdirBin.running = true
  }

  function startHelper() {
    if (helperProc.running || _startingHelper) return
    _startingHelper = true
    helperState = "starting"
    helperProc.running = true
  }

  function stopHelper() {
    helperProc.running = false
  }

  Component.onCompleted: {
    // Talk to a helper that is already up before we try to spawn another.
    rebuildSocket()
    checkGo()
  }

  Component.onDestruction: stopHelper()

  Process {
    id: testHelper
    command: ["/usr/bin/test", "-x", root.helperPath]
    onExited: function(code) {
      root.helperPresent = code === 0
      if (root.helperPresent) {
        if (!root.connected) root.startHelper()
        else root.helperState = "running"
        return
      }
      if (root.connected) {
        root.helperState = "running"
        return
      }
      root.helperState = "missing"
    }
  }

  Process {
    id: testGo
    command: ["/usr/bin/go", "version"]
    onExited: function(code) {
      root.goPresent = code === 0
      root.checkHelper()
    }
  }

  Process {
    id: mkdirBin
    command: ["/usr/bin/mkdir", "-p", root.pluginDir + "/bin"]
    onExited: function(code) {
      if (code !== 0) {
        root.building = false
        root.helperState = "missing"
        root.helperError = "Could not create " + root.pluginDir + "/bin"
        return
      }
      buildProc.command = ["python3", root.pluginDir + "/scripts/updates.py", "build"]
      buildProc.running = false
      buildProc.running = true
      buildStartTimeout.restart()
    }
  }

  Process {
    id: buildProc
    onStarted: buildStartTimeout.stop()
    stdout: StdioCollector { id:buildResult; waitForEnd:true }
    command: ["/usr/bin/go", "version"]
    environment: ({ GOPROXY: "off", CGO_ENABLED: "1" })
    stderr: SplitParser {
      splitMarker: "\n"
      onRead: function(line) {
        if (root.buildLog.length < 4000) root.buildLog += line + "\n"
      }
    }
    onExited: function(code) {
      buildStartTimeout.stop()
      root.building = false
      if (code === 0) {
        root.helperPresent = true
        root.helperError = ""
        root.helperState = "ready"
        updateManager.inspect()
        if (helperProc.running) {
          root.restartingBuild=true
          helperProc.running=false
        } else if(root.connected) {
          root.helperError="Built successfully, but another helper still owns the connection. Restart the Omarchy shell. If this persists, stop the separately launched omachatd, restart the shell, and check Updates again."
          root.checkRunningBuild()
        } else root.startHelper()
      } else {
        root.helperState = root.helperPresent ? "running" : "missing"
        var tail = root.buildLog.trim()
        try { var result=JSON.parse(buildResult.text); if(result.error) tail += "\n" + result.error } catch(e) {}
        root.helperError = tail !== ""
          ? "Build failed (exit " + code + "):\n" + tail
          : "Build failed (exit " + code + "). The shared helper needs Go and a C compiler (gcc or clang) for all services. Check Settings > Tools to review missing requirements and choose whether to install them. OmaChat never installs dependencies automatically."
      }
    }
  }

  Timer {
    id:buildStartTimeout
    interval:5000
    onTriggered: {
      if (!root.building || buildProc.running) return
      root.building=false
      root.helperState=root.helperPresent ? "running" : "missing"
      root.helperError="Could not start the build. Python 3 is required. Review Settings > Tools, then try again. The previous executable was kept."
    }
  }

  Process {
    id: helperProc
    command: [root.helperPath, "--log-level", "info"]
    onStarted: {
      root._startingHelper = false
      root._restartMs = 1000
      root.helperPresent = true
      root.helperState = "running"
      helperReadyTimer.restart()
    }
    onExited: function(code) {
      root._startingHelper = false
      if (root.restartingBuild) {
        root.restartingBuild=false
        root.connected=false
        root.helperBuildChecked=false
        root.runningSourceID=""
        root._restartMs=1000
        root.rebuildSocket()
        restartTimer.restart()
        return
      }
      if (root.restartingServices) {
        restartGrace.stop()
        root.connected=false
        root.helperState="restarting"
        root._restartMs=1000
        restartTimer.restart()
        root.rebuildSocket()
        return
      }
      // A live helper already owns the socket. That is success, not a crash.
      if (root.connected) {
        root.helperState = "running"
        return
      }
      root.helperState = "exited"
      root.status = { state: "disconnected", unread: 0, phoneOK: false, qrURL: "", error: "" }
      if (root.helperPresent) restartTimer.restart()
    }
  }

  Timer {
    id: helperReadyTimer
    interval: 250
    onTriggered: root.rebuildSocket()
  }

  Timer {
    id: restartTimer
    interval: root._restartMs
    onTriggered: {
      if (root.connected || helperProc.running) return
      root._restartMs = Math.min(root._restartMs * 2, 30000)
      root.startHelper()
    }
  }

  Component {
    id: sockComponent

    Socket {
      path: root.socketPath
      connected: root.socketPath !== ""

      parser: SplitParser {
        splitMarker: "\n"
        onRead: function(line) { root._handleLine(line) }
      }

      onConnectionStateChanged: {
        root.connected = connected
        if (connected) {
          root.connectionGeneration++
          root.helperState = "running"
          reconnectTimer.stop()
          reconnectTimer.interval = 1000
          var generation=root.connectionGeneration
          // Socket can connect during Loader construction, before item exists.
          Qt.callLater(function() {
            if (!root.connected || generation !== root.connectionGeneration) return
            root.call("status", null, function(ok, res) { if (ok && res) root.status = res }, "gmessages")
            root.call("status", null, function(ok, res) { if (ok && res) root.statusWA = res }, "whatsapp")
            root.call("status", null, function(ok, res) { if (ok && res) root.statusTG = res }, "telegram")
            root.loadServiceConfig()
            root.checkRunningBuild()
          })
        } else {
          root.failPending(root.restartingServices ? "Helper restarting after service changes. A submitted message may still arrive; check before retrying." : "Disconnected from omachatd")
          reconnectTimer.start()
          // Process exit can precede the socket's disconnect notification.
          if (root.helperPresent && !helperProc.running) restartTimer.restart()
        }
      }

      onError: function(err) {
        root.connected = false
        root.transportError("socket error: " + err)
        // A failed initial connection may never change connectionState.
        // Keep retrying while the shell-owned helper finishes startup.
        reconnectTimer.start()
      }

      Component.onCompleted: root.connected = connected
    }
  }

  Loader {
    id: sockLoader
    active: false
    sourceComponent: sockComponent
  }

  Timer {
    interval:1000
    repeat:true
    running:root.connected && (!root.servicesConfigLoaded || root.restartingServices)
    onTriggered:root.loadServiceConfig()
  }

  Timer {
    id: reconnectTimer
    interval: 1000
    repeat: true
    onTriggered: {
      if (root.connected) {
        stop()
        interval = 1000
        return
      }
      if (interval < 8000) interval = Math.min(interval * 2, 8000)
      root.rebuildSocket()
    }
  }

  function _handleLine(line) {
    if (!line || line.length === 0) return
    var frame
    try {
      frame = JSON.parse(line)
    } catch (e) {
      root.transportError("bad frame from helper")
      return
    }

    if (frame.event !== undefined) {
      var net = frame.network || "gmessages"
      if (net !== "gmessages" && net !== "whatsapp" && net !== "telegram") return
      root._handleEvent(frame)
      return
    }

    var cb = _pending[frame.id]
    if (cb) {
      delete _pending[frame.id]
      cb(frame.ok === true, frame.ok === true ? frame.result : (frame.error || "unknown error"))
    }
  }

  function _handleEvent(frame) {
    if (frame.event === "config") {
      if (frame.data && frame.data.restartRequired === true && !restartingServices)
        awaitServiceRestart(frame.data.enabledServices,null)
      else if (!savingServices) applyServiceConfig(frame.data)
      return
    }
    var net = frame.network || "gmessages"
    if (frame.event !== "status" && !isServiceEnabled(net)) return
    switch (frame.event) {
    case "status":
      if (net === "whatsapp") {
        root.statusWA = frame.data
        if (root.statusWA && root.statusWA.state === "unpaired") root.conversationsWA = []
      } else if (net === "telegram") {
        root.statusTG = frame.data
        if (root.statusTG && root.statusTG.state === "unpaired") root.conversationsTG = []
      } else {
        root.status = frame.data
        if (root.state === "unpaired") root.conversations = []
      }
      break
    case "conversation":
      if (net === "whatsapp") {
        root._mergeConversationWA(frame.data)
      } else if (net === "telegram") {
        root._mergeConversationTG(frame.data)
      } else {
        root._mergeConversation(frame.data)
      }
      break
    case "message":
      root.messageReceived(frame.data, net)
      break
    case "paired":
      root.paired(net)
      root.loadConversations(net)
      break
    }
  }

  function _mergeConversation(conv) {
    if (!conv || !conv.id) return
    var list = root.conversations.slice()
    var found = false
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === conv.id) {
        list[i] = conv
        found = true
        break
      }
    }
    if (!found) list.push(conv)
    list.sort(function(a, b) {
      if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1
      return (b.timestamp || 0) - (a.timestamp || 0)
    })
    root.conversations = list
    root.conversationUpdated(conv, "gmessages")
  }

  function _mergeConversationWA(conv) {
    if (!conv || !conv.id) return
    var list = root.conversationsWA.slice()
    var found = false
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === conv.id) {
        list[i] = conv
        found = true
        break
      }
    }
    if (!found) list.push(conv)
    list.sort(function(a, b) {
      if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1
      return (b.timestamp || 0) - (a.timestamp || 0)
    })
    root.conversationsWA = list
    root.conversationUpdated(conv, "whatsapp")
  }

  function _mergeConversationTG(conv) {
    if (!conv || !conv.id) return
    var list = root.conversationsTG.slice()
    var found = false
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === conv.id) {
        list[i] = conv
        found = true
        break
      }
    }
    if (!found) list.push(conv)
    list.sort(function(a, b) {
      if (!!a.pinned !== !!b.pinned) return a.pinned ? -1 : 1
      return (b.timestamp || 0) - (a.timestamp || 0)
    })
    root.conversationsTG = list
    root.conversationUpdated(conv, "telegram")
  }
}
