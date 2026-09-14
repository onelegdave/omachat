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

  property bool helperPresent: false
  property bool goPresent: false
  property bool building: false
  property bool connected: false
  property string helperError: ""
  property string helperState: "checking"
  property string buildLog: ""

  property string currentNetwork: "gmessages"

  property var status: ({ state: "disconnected", unread: 0, phoneOK: false, qrURL: "", error: "" })
  property var statusWA: ({ state: "disconnected", unread: 0, phoneOK: true, qrURL: "", error: "" })
  property var statusTG: ({ state: "unpaired", unread: 0, phoneOK: true, qrURL: "", error: "" })
  readonly property string state: status && status.state ? status.state : "disconnected"
  readonly property int unread: (status && status.unread ? status.unread : 0) + (statusWA && statusWA.unread ? statusWA.unread : 0) + (statusTG && statusTG.unread ? statusTG.unread : 0)
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
    call("conversations", { count: 50 }, function(ok, res) {
      if (ok && res) {
        if (net === "whatsapp") root.conversationsWA = res
        else root.conversations = res
      }
    }, net)
  }

  function refreshConversations(network) {
    var net = network || "gmessages"
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
      buildProc.command = ["/usr/bin/go", "-C", root.pluginDir, "build", "-mod=vendor", "-o", root.helperPath, "./cmd/omachatd"]
      buildProc.running = false
      buildProc.running = true
    }
  }

  Process {
    id: buildProc
    command: ["/usr/bin/go", "version"]
    environment: ({ GOPROXY: "off", CGO_ENABLED: "1" })
    stderr: SplitParser {
      splitMarker: "\n"
      onRead: function(line) {
        if (root.buildLog.length < 4000) root.buildLog += line + "\n"
      }
    }
    onExited: function(code) {
      root.building = false
      if (code === 0) {
        root.helperPresent = true
        root.helperError = ""
        root.helperState = "ready"
        root.startHelper()
      } else {
        root.helperState = "missing"
        var tail = root.buildLog.trim()
        root.helperError = tail !== ""
          ? "Build failed (exit " + code + "):\n" + tail
          : "Build failed (exit " + code + "). Install Go with: omarchy pkg add go. WhatsApp needs a C compiler (gcc or clang) because the helper links mattn/go-sqlite3 with CGO."
      }
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
          root.helperState = "running"
          reconnectTimer.stop()
          reconnectTimer.interval = 1000
          root.call("status", null, function(ok, res) { if (ok && res) root.status = res }, "gmessages")
          root.call("status", null, function(ok, res) { if (ok && res) root.statusWA = res }, "whatsapp")
          root.call("status", null, function(ok, res) { if (ok && res) root.statusTG = res }, "telegram")
          root.loadConversations("gmessages")
          root.loadConversations("whatsapp")
        } else {
          root.failPending("Disconnected from omachatd")
          reconnectTimer.start()
        }
      }

      onError: function(err) {
        root.connected = false
        root.transportError("socket error: " + err)
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
    var net = frame.network || "gmessages"
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
