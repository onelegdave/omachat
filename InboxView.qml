import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root

  property var service: null
  property color foreground: Model.readableInk(Color.popups.background, Color.popups.text, 7)
  property string fontFamily: Style.font.family
  property real uiScale: 1
  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }
  property var host: null
  property var settings: null
  property string network: "gmessages"
  readonly property bool isWhatsApp: network === "whatsapp"
  readonly property bool isTelegram: network === "telegram"
  readonly property bool isMessenger: network === "messenger"
  readonly property bool reactionsSupported: true
  property string networkLabel: isWhatsApp ? "WhatsApp" : (isTelegram ? "Telegram" : (isMessenger ? "Messenger" : "Google Messages"))

  readonly property color dim: Model.readableInk(panelBg, Color.muted)
  readonly property color errorInk: Model.readableInk(panelBg, Color.urgent)
  readonly property color panelBg: Color.popups.background
  readonly property color mineFill: Model.outgoingFill(panelBg, Color.accent, foreground)
  readonly property color mineInk: Model.inkOn(mineFill, foreground, panelBg)
  readonly property color mineMeta: Model.metaInk(mineInk, mineFill)
  readonly property color theirsFill: Model.incomingFill(panelBg, foreground)
  readonly property color theirsInk: Model.inkOn(theirsFill, foreground, panelBg)
  readonly property color theirsMeta: Model.metaInk(theirsInk, theirsFill)
  readonly property color selectedFill: Model.outgoingFill(panelBg, Color.accent, foreground)
  readonly property color selectedInk: Model.inkOn(selectedFill, foreground, panelBg)
  readonly property color selectedMeta: Model.metaInk(selectedInk, selectedFill)
  readonly property bool pendingIsGif: Model.isGif("", "", pendingAttachment)
  readonly property bool pendingIsVoice: pendingAttachment.indexOf("/voice-") >= 0 && (pendingAttachment.indexOf(".m4a") >= 0 || pendingAttachment.indexOf(".ogg") >= 0 || pendingAttachment.indexOf(".opus") >= 0)
  property bool viewActive: true
  readonly property bool panelOpen: viewActive && host && host.opened === true
  onPanelOpenChanged: {
    if (panelOpen) return
    if (recording) stopRecording(false)
    stopPlayback()
  }
  readonly property string captureDir: {
    var c = Quickshell.env("XDG_CACHE_HOME")
    if (c && c.length > 0) return c + "/omachat"
    return Quickshell.env("HOME") + "/.cache/omachat"
  }
  readonly property int maxRecordSeconds: 60
  property bool recording: false
  property int recordSeconds: 0
  property int pendingVoiceSeconds: 0
  property string voicePath: ""
  property bool keepRecording: false
  property string playingKey: ""
  property string audioWaitingKey: ""
  readonly property bool playingVoice: playingKey !== ""
  readonly property var conversations: service ? (typeof service.conversationsFor === "function" ? service.conversationsFor(root.network) : (service.conversations || [])) : []
  property string searchQuery: ""
  property string selectedConvID: ""
  property var _draftsByNet: ({})
  property var _pendingSends: ({})
  property var _networkEpochs: ({})
  property int mediaSendToken: 0
  property int selectionGeneration: 0
  property var messages: []
  property var grouped: []
  // Keep delegates alive across snapshots and receipt updates. Replacing a JS
  // array model destroys every visible image, even when its message is unchanged.
  ListModel { id: messageRows; dynamicRoles: true }
  onGroupedChanged: {
    for (var i = 0; i < grouped.length; i++) {
      var entry = grouped[i]
      var found = -1
      for (var j = i; j < messageRows.count; j++) {
        if (messageRows.get(j).entry.key === entry.key) { found = j; break }
      }
      if (found < 0) messageRows.insert(i, {entry: entry})
      else {
        if (found !== i) messageRows.move(found, i, 1)
        if (JSON.stringify(messageRows.get(i).entry) !== JSON.stringify(entry))
          messageRows.setProperty(i, "entry", entry)
      }
    }
    if (messageRows.count > grouped.length)
      messageRows.remove(grouped.length, messageRows.count - grouped.length)
  }
  property bool loadingMessages: false
  property bool loadingOlder: false
  property bool hasOlder: false
  property bool historyExpanded: false
  property bool historyCursorStalled: false
  property string historyCursorID: ""
  property double historyCursorTime: 0
  property string historyError: ""
  property string historyNotice: ""
  property var historyCursors: ({})
  property int historyRequest: 0
  property int viewportRevision: 0
  property var pendingAnchor: null
  property string threadError: ""
  property var mediaPaths: ({})
  property var _mediaNetworks: ({})
  property var mediaRequests: ({})
  property string pendingAttachment: ""
  property bool sendingMedia: false
  onPendingAttachmentChanged: attachCaption.text = ""
  property bool emojiPickerOpen: false
  property bool emojiPickerForReact: false
  property bool gifPickerOpen: false
  property var gifList: []
  property bool gifNeedsKey: false
  property string gifError: ""
  property string gifQuery: ""
  property string gifAttribution: "Powered by GIPHY"
  property string gifKeyDraft: ""
  property string reactingTo: ""
  property bool copied: false
  property bool composerFocus: false
  property bool linkConfirmOpen: false
  property string pendingUrl: ""
  property var emojiList: []
  readonly property var reactionChoices: ["❤️", "👍", "👎", "😂", "😮", "😢"]
  readonly property var fallbackEmoji: [
    { e: "😀", k: "grinning" }, { e: "😂", k: "laugh tears" },
    { e: "🙂", k: "smile" }, { e: "👍", k: "thumbs up yes" },
    { e: "👎", k: "thumbs down no" }, { e: "❤", k: "heart love" },
    { e: "🎉", k: "party tada" }, { e: "😭", k: "cry sob" }
  ]

  readonly property var selectedConv: {
    for (var i = 0; i < conversations.length; i++) {
      if (conversations[i].id === selectedConvID) return conversations[i]
    }
    return null
  }

  readonly property var visibleConversations: {
    var q = searchQuery.trim().toLowerCase()
    if (q === "") return conversations
    var out = []
    for (var i = 0; i < conversations.length; i++) {
      var c = conversations[i]
      if ((c.name || "").toLowerCase().indexOf(q) >= 0
          || (c.preview || "").toLowerCase().indexOf(q) >= 0) out.push(c)
    }
    return out
  }

  readonly property int unreadConversations: {
    var n = 0
    for (var i = 0; i < conversations.length; i++) if (conversations[i].unread === true) n++
    return n
  }


  onSettingsChanged: syncGiphyKey()
  onServiceChanged: syncGiphyKey()

  property var _selectedByNet: ({})
  property string _previousNetwork: network
  onNetworkChanged: {
    var prev = _previousNetwork
    _previousNetwork = network
    if (prev === network) return
    mediaSendToken++
    sendingMedia = false
    mediaRequests = ({})
    mediaRetry.queue = []
    mediaRetry.stop()
    selectionGeneration++
    historyRequest++
    loadingMessages=false
    loadingOlder=false

    if (selectedConvID) {
      var saved = Object.assign({}, _draftsByNet)
      var netDrafts = Object.assign({}, saved[prev] || {})
      netDrafts[selectedConvID] = composer ? composer.text : ""
      saved[prev] = netDrafts
      _draftsByNet = saved
    }
    var nextSel = Object.assign({}, _selectedByNet)
    nextSel[prev] = selectedConvID
    _selectedByNet = nextSel

    stopPlayback()
    if (recording) stopRecording(false)
    discardPendingCapture()
    pendingAttachment = ""
    attachCaption.text = ""
    emojiPickerOpen = false
    gifPickerOpen = false
    reactingTo = ""
    threadError = ""

    var restoredID = _selectedByNet[network] || ""
    selectedConvID = ""
    messages = []
    grouped = []
    if (restoredID) {
      selectConversation(restoredID)
    } else {
      if (composer) composer.text = ""
    }
  }

  Timer {
    interval: 600
    running: root.service && root.service.connected && (typeof root.service.stateFor === "function" ? root.service.stateFor(root.network) : root.service.state) === "connected" && root.conversations.length === 0
    repeat: true
    onTriggered: if (root.service && root.service.loadConversations) root.service.loadConversations(root.network)
  }

  function setting(key, fallback) {
    if (settings && settings[key] !== undefined && String(settings[key]) !== "") return settings[key]
    return fallback
  }

  function selectConversation(id) {
    if (id === selectedConvID || sendingMedia) return
    var saved = Object.assign({}, _draftsByNet)
    if (selectedConvID) {
      var netDrafts = Object.assign({}, saved[network] || {})
      netDrafts[selectedConvID] = composer.text
      saved[network] = netDrafts
      _draftsByNet = saved
    }
    selectionGeneration++
    historyRequest++
    viewportRevision++
    pendingAnchor = null
    loadingOlder = false
    hasOlder = false
    historyExpanded = false
    historyCursorStalled = false
    loadingMessages = false
    historyCursorID = ""
    historyCursorTime = 0
    historyError = ""
    historyNotice = ""
    historyCursors = ({})
    stopPlayback()
    if (recording) stopRecording(false)
    discardPendingCapture()
    emojiPickerOpen = false
    gifPickerOpen = false
    reactingTo = ""
    pendingAttachment = ""
    selectedConvID = id
    var currentNetDrafts = _draftsByNet[network] || {}
    composer.text = currentNetDrafts[id] || ""
    attachCaption.text = ""
    pendingVoiceSeconds = 0
    messages = []
    grouped = []
    threadError = ""
    loadMessages()
  }

  function refreshThread() {
    mediaRequests = ({})
    mediaRetry.queue = []
    mediaRetry.stop()
    loadMessages()
  }

  function captureViewport() {
    if (pendingAnchor) return pendingAnchor
    messageList.forceLayout()
    for (var y = 1; y < messageList.height; y += 4) {
      var index = messageList.indexAt(messageList.width / 2, messageList.contentY + y)
      if (index < 0 || !grouped[index] || grouped[index].kind !== "msg") continue
      var item = messageList.itemAtIndex(index)
      if (item) return { key: grouped[index].key, offset: item.y - messageList.contentY }
    }
    return null
  }

  function displayMessages(next, followEnd) {
    var anchor = followEnd ? null : captureViewport()
    pendingAnchor = anchor
    messages = next
    grouped = Model.groupMessages(messages)
    var revision = ++viewportRevision
    var generation = selectionGeneration
    Qt.callLater(function() {
      if (revision !== viewportRevision || generation !== selectionGeneration) return
      messageList.forceLayout()
      if (followEnd) {
        messageList.positionViewAtEnd()
        messageList.forceLayout()
        messageList.positionViewAtEnd()
      }
      else if (anchor) {
        for (var i = 0; i < grouped.length; i++) {
          if (grouped[i].key !== anchor.key) continue
          messageList.positionViewAtIndex(i, ListView.Beginning)
          messageList.forceLayout()
          var item = messageList.itemAtIndex(i)
          if (item) messageList.contentY = item.y - anchor.offset
          break
        }
      }
      pendingAnchor = null
    })
  }

  function readHistoryCursor(res, older) {
    var id = String(res.cursorID || "")
    var time = Number(res.cursorTime || 0)
    var key = JSON.stringify([id, time])
    historyCursorStalled = false
    hasOlder = res.hasMore === true && id !== ""
    if (hasOlder && older && historyCursors[key]) {
      hasOlder = false
      historyCursorStalled = true
      historyError = root.isWhatsApp
        ? "WhatsApp repeated the cached history cursor. Refresh the conversation to try again."
        : (root.isTelegram ? "Telegram repeated the history cursor. Refresh the conversation to try again." : (root.isMessenger ? "Messenger repeated the history cursor. Refresh the conversation to try again." : "Google repeated the history cursor. Refresh the conversation to try again."))
    }
    historyCursorID = id
    historyCursorTime = time
    var seen = older ? Object.assign({}, historyCursors) : ({})
    if (hasOlder) seen[key] = true
    historyCursors = seen
  }

  function loadMessages() {
    if (selectedConvID === "" || !service) return
    loadingMessages = true
    loadingOlder = false
    var request = ++historyRequest
    var target = selectedConvID
    var generation = selectionGeneration
    var source = service
    source.call("messages", { conversationID: target, count: 60 }, function(ok, res) {
      if (source !== service || target !== selectedConvID || generation !== selectionGeneration || request !== historyRequest) return
      loadingMessages = false
      if (!ok) { threadError = String(res); return }
      threadError = ""
      var initial = messages.length === 0
      var fetched = res.messages || []
      var netPending = root._pendingSends[root.network] || {}
      var convPending = netPending[target] || []
      var combined = Model.mergePage(convPending, fetched, false)
      var remaining = convPending.filter(function(local) {
        return !fetched.some(function(remote) { return Model.sameMessage(local, remote) })
      })
      root.storeLocalSends(root.network, target, remaining)
      displayMessages(Model.mergePage(messages, combined, false), initial || messageList.atYEnd)
      historyError = ""
      historyNotice = String(res.historyNotice || "")
      if (!historyExpanded || historyCursorStalled) {
        readHistoryCursor(res, false)
      }
      markThreadRead()
    }, root.network)
  }

  function loadOlderMessages() {
    if (!service || selectedConvID === "" || loadingMessages || loadingOlder || !hasOlder) return
    loadingOlder = true
    historyError = ""
    var request = ++historyRequest
    var target = selectedConvID
    var generation = selectionGeneration
    var source = service
    source.call("messages", { conversationID: target, count: 60,
      cursorID: historyCursorID, cursorTime: historyCursorTime }, function(ok, res) {
      if (source !== service || target !== selectedConvID || generation !== selectionGeneration || request !== historyRequest) return
      loadingOlder = false
      if (!ok) { historyError = "Could not load older messages. Try again."; return }
      displayMessages(Model.mergePage(messages, res.messages || [], true), false)
      historyExpanded = true
      readHistoryCursor(res, true)
    }, root.network)
  }

  function markThreadRead() {
    if (!panelOpen || !service || selectedConvID === "" || messages.length === 0) return
    var last = null
    for (var i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id && !messages[i].provisional) { last = messages[i]; break }
    }
    if (!last) return
    service.call("markRead", { conversationID: selectedConvID, messageID: last.id }, null, root.network)
  }

  function sendMessage(rawText) {
    var text = (rawText || "").trim()
    if (text === "" || root.selectedConvID === "" || !root.service) return
    var convID = root.selectedConvID
    var tmpID = Model.transactionID()
    var targetNet = root.network
    var epoch = root._networkEpochs[targetNet] || 0
    root.mergeMessage({
      id: tmpID, tmpID: tmpID, conversationID: convID, text: text,
      timestamp: Date.now() * 1000, fromMe: true, pending: true, provisional: true, failed: false
    }, targetNet)
    root.service.call("send", { conversationID: convID, text: text, tmpID: tmpID }, function(ok, res) {
      if ((root._networkEpochs[targetNet] || 0) !== epoch) return
      var nextPending = Object.assign({}, root._pendingSends)
      var netPending = Object.assign({}, nextPending[targetNet] || {})
      var convPending = (netPending[convID] || []).slice()
      if (ok) {
        root.mergeMessage(Object.assign({}, res, {tmpID:tmpID}), targetNet, true)
        return
      }
      convPending = Model.failSend(convPending, tmpID)
      if (convPending.length > 0) netPending[convID] = convPending
      else delete netPending[convID]
      nextPending[targetNet] = netPending
      root._pendingSends = nextPending

      if (convID === root.selectedConvID && targetNet === root.network) {
        root.displayMessages(Model.failSend(root.messages, tmpID), messageList.atYEnd)
        root.threadError = String(res)
      }
    }, targetNet)
  }

  function chooseEmoji(emoji) {
    if (!emoji) return
    if (root.emojiPickerForReact && root.reactingTo) root.react(root.reactingTo, emoji)
    else composer.text += emoji
    root.emojiPickerOpen = false
    root.emojiPickerForReact = false
    composer.forceActiveFocus()
  }

  onEmojiPickerOpenChanged: if (emojiPickerOpen) Qt.callLater(function() { emojiGrid.forceActiveFocus() })

  function storeLocalSends(net, convID, rows) {
    var next = Object.assign({}, root._pendingSends)
    var conversations = Object.assign({}, next[net] || {})
    if (rows.length) conversations[convID] = rows
    else delete conversations[convID]
    next[net] = conversations
    root._pendingSends = next
  }

  function mergeMessage(msg, targetNet, localSend) {
    if (!msg) return
    var net = targetNet || root.network
    var convPending = (root._pendingSends[net] || {})[msg.conversationID] || []
    if (msg.provisional || localSend || convPending.some(function(m) { return Model.sameMessage(m, msg) }))
      root.storeLocalSends(net, msg.conversationID, Model.mergeMessage(convPending, msg))
    if (net === root.network && msg.conversationID === root.selectedConvID) {
      var follow = root.messages.length === 0 || messageList.atYEnd || (msg.fromMe && msg.provisional)
      root.displayMessages(Model.mergeMessage(root.messages, msg), follow)
    }
  }

  function _withMedia(mediaID, value) {
    var owners = Object.assign({}, _mediaNetworks)
    owners[mediaID] = root.network
    _mediaNetworks = owners
    var next = {}
    for (var k in mediaPaths) next[k] = mediaPaths[k]
    next[mediaID] = value
    mediaPaths = next
  }

  function _evictMedia(key) {
    if (!key) return
    var nextPaths = {}
    for (var k in mediaPaths) {
      if (k !== key) nextPaths[k] = mediaPaths[k]
    }
    mediaPaths = nextPaths
    var nextReqs = {}
    for (var rk in mediaRequests) {
      if (rk !== key) nextReqs[rk] = mediaRequests[rk]
    }
    mediaRequests = nextReqs
  }

  function _clearMediaForNetwork(net) {
    var target = net || root.network
    var paths = {}, requests = {}, owners = {}
    var keys = Object.assign({}, mediaPaths, mediaRequests, _mediaNetworks)
    for (var key in keys) {
      var owner = _mediaNetworks[key] || (key.indexOf("tg:") === 0 ? "telegram" : (key.indexOf("@") >= 0 ? "whatsapp" : "gmessages"))
      if (owner === target) continue
      if (mediaPaths[key]) paths[key] = mediaPaths[key]
      if (mediaRequests[key]) requests[key] = mediaRequests[key]
      owners[key] = owner
    }
    mediaPaths = paths
    mediaRequests = requests
    _mediaNetworks = owners
    mediaRetry.queue = []
    mediaRetry.stop()
  }

  function setMediaRequest(key, state) {
    var owners = Object.assign({}, _mediaNetworks)
    owners[key] = root.network
    _mediaNetworks = owners
    var next = Object.assign({}, mediaRequests)
    next[key] = state
    mediaRequests = next
  }

  function requestMedia(key, attempt) {
    if (!key || !service) return
    var tries = attempt === undefined ? 0 : attempt
    if (tries === 0 && (mediaRequests[key] === "loading" || (mediaRequests[key] === "ready" && mediaPaths[key]))) return
    setMediaRequest(key, "loading")
    var targetNet = root.network
    var epoch = root._networkEpochs[targetNet] || 0
    service.call("media", { key: key }, function(ok, res) {
      if (targetNet !== root.network || epoch !== (root._networkEpochs[targetNet] || 0)) return
      if (ok && res && res.path) _withMedia(key, res.path)
      if (ok && res && res.path && !res.thumbnail) {
        setMediaRequest(key, "ready")
        return
      }
      if (ok && res && (res.pending || res.thumbnail) && tries < 6) {
        mediaRetry.schedule(key, tries + 1)
        return
      }
      if (!ok && tries < 3) {
        setMediaRequest(key, "failed")
        mediaRetry.schedule(key, tries + 1)
        return
      }
      setMediaRequest(key, "failed")
    }, root.network)
  }

  function syncGiphyKey() {
    var k = settings && settings.giphyApiKey ? String(settings.giphyApiKey).trim() : ""
    if (k && service) service.call("setGiphyKey", { key: k }, null, root.network)
  }

  function openGifPicker() {
    if (root.isTelegram) {
      threadError = "GIF search is not supported for " + root.networkLabel + " in this version."
      return
    }
    if (!service) return
    emojiPickerOpen = false
    gifPickerOpen = !gifPickerOpen
    if (gifPickerOpen) searchGifs(gifQuery)
  }

  function searchGifs(q) {
    if (!service) return
    gifQuery = q || ""
    gifError = ""
    service.call("gifSearch", { query: gifQuery, limit: 24 }, function(ok, res) {
      if (!ok) {
        gifNeedsKey = false
        gifList = []
        gifError = String(res)
        return
      }
      gifNeedsKey = res && res.needsKey === true
      gifList = res && res.gifs ? res.gifs : []
      gifAttribution = res && res.attribution ? res.attribution : "Powered by GIPHY"
    }, root.network)
  }

  function saveGiphyKey() {
    var k = gifKeyDraft.trim()
    if (!k || !service) return
    service.call("setGiphyKey", { key: k }, function(ok, res) {
      if (!ok) { gifError = String(res); return }
      gifNeedsKey = false
      gifKeyDraft = ""
      searchGifs(gifQuery)
    }, root.network)
  }

  function pickGif(item) {
    if (!service || !item || !item.sendURL) return
    gifError = ""
    var generation = selectionGeneration
    service.call("gifFetch", { url: item.sendURL, id: item.id || "" }, function(ok, res) {
      if (generation !== selectionGeneration) return
      if (!ok) { gifError = String(res); return }
      gifPickerOpen = false
      if (res && res.path) pendingAttachment = res.path
    }, root.network)
  }

  function requestOpenUrl(raw) {
    var u = Model.safeHttpUrl(raw)
    if (!u) return
    pendingUrl = u
    linkConfirmOpen = true
  }

  function confirmOpenUrl() {
    var u = pendingUrl
    linkConfirmOpen = false
    pendingUrl = ""
    if (u) Util.execArgv(["xdg-open", u])
  }

  function cancelOpenUrl() {
    linkConfirmOpen = false
    pendingUrl = ""
  }

  function openImage(path) {
    if (!path) return
    Util.execArgv(["xdg-open", path])
  }

  function attachFromDisk() {
    if (!service) return
    threadError = ""
    var generation = selectionGeneration
    service.call("pickImage", null, function(ok, res) {
      if (generation !== selectionGeneration) return
      if (!ok) { threadError = String(res); return }
      if (res && res.path) pendingAttachment = res.path
    }, root.network)
  }

  function sendAttachment(caption) {
    if (!root.service || root.sendingMedia || root.pendingAttachment === "" || root.selectedConvID === "") return
    root.stopPlayback()
    root.threadError = ""
    var path = root.pendingAttachment
    var convID = root.selectedConvID
    var targetNet = root.network
    var generation = root.selectionGeneration
    var token = ++root.mediaSendToken
    var tmpID = Model.transactionID()
    root.sendingMedia = true
    root.service.call("sendMedia", {
      conversationID: convID,
      path: path,
      caption: caption || "",
      tmpID: tmpID,
      durationSeconds: root.pendingIsVoice ? Math.max(1, root.pendingVoiceSeconds) : 0
    },
      function(ok, res) {
        if (token !== root.mediaSendToken || generation !== root.selectionGeneration || targetNet !== root.network) return
        root.sendingMedia = false
        if (!ok) {
           if (convID === root.selectedConvID && targetNet === root.network && generation === root.selectionGeneration)
             root.threadError = String(res)
           return
        }
        if (targetNet === root.network && root.pendingAttachment === path) root.pendingAttachment = ""
        if (targetNet === root.network) root.pendingVoiceSeconds = 0
        if (res.message && res.message.attachments) {
          for (var ai = 0; ai < res.message.attachments.length; ai++) {
            var sentAttachment = res.message.attachments[ai]
            // WhatsApp converts GIF inputs to MP4 before upload. Let the media
            // request fetch that sent MP4 instead of mapping its key to the
            // original GIF and handing it to the video player.
            if (sentAttachment && sentAttachment.key && !(sentAttachment.isGif && sentAttachment.isVideo))
              root._withMedia(sentAttachment.key, path)
          }
        }
        root.mergeMessage(res.message, targetNet, true)
        if (res.captionMessage) root.mergeMessage(res.captionMessage, targetNet, true)
        if (res.captionError && convID === root.selectedConvID && targetNet === root.network && generation === root.selectionGeneration) {
          if (composer.text === "") composer.text = String(caption || "").trim()
          root.threadError = "Attachment submitted, but the caption could not be confirmed. Check the conversation before retrying. " + String(res.captionError)
        }
      }, targetNet)
  }

  function discardPendingCapture() {
    if (pendingAttachment === "") return
    if (!pendingIsVoice) return
    if (service) service.call("discardCapture", { path: pendingAttachment }, null, root.network)
  }

  function cancelAttachment() {
    stopPlayback()
    discardPendingCapture()
    pendingAttachment = ""
    pendingVoiceSeconds = 0
  }

  function startRecording() {
    if (recording || selectedConvID === "") return
    emojiPickerOpen = false
    gifPickerOpen = false
    threadError = ""
    discardPendingCapture()
    pendingAttachment = ""
    pendingVoiceSeconds = 0
    recordSeconds = 0
    voicePath = captureDir + "/voice-" + Date.now() + (root.isTelegram ? ".ogg" : ".m4a")
    mkdirCache.running = false
    mkdirCache.running = true
  }

  function _runFfmpegRecord() {
    var cmd = [
      "ffmpeg", "-y", "-hide_banner", "-loglevel", "error",
      "-f", "pulse", "-i", String(setting("audioDevice", "default")),
      "-ac", "1", "-ar", "48000"
    ]
    if (setting("normalizeVoice", true) !== false && setting("normalizeVoice", true) !== "false") {
      cmd.push("-af", "highpass=f=80,speechnorm=e=12.5:r=0.00025:l=1")
    }
    if (root.isTelegram)
      cmd.push("-c:a", "libopus", "-b:a", "32k", "-application", "voip", "-f", "ogg")
    else
      cmd.push("-c:a", "aac", "-b:a", "96k")
    cmd.push("-t", String(maxRecordSeconds), voicePath)
    voiceProc.command = cmd
    recording = true
    voiceProc.running = true
    recordTimer.start()
  }

  function stopRecording(keep) {
    if (!recording) return
    recordTimer.stop()
    keepRecording = keep === true
    pendingVoiceSeconds = recordSeconds
    recording = false
    try {
      voiceProc.write("q\n")
    } catch (e) {
      voiceProc.running = false
    }
    stopFallbackTimer.restart()
  }

  function playPendingVoice() {
    if (pendingAttachment === "") return
    _startAudio("staged", pendingAttachment)
  }

  function playAttachment(key) {
    if (!key) return
    if (playingKey === key) { stopPlayback(); return }
    var path = mediaPaths[key]
    if (path) {
      _startAudio(key, path)
      return
    }
    stopPlayback()
    audioWaitingKey = key
    _fetchAudio(key, 0)
  }

  function openVideo(key) {
    if (!key) return
    var path = mediaPaths[key]
    if (path) {
      openImage(path)
      return
    }
    audioWaitingKey = key
    service.call("media", { key: key }, function(ok, res) {
      if (audioWaitingKey !== key) return
      audioWaitingKey = ""
      if (ok && res && res.path) {
        _withMedia(key, res.path)
        openImage(res.path)
        return
      }
      threadError = "That video could not be downloaded yet."
    }, root.network)
  }

  function _fetchAudio(key, attempt) {
    if (!service) return
    service.call("media", { key: key }, function(ok, res) {
      if (root.audioWaitingKey !== key) return
      if (ok && res && res.path && !res.thumbnail) {
        root._withMedia(key, res.path)
        root._startAudio(key, res.path)
        return
      }
      var stillComing = ok && res && (res.pending === true || res.thumbnail === true)
      if (stillComing && attempt < 6) {
        audioRetry.schedule(key, attempt + 1)
        return
      }
      root.audioWaitingKey = ""
      root.threadError = stillComing
        ? "That voice message is still uploading from the phone. Try again in a moment."
        : "That voice message could not be downloaded."
    }, root.network)
  }

  function _startAudio(key, path) {
    stopPlayback()
    audioWaitingKey = ""
    playProc.command = ["ffplay", "-nodisp", "-autoexit", "-loglevel", "error", path]
    playingKey = key
    playProc.running = true
  }

  function stopPlayback() {
    audioWaitingKey = ""
    if (playingKey === "") return
    playingKey = ""
    playProc.running = false
  }

  function react(messageID, emoji) {
    reactingTo = ""
    if (!service || selectedConvID === "" || !messageID) return
    var generation = selectionGeneration
    service.call("react", {
      conversationID: selectedConvID,
      messageID: messageID,
      emoji: emoji || ""
    }, function(ok, res) {
      if (!ok && generation === selectionGeneration) threadError = String(res)
    }, root.network)
  }

  function copyText(value) {
    var v = String(value || "")
    if (!v) return
    Util.execArgv(["wl-copy", "--", v])
    flashCopied()
  }

  function flashCopied() {
    copied = true
    copiedTimer.restart()
  }

  readonly property var statusWA: service && typeof service.statusFor === "function" ? service.statusFor("whatsapp") : (service ? service.statusWA : null)
  onStatusWAChanged: if (statusWA && statusWA.state === "unpaired") root.clearNetwork("whatsapp")
  readonly property var statusTG: service && typeof service.statusFor === "function" ? service.statusFor("telegram") : (service ? service.statusTG : null)
  onStatusTGChanged: if (statusTG && statusTG.state === "unpaired") root.clearNetwork("telegram")
  readonly property var statusFB: service && typeof service.statusFor === "function" ? service.statusFor("messenger") : (service ? service.statusFB : null)
  onStatusFBChanged: if (statusFB && statusFB.state === "unpaired") root.clearNetwork("messenger")
  readonly property var statusGM: service && typeof service.statusFor === "function" ? service.statusFor("gmessages") : (service ? service.status : null)
  onStatusGMChanged: if (statusGM && statusGM.state === "unpaired") root.clearNetwork("gmessages")

  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onMessageReceived(msg, net) {
      root.mergeMessage(msg, net || root.network)
      if ((!net || net === root.network) && msg.conversationID === root.selectedConvID && root.panelOpen && !msg.fromMe) root.markThreadRead()
    }
    function onPaired(net) {
      root.clearNetwork(net)
    }
  }

  function clearNetwork(net) {
    var targetNet = net || root.network
    var epochs = Object.assign({}, root._networkEpochs)
    epochs[targetNet] = (epochs[targetNet] || 0) + 1
    root._networkEpochs = epochs
    if (targetNet === root.network) {
      root.mediaSendToken++; root.sendingMedia = false
      root.stopPlayback()
      if (root.recording) root.stopRecording(false)
      root.pendingAttachment = ""
      attachCaption.text = ""
    }
    if (net && net !== root.network) {
      var nextSel = Object.assign({}, root._selectedByNet)
      delete nextSel[net]
      root._selectedByNet = nextSel
      var nextDrafts = Object.assign({}, root._draftsByNet)
      delete nextDrafts[net]
      root._draftsByNet = nextDrafts
      var nextPending = Object.assign({}, root._pendingSends)
      delete nextPending[net]
      root._pendingSends = nextPending
      root._clearMediaForNetwork(net)
      return
    }
    root.selectionGeneration++
    root.historyRequest++
    root.selectedConvID = ""
    var s2 = Object.assign({}, root._selectedByNet)
    delete s2[root.network]
    root._selectedByNet = s2
    var d2 = Object.assign({}, root._draftsByNet)
    delete d2[root.network]
    root._draftsByNet = d2
    var p2 = Object.assign({}, root._pendingSends)
    delete p2[root.network]
    root._pendingSends = p2
    root.messages = []
    root.grouped = []
    composer.text = ""
    root._clearMediaForNetwork(root.network)
  }

  Timer {
    id: copiedTimer
    interval: 1200
    onTriggered: root.copied = false
  }

  Process {
    id: mkdirCache
    command: ["mkdir", "-p", root.captureDir]
    onExited: function(code) {
      if (code !== 0) {
        root.threadError = "Could not create the capture folder."
        return
      }
      root._runFfmpegRecord()
    }
  }

  Process {
    id: voiceProc
    stdinEnabled: true
    onExited: function(code) {
      stopFallbackTimer.stop()
      root.recording = false
      recordTimer.stop()
      var ok = (code === 0 || code === 255)
      if (!root.keepRecording) {
        if (root.voicePath !== "" && root.service)
          root.service.call("discardCapture", { path: root.voicePath }, null, root.network)
        root.voicePath = ""
        return
      }
      if (!ok) {
        root.threadError = "Recording failed (ffmpeg exit " + code + "). Check that a microphone is available."
        if (root.voicePath !== "" && root.service)
          root.service.call("discardCapture", { path: root.voicePath }, null, root.network)
        root.voicePath = ""
        return
      }
      if (root.pendingVoiceSeconds < 1) {
        root.threadError = "That was too short to send."
        if (root.service) root.service.call("discardCapture", { path: root.voicePath }, null, root.network)
        root.voicePath = ""
        return
      }
      root.pendingAttachment = root.voicePath
      root.voicePath = ""
    }
  }

  Process {
    id: playProc
    onExited: root.playingKey = ""
  }

  Timer {
    id: recordTimer
    interval: 1000
    repeat: true
    onTriggered: {
      root.recordSeconds += 1
      if (root.recordSeconds >= root.maxRecordSeconds) {
        root.recordSeconds = root.maxRecordSeconds
        root.stopRecording(true)
      }
    }
  }

  Timer {
    id: stopFallbackTimer
    interval: 3000
    onTriggered: if (voiceProc.running) voiceProc.running = false
  }

  Timer {
    id: audioRetry
    property var queue: []
    interval: 8000
    function schedule(key, attempt) {
      var q = queue.slice()
      q.push({ key: key, attempt: attempt })
      queue = q
      if (!running) start()
    }
    onTriggered: {
      var q = queue.slice()
      queue = []
      for (var i = 0; i < q.length; i++) root._fetchAudio(q[i].key, q[i].attempt)
    }
  }

  Timer {
    id: mediaRetry
    property var queue: []
    interval: 8000
    repeat: false
    function schedule(key, attempt) {
      var q = queue.slice()
      q.push({ key: key, attempt: attempt })
      queue = q
      if (!running) start()
    }
    onTriggered: {
      var q = queue.slice()
      queue = []
      for (var i = 0; i < q.length; i++) root.requestMedia(q[i].key, q[i].attempt)
    }
  }

  FileView {
    path: {
      var p = Quickshell.env("OMARCHY_PATH")
      return (p && p.length > 0 ? p : "/usr/share/omarchy") + "/shell/plugins/emojis/emojis.json"
    }
    onLoaded: {
      try {
        var parsed = JSON.parse(text())
        root.emojiList = Array.isArray(parsed) && parsed.length > 0 ? parsed : root.fallbackEmoji
      } catch (e) {
        root.emojiList = root.fallbackEmoji
      }
    }
    onLoadFailed: root.emojiList = root.fallbackEmoji
  }

  Item {
    id: listPane
    anchors.left: parent.left
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    width: Math.round(parent.width * 0.32)

    PanelSectionHeader {
      id: inboxHeader
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: parent.top
      text: "INBOX"
      color: root.dim
      foreground: root.foreground
      fontFamily: root.fontFamily
    }

    TextField {
      id: searchField
        objectName: "searchField"
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: inboxHeader.bottom
      anchors.topMargin: Style.space(6)
      placeholderText: "Search conversations"
      placeholderTextColor: root.dim
      font.pixelSize: fs(Style.font.body)
      foreground: root.foreground
      onTextChanged: root.searchQuery = text
      onActiveFocusChanged: root.composerFocus = activeFocus
    }

    Rectangle {
      id: unreadChip
      visible: root.unreadConversations > 0
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: searchField.bottom
      anchors.topMargin: Style.space(6)
      height: visible ? Style.space(28) : 0
      radius: Style.space(6)
      color: Style.normalFillFor(root.foreground, Color.accent)

      MouseArea {
        anchors.fill: parent
        cursorShape: Qt.PointingHandCursor
        onClicked: {
          for (var i = 0; i < root.conversations.length; i++) {
            if (root.conversations[i].unread === true) {
              root.selectConversation(root.conversations[i].id)
              convList.positionViewAtIndex(i, ListView.Contain)
              return
            }
          }
        }
      }

      Text {
        anchors.centerIn: parent
        text: root.unreadConversations === 1 ? "1 unread" : root.unreadConversations + " unread"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.caption)
        font.bold: true
      }
    }

    ListView {
      id: convList
      objectName: "convList"
      activeFocusOnTab: true
      keyNavigationEnabled: true
      Accessible.role: Accessible.List
      Accessible.name: "Conversations"
      Keys.onReturnPressed: {
        if (currentItem && currentItem.modelData) root.selectConversation(currentItem.modelData.id)
      }
      Keys.onEnterPressed: {
        if (currentItem && currentItem.modelData) root.selectConversation(currentItem.modelData.id)
      }
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: unreadChip.visible ? unreadChip.bottom : searchField.bottom
      anchors.bottom: parent.bottom
      anchors.topMargin: Style.space(8)
      clip: true
      spacing: Style.space(2)
      model: root.visibleConversations
      boundsBehavior: Flickable.StopAtBounds

      delegate: Rectangle {
        id: convItem
        required property int index
        required property var modelData
        Accessible.role: Accessible.ListItem
        Accessible.name: modelData.name || "Conversation"
        readonly property bool selected: modelData.id === root.selectedConvID
        width: convList.width
        height: Math.max(Style.space(60), fs(Style.font.bodySmall) + fs(Style.font.caption) + Style.space(24))
        radius: Style.space(4)
        color: selected
          ? root.selectedFill
          : (convMouse.containsMouse ? Style.hoverFillFor(root.foreground, Color.accent) : "transparent")
        border.width: selected || (convItem.ListView.isCurrentItem && convList.activeFocus) ? 1 : 0
        border.color: convItem.ListView.isCurrentItem && convList.activeFocus ? Color.accent : (selected ? Color.accent : "transparent")

        Rectangle {
          visible: convItem.selected
          anchors.left: parent.left
          anchors.top: parent.top
          anchors.bottom: parent.bottom
          width: Style.space(2)
          color: Color.accent
        }

        MouseArea {
          id: convMouse
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: { convList.currentIndex = index; convList.forceActiveFocus(); root.selectConversation(convItem.modelData.id) }
        }

        Avatar {
          id: convAvatar
          anchors.left: parent.left
          anchors.leftMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          implicitWidth: Style.space(32)
          implicitHeight: Style.space(32)
          imagePath: convItem.modelData.avatarPath || ""
          initials: convItem.modelData.initials || "#"
          hexColor: convItem.modelData.avatarColor || ""
          seed: convItem.modelData.id || ""
          fontFamily: root.fontFamily
        }

        Text {
          id: convTime
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.top: parent.top
          anchors.topMargin: Style.space(8)
          text: Model.relativeTime(convItem.modelData.timestamp)
          color: convItem.selected ? root.selectedMeta : root.dim
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }

        Text {
          anchors.left: convAvatar.right
          anchors.leftMargin: Style.space(8)
          anchors.right: convTime.left
          anchors.rightMargin: Style.space(8)
          anchors.top: parent.top
          anchors.topMargin: Style.space(7)
          elide: Text.ElideRight
          text: convItem.modelData.name || "(no name)"
          color: convItem.selected ? root.selectedInk : root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.bodySmall)
          font.bold: convItem.modelData.unread === true
        }

        Text {
          anchors.left: convAvatar.right
          anchors.leftMargin: Style.space(8)
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.bottom: parent.bottom
          anchors.bottomMargin: Style.space(7)
          elide: Text.ElideRight
          text: Model.previewText(convItem.modelData)
          color: convItem.selected
            ? root.selectedMeta
            : (convItem.modelData.unread === true ? root.foreground : root.dim)
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }
      }
    }
  }

  Rectangle {
    id: paneRule
    anchors.left: listPane.right
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    anchors.leftMargin: Style.space(8)
    width: 1
    color: Color.popups.border
  }

  Item {
    id: threadPane
    anchors.left: paneRule.right
    anchors.leftMargin: Style.space(10)
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.bottom: parent.bottom

    Text {
      anchors.centerIn: parent
      width: Math.max(0, parent.width - Style.space(32))
      horizontalAlignment: Text.AlignHCenter
      wrapMode: Text.Wrap
      visible: root.selectedConvID === ""
      text: root.conversations.length === 0 ? "No conversations yet. Refresh after your service finishes syncing." : "Choose a conversation to read and reply."
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Item {
      id: threadHeader
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: parent.top
      height: root.selectedConvID === "" ? 0 : Style.space(36)
      visible: root.selectedConvID !== ""

      Avatar {
        id: threadAvatar
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        implicitWidth: Style.space(26)
        implicitHeight: Style.space(26)
        imagePath: root.selectedConv ? (root.selectedConv.avatarPath || "") : ""
        initials: root.selectedConv ? (root.selectedConv.initials || "#") : "#"
        hexColor: root.selectedConv ? (root.selectedConv.avatarColor || "") : ""
        seed: root.selectedConvID
        fontFamily: root.fontFamily
      }

      Column {
        anchors.left: threadAvatar.right
        anchors.leftMargin: Style.space(8)
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        spacing: 0

        Text {
          width: parent.width
          elide: Text.ElideRight
          text: root.selectedConv ? (root.selectedConv.name || "") : ""
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          font.bold: true
        }

        Text {
          width: parent.width
          elide: Text.ElideRight
          text: root.networkLabel
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }
      }
    }

    PanelSeparator {
      id: threadSep
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: threadHeader.bottom
      anchors.topMargin: Style.space(6)
      visible: root.selectedConvID !== ""
    }

    Row {
      id: historyControls
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: threadSep.bottom
      height: root.selectedConvID !== "" ? Math.max(Style.space(40), historyStatus.implicitHeight + Style.space(12), children[0].implicitHeight) : 0
      spacing: Style.space(8)
      visible: root.selectedConvID !== "" && (root.messages.length > 0 || root.hasOlder || root.historyNotice !== "")

      Button {
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: root.loadingOlder ? "Loading older messages..." : "Load older messages"
        objectName: "loadOlderButton"
        anchors.verticalCenter: parent.verticalCenter
        visible: root.hasOlder || root.loadingOlder
        enabled: !root.loadingOlder && !root.loadingMessages
        text: root.loadingOlder ? "Loading older messages..." : "Load older messages"
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.loadOlderMessages()
      }
      Text {
        id: historyStatus
        objectName: "historyStatus"
        anchors.verticalCenter: parent.verticalCenter
        width: Math.max(0, parent.width - (parent.children[0].visible ? parent.children[0].width + parent.spacing : 0))
        text: root.historyError || root.historyNotice || (!root.hasOlder && !root.loadingMessages ? (root.isWhatsApp ? "Showing cached WhatsApp history. On-demand phone history is not requested in this version." : "All available history loaded") : "")
        textFormat: Text.PlainText
        wrapMode: Text.WordWrap
        color: root.historyError ? root.errorInk : root.dim
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.caption)
      }
    }

    ListView {
      id: messageList
      objectName: "messageList"
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: historyControls.bottom
      anchors.bottom: attachmentBar.visible ? attachmentBar.top : composerRow.top
      anchors.leftMargin: Style.space(4)
      anchors.rightMargin: Style.space(4)
      anchors.topMargin: Style.space(8)
      anchors.bottomMargin: Style.space(8)
      visible: root.selectedConvID !== ""
      clip: true
      spacing: Style.space(2)
      model: messageRows
      boundsBehavior: Flickable.StopAtBounds

      delegate: Item {
        id: row
        required property var entry
        readonly property var modelData: entry
        readonly property bool isDay: modelData.kind === "day"
        readonly property var msg: isDay ? null : modelData.message
        readonly property bool mine: msg ? msg.fromMe === true : false
        readonly property bool startsRun: !isDay && modelData.startsRun === true
        readonly property int maxBubble: Math.min(Math.round(width * 0.78), Style.space(420))
        readonly property bool hasText: msg && String(msg.text || "") !== ""
        property var attachments: []
        onMsgChanged: {
          var next = msg && msg.attachments ? msg.attachments : []
          if (JSON.stringify(attachments) !== JSON.stringify(next)) attachments = next
        }
        readonly property var copyTargets: hasText ? Model.extractCopyTargets(msg.text) : []

        width: messageList.width
        height: isDay ? Style.space(28) : bubbleCol.implicitHeight + (startsRun ? Style.space(8) : 0)

        Text {
          visible: row.isDay
          anchors.horizontalCenter: parent.horizontalCenter
          anchors.verticalCenter: parent.verticalCenter
          text: row.isDay ? (row.modelData.label || "") : ""
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }

        Column {
          id: bubbleCol
          visible: !row.isDay
          anchors.right: row.mine ? parent.right : undefined
          anchors.left: row.mine ? undefined : parent.left
          anchors.bottom: parent.bottom
          width: row.maxBubble
          spacing: Style.space(4)

          Text {
            width: parent.width
            visible: !row.mine && row.startsRun && root.selectedConv && root.selectedConv.isGroup === true
              && String(row.msg && row.msg.senderName || "") !== ""
            height: visible ? implicitHeight : 0
            text: row.msg ? (row.msg.senderName || "") : ""
            color: root.dim
            elide: Text.ElideRight
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.caption)
          }

          Rectangle {
            width: parent.width
            height: bubbleInner.implicitHeight + Style.space(16)
            radius: Style.space(8)
            color: row.mine ? root.mineFill : root.theirsFill
            border.width: 1
            border.color: row.mine ? Color.accent : Color.popups.border

            MouseArea {
              anchors.fill: parent
              acceptedButtons: Qt.RightButton
              onClicked: {
                if (!row.hasText) return
                root.copyText(row.msg.text)
              }
            }

            Column {
              id: bubbleInner
              anchors.left: parent.left
              anchors.right: parent.right
              anchors.top: parent.top
              anchors.margins: Style.space(8)
              spacing: Style.space(6)

              Repeater {
                model: row.attachments
                delegate: Item {
                  id: attachItem
                  required property var modelData
                  readonly property bool isImage: !!(modelData && (modelData.isImage || modelData.isGif) && !modelData.isAudio && !modelData.isVideo)
                  readonly property bool isVoice: !!(modelData && modelData.isAudio)
                  readonly property bool isVideo: !!(modelData && modelData.isVideo)
                  readonly property bool isVideoGif: !!(isVideo && modelData && modelData.isGif)
                  readonly property bool loadsInline: isImage || isVideoGif
                  readonly property string mediaKey: modelData && modelData.key ? modelData.key : ""
                  readonly property string mediaPath: mediaKey && root.mediaPaths[mediaKey]
                    ? root.mediaPaths[mediaKey] : (modelData && modelData.path ? modelData.path : "")
                  readonly property string mediaState: mediaKey && root.mediaRequests[mediaKey] ? root.mediaRequests[mediaKey] : ""
                  readonly property bool mediaFailed: loadsInline && mediaPath === "" && mediaState === "failed"
                  readonly property bool mediaLoading: loadsInline && !mediaFailed
                    && !(isImage ? thumb.ready : (videoGifLoader.item && videoGifLoader.item.ready))
                    && !(isImage ? thumb.hasError : (videoGifLoader.item && videoGifLoader.item.hasError))
                  readonly property bool playingThis: isVoice && root.playingKey === mediaKey
                  readonly property bool loadingThis: isVoice && root.audioWaitingKey === mediaKey
                  width: parent.width
                  height: {
                    if (isVoice || (isVideo && !isVideoGif)) return Style.space(36)
                    if (loadsInline) {
                      if (mediaPath !== "") return isImage ? (thumb.height || Style.space(96))
                        : ((videoGifLoader.item && videoGifLoader.item.height) || Style.space(96))
                      return Style.space(96)
                    }
                    return 0
                  }
                  visible: isImage || isVoice || isVideo
                  onMediaKeyChanged: if (loadsInline && mediaKey && !mediaPath) root.requestMedia(mediaKey)
                  Component.onCompleted: if (loadsInline && mediaKey && !mediaPath) root.requestMedia(mediaKey)

                  Rectangle {
                    visible: parent.isVoice
                    width: Math.min(parent.width, Style.space(220))
                    height: Style.space(34)
                    radius: height / 2
                    color: Style.normalFillFor(row.mine ? root.mineInk : root.theirsInk, Color.accent)
                    border.width: 1
                    border.color: row.mine ? root.mineInk : Color.popups.border

                    MouseArea {
                      anchors.fill: parent
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.playAttachment(parent.parent.mediaKey)
                    }

                    Text {
                      anchors.centerIn: parent
                      text: parent.parent.loadingThis ? "Fetching voice"
                        : (parent.parent.playingThis ? "Stop voice" : "Play voice")
                      color: row.mine ? root.mineInk : root.theirsInk
                      font.family: root.fontFamily
                      font.pixelSize: fs(Style.font.caption)
                      font.bold: true
                    }
                  }

                  Rectangle {
                    visible: parent.isVideo && !parent.isVideoGif
                    width: Math.min(parent.width, Style.space(220))
                    height: Style.space(34)
                    radius: height / 2
                    color: Style.normalFillFor(row.mine ? root.mineInk : root.theirsInk, Color.accent)
                    border.width: 1
                    border.color: row.mine ? root.mineInk : Color.popups.border

                    MouseArea {
                      anchors.fill: parent
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.openVideo(parent.parent.mediaKey)
                    }

                    Text {
                      anchors.centerIn: parent
                      text: "Open video"
                      color: row.mine ? root.mineInk : root.theirsInk
                      font.family: root.fontFamily
                      font.pixelSize: fs(Style.font.caption)
                      font.bold: true
                    }
                  }

                  Rectangle {
                    visible: parent.mediaLoading
                    width: Math.min(parent.width, Style.space(160))
                    height: Style.space(34)
                    radius: height / 2
                    color: Style.normalFillFor(row.mine ? root.mineInk : root.theirsInk, Color.accent)
                    border.width: 1
                    border.color: row.mine ? root.mineInk : Color.popups.border

                    Text {
                      anchors.centerIn: parent
                      text: "Loading media…"
                      color: row.mine ? root.mineInk : root.theirsInk
                      font.family: root.fontFamily
                      font.pixelSize: fs(Style.font.caption)
                    }
                  }

                  Rectangle {
                    visible: parent.mediaFailed
                    width: Math.min(parent.width, Style.space(220))
                    height: Style.space(34)
                    radius: height / 2
                    color: Style.normalFillFor(row.mine ? root.mineInk : root.theirsInk, Color.accent)
                    border.width: 1
                    border.color: row.mine ? root.mineInk : Color.popups.border

                    MouseArea {
                      anchors.fill: parent
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.requestMedia(parent.parent.mediaKey, 0)
                    }

                    Text {
                      anchors.centerIn: parent
                      text: "Media failed. Tap to retry"
                      color: row.mine ? root.mineInk : root.theirsInk
                      font.family: root.fontFamily
                      font.pixelSize: fs(Style.font.caption)
                      font.bold: true
                    }
                  }

                  MediaThumb {
                    id: thumb
                    visible: parent.isImage && parent.mediaPath !== ""
                    path: parent.isImage ? parent.mediaPath : ""
                    mimeType: modelData ? (modelData.mimeType || "") : ""
                    fileName: modelData ? (modelData.name || "") : ""
                    playing: root.panelOpen
                    maxEdge: Math.min(parent.width, Style.space(280))
                    onClicked: root.openImage(parent.mediaPath)
                    onLoadFailed: {
                      if (parent.mediaKey) {
                        root._evictMedia(parent.mediaKey)
                        root.setMediaRequest(parent.mediaKey, "failed")
                      }
                    }
                  }

                  Loader {
                    id: videoGifLoader
                    active: parent.isVideoGif
                    visible: active && parent.mediaPath !== ""
                    sourceComponent: Component {
                      LoopingVideoThumb {
                        path: attachItem.isVideoGif ? attachItem.mediaPath : ""
                        playing: root.panelOpen && videoGifLoader.visible
                        maxEdge: Math.min(attachItem.width, Style.space(280))
                        onLoadFailed: {
                          if (attachItem.mediaKey) {
                            root._evictMedia(attachItem.mediaKey)
                            root.setMediaRequest(attachItem.mediaKey, "failed")
                          }
                        }
                      }
                    }
                  }
                }
              }

              Text {
                id: bubbleText
                width: parent.width
                visible: !!(row.hasText || (row.msg && row.msg.deleted === true))
                height: visible ? implicitHeight : 0
                text: {
                  if (row.msg && row.msg.deleted) return "Message deleted"
                  return Model.linkify(row.msg ? (row.msg.text || "") : "")
                }
                wrapMode: Text.Wrap
                color: {
                  if (row.msg && row.msg.deleted) return row.mine ? root.mineMeta : root.theirsMeta
                  return row.mine ? root.mineInk : root.theirsInk
                }
                linkColor: {
                  if (row.mine) return root.mineInk
                  return Model.readableInk(root.theirsFill, Color.accent)
                }
                font.italic: row.msg && row.msg.deleted === true
                font.family: root.fontFamily
                font.pixelSize: fs(Style.font.body)
                textFormat: row.msg && row.msg.deleted ? Text.PlainText : Text.StyledText
                onLinkActivated: function(link) { root.requestOpenUrl(link) }

                MouseArea {
                  anchors.fill: parent
                  acceptedButtons: Qt.LeftButton
                  enabled: bubbleText.hoveredLink !== ""
                  cursorShape: Qt.PointingHandCursor
                  onClicked: root.requestOpenUrl(bubbleText.hoveredLink)
                }
              }

              Flow {
                width: parent.width
                spacing: Style.space(4)
                visible: row.copyTargets && row.copyTargets.length > 0
                Repeater {
                  model: row.copyTargets
                  Rectangle {
                    required property var modelData
                    readonly property string code: String(modelData)
                    width: copyLabel.implicitWidth + Style.space(12)
                    height: Style.space(22)
                    radius: height / 2
                    color: Style.normalFillFor(row.mine ? root.mineInk : root.theirsInk, Color.accent)
                    border.width: 1
                    border.color: row.mine ? root.mineInk : Color.accent

                    Text {
                      id: copyLabel
                      anchors.centerIn: parent
                      text: "Copy " + parent.code
                      color: row.mine ? root.mineInk : root.theirsInk
                      font.family: root.fontFamily
                      font.pixelSize: fs(Style.font.caption)
                    }

                    MouseArea {
                      anchors.fill: parent
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.copyText(parent.code)
                    }
                  }
                }
              }

              Row {
                spacing: Style.space(6)
                Text {
                  text: row.msg ? Model.bubbleTime(row.msg.timestamp) : ""
                  color: row.mine ? root.mineMeta : root.theirsMeta
                  font.family: root.fontFamily
                  font.pixelSize: fs(Style.font.caption)
                }
                Text {
                  visible: row.mine && text !== ""
                  text: Model.receiptLabel(row.msg)
                  color: {
                    var s = String(row.msg && (row.msg.delivery || row.msg.status) || "")
                    if (s === "failed") return Model.readableInk(row.mine ? root.mineFill : root.theirsFill, Color.urgent)
                    return root.mineMeta
                  }
                  font.family: root.fontFamily
                  font.pixelSize: fs(Style.font.caption)
                }
                Item { width: Style.space(4); height: 1 }
                Text {
                  visible: root.reactionsSupported && row.msg && !row.msg.deleted
                  text: (row.msg && root.reactingTo === row.msg.id) ? "Close" : "React"
                  color: row.mine ? root.mineMeta : root.theirsMeta
                  font.family: root.fontFamily
                  font.pixelSize: fs(Style.font.caption)
                  font.underline: true
                  MouseArea {
                    anchors.fill: parent
                    cursorShape: Qt.PointingHandCursor
                    onClicked: if (row.msg && !row.msg.deleted)
                      root.reactingTo = root.reactingTo === row.msg.id ? "" : row.msg.id
                  }
                }
              }
            }
          }

          Flow {
            width: parent.width
            spacing: Style.space(4)
            visible: !!(row.msg && row.msg.reactions && row.msg.reactions.length)
            Repeater {
              model: row.msg && row.msg.reactions ? row.msg.reactions : []
              Rectangle {
                required property var modelData
                width: chipLabel.implicitWidth + Style.space(10)
                height: Style.space(22)
                radius: height / 2
                color: modelData.mine
                  ? root.selectedFill
                  : Style.normalFillFor(root.foreground, Color.accent)
                border.width: 1
                border.color: modelData.mine ? Color.accent : Color.popups.border

                Text {
                  id: chipLabel
                  anchors.centerIn: parent
                  text: (modelData.emoji || "") + (modelData.count > 1 ? " " + modelData.count : "")
                  color: modelData.mine ? root.selectedInk : root.foreground
                  font.family: root.fontFamily
                  font.pixelSize: fs(Style.font.bodySmall)
                }

                MouseArea {
                  anchors.fill: parent
                  cursorShape: Qt.PointingHandCursor
                  onClicked: if (row.msg) root.react(row.msg.id, modelData.emoji)
                }
              }
            }
          }

          Row {
            visible: row.msg && root.reactingTo === row.msg.id
            spacing: Style.space(4)
            Repeater {
              model: root.reactionChoices
              Button {
                focusable: true
                Accessible.role: Accessible.Button
                Accessible.name: modelData
                required property string modelData
                text: modelData
                bordered: true
                selected: {
                  var rx = row.msg && row.msg.reactions ? row.msg.reactions : []
                  for (var i = 0; i < rx.length; i++) {
                    if (rx[i].mine && rx[i].emoji === modelData) return true
                  }
                  return false
                }
                foreground: root.foreground
                fontFamily: root.fontFamily
                onClicked: if (row.msg) root.react(row.msg.id, modelData)
              }
            }
            Button {
              focusable: true
              Accessible.role: Accessible.Button
              Accessible.name: "+"
              text: "+"
              bordered: true
              tooltipText: "More reactions"
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: {
                root.gifPickerOpen = false
                root.emojiPickerForReact = true
                root.emojiPickerOpen = true
              }
            }
          }
        }
      }
    }

    Rectangle {
      id: attachmentBar
      visible: root.pendingAttachment !== "" && root.selectedConvID !== ""
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(6)
      height: visible ? (root.pendingIsVoice ? Style.space(48) : Style.space(132)) : 0
      radius: Style.space(8)
      color: Color.popups.background
      border.width: 1
      border.color: Color.popups.border

      Item {
        visible: root.pendingIsVoice
        anchors.fill: parent
        anchors.margins: Style.space(8)

        Text {
          anchors.left: parent.left
          anchors.verticalCenter: parent.verticalCenter
          text: "Voice  ·  " + Model.formatDuration(Math.min(root.pendingVoiceSeconds, root.maxRecordSeconds))
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.bodySmall)
          font.bold: true
        }

        Row {
          anchors.right: parent.right
          anchors.verticalCenter: parent.verticalCenter
          spacing: Style.space(6)
          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: root.playingVoice ? "Stop" : "Play"
            text: root.playingVoice ? "Stop" : "Play"
            foreground: root.foreground
            fontFamily: root.fontFamily
            enabled: !root.sendingMedia
            onClicked: root.playingVoice ? root.stopPlayback() : root.playPendingVoice()
          }
          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: "Redo"
            text: "Redo"
            foreground: root.foreground
            fontFamily: root.fontFamily
            enabled: !root.sendingMedia
            onClicked: { root.stopPlayback(); root.startRecording() }
          }
          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: "Cancel"
            text: "Cancel"
            foreground: root.dim
            fontFamily: root.fontFamily
            enabled: !root.sendingMedia
            onClicked: root.cancelAttachment()
          }
          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: root.sendingMedia ? "Sending" : "Send"
            text: root.sendingMedia ? "Sending" : "Send"
            bordered: true
            foreground: root.foreground
            fontFamily: root.fontFamily
            enabled: !root.sendingMedia
            onClicked: root.sendAttachment("")
          }
        }
      }

      MediaThumb {
        id: attachPreview
        visible: !root.pendingIsVoice
        anchors.left: parent.left
        anchors.top: parent.top
        anchors.margins: Style.space(8)
        maxEdge: Style.space(150)
        height: parent.height - Style.space(16)
        width: visible ? (implicitWidth > 0 ? Math.min(Style.space(150), implicitWidth) : Style.space(96)) : 0
        path: root.pendingIsVoice ? "" : root.pendingAttachment
        playing: root.panelOpen && root.pendingAttachment !== "" && !root.pendingIsVoice
        onClicked: root.openImage(root.pendingAttachment)
      }

      TextField {
        id: attachCaption
        objectName: "attachCaption"
        visible: !root.pendingIsVoice
        anchors.left: attachPreview.right
        anchors.leftMargin: Style.space(10)
        anchors.right: parent.right
        anchors.rightMargin: Style.space(10)
        anchors.top: parent.top
        anchors.topMargin: Style.space(10)
        placeholderText: "Add a caption (optional)"
        placeholderTextColor: root.dim
        font.pixelSize: fs(Style.font.body)
        foreground: root.foreground
        enabled: !root.sendingMedia
        onAccepted: root.sendAttachment(text)
        onActiveFocusChanged: root.composerFocus = activeFocus
      }

      Row {
        visible: !root.pendingIsVoice
        anchors.right: parent.right
        anchors.rightMargin: Style.space(10)
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Style.space(10)
        spacing: Style.space(6)
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: "Cancel"
          text: "Cancel"
          foreground: root.dim
          fontFamily: root.fontFamily
          onClicked: root.cancelAttachment()
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: root.sendingMedia ? "Sending" : (root.pendingIsGif ? "Send GIF" : "Send image")
          text: root.sendingMedia ? "Sending" : (root.pendingIsGif ? "Send GIF" : "Send image")
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          enabled: !root.sendingMedia
          onClicked: root.sendAttachment(attachCaption.text)
        }
      }
    }

    Rectangle {
      id: composerRow
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: parent.bottom
      height: root.selectedConvID === "" ? 0 : Math.max(sendButton.implicitHeight, composer.implicitHeight) + Style.space(8)
      visible: root.selectedConvID !== ""
      radius: Style.space(6)
      color: "transparent"
      border.width: 1
      border.color: Color.popups.border

      Button {
        id: sendButton
        objectName: "sendButton"
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: "Send message"
        anchors.right: parent.right
        anchors.rightMargin: Style.space(4)
        anchors.verticalCenter: parent.verticalCenter
        text: "Send"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        enabled: composer.enabled && composer.text.trim() !== ""
        onClicked: {
          root.sendMessage(composer.text)
          composer.text = ""
        }
      }

      PanelActionButton {
        id: attachButton
        objectName: "attachButton"
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: root.isMessenger ? "Attach image or file" : "Attach photo or GIF"
        anchors.left: parent.left
        anchors.leftMargin: Style.space(4)
        anchors.verticalCenter: parent.verticalCenter
        size: visible ? fs(Style.space(28)) : 0
        fontSize: fs(Style.space(16))
        iconText: "󰁦"
        tooltipText: root.isMessenger ? "Attach an image or file" : "Attach a photo or GIF"
        visible: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        enabled: composer.enabled && !root.sendingMedia
        onClicked: root.attachFromDisk()
      }

      PanelActionButton {
        id: micButton
        objectName: "micButton"
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: "Microphone"
        visible: true
        anchors.left: attachButton.right
        anchors.leftMargin: visible ? Style.space(2) : 0
        anchors.verticalCenter: parent.verticalCenter
        size: visible ? fs(Style.space(28)) : 0
        fontSize: fs(Style.space(16))
        iconText: root.recording ? "󰓛" : "󰍬"
        tooltipText: root.recording ? "Stop recording" : "Record a voice message"
        foreground: root.recording ? root.errorInk : root.foreground
        fontFamily: root.fontFamily
        enabled: visible && composer.enabled && !root.sendingMedia
        onClicked: root.recording ? root.stopRecording(true) : root.startRecording()
      }

      PanelActionButton {
        id: gifButton
        objectName: "gifButton"
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: "Search GIFs"
        visible: !root.isTelegram
        anchors.left: micButton.visible ? micButton.right : attachButton.right
        anchors.leftMargin: visible ? Style.space(2) : 0
        anchors.verticalCenter: parent.verticalCenter
        size: visible ? fs(Style.space(28)) : 0
        iconText: "GIF"
        fontSize: fs(Style.space(10))
        tooltipText: "Search GIFs"
        foreground: root.foreground
        fontFamily: root.fontFamily
        enabled: visible && composer.enabled && !root.sendingMedia
        onClicked: root.openGifPicker()
      }

      PanelActionButton {
        id: emojiButton
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: "Insert emoji"
        anchors.left: gifButton.visible ? gifButton.right : (micButton.visible ? micButton.right : attachButton.right)
        anchors.leftMargin: Style.space(2)
        anchors.verticalCenter: parent.verticalCenter
        size: fs(Style.space(28))
        fontSize: fs(Style.space(16))
        iconText: "󰱱"
        tooltipText: "Insert emoji"
        foreground: root.foreground
        fontFamily: root.fontFamily
        enabled: composer.enabled
        onClicked: {
          root.gifPickerOpen = false
          root.emojiPickerForReact = false
          root.emojiPickerOpen = !root.emojiPickerOpen
        }
      }

      TextField {
        id: composer
        objectName: "composer"
        placeholderTextColor: root.dim
        font.pixelSize: fs(Style.font.body)
        anchors.left: emojiButton.right
        anchors.leftMargin: Style.space(4)
        anchors.right: sendButton.left
        anchors.rightMargin: Style.space(6)
        anchors.verticalCenter: parent.verticalCenter
        foreground: root.foreground
        placeholderText: {
          if (root.selectedConv && root.selectedConv.readOnly) return "This conversation is read-only"
          if (root.recording) return "Recording " + Model.formatDuration(root.recordSeconds)
          return "Write a message"
        }
        enabled: root.service && root.service.connected && (typeof root.service.stateFor === "function" ? root.service.stateFor(root.network) : root.service.state) === "connected" && !(root.selectedConv && root.selectedConv.readOnly)
        onAccepted: {
          root.sendMessage(text)
          text = ""
        }
        onActiveFocusChanged: root.composerFocus = activeFocus
      }
    }

    Rectangle {
      id: gifPicker
      visible: root.gifPickerOpen && root.selectedConvID !== ""
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(6)
      height: visible ? Style.space(220) : 0
      radius: Style.space(8)
      color: Color.popups.background
      border.width: 1
      border.color: Color.popups.border
      clip: true

      Column {
        anchors.fill: parent
        anchors.margins: Style.space(8)
        spacing: Style.space(6)

        TextField {
          id: gifSearchField
        objectName: "gifSearchField"
          width: parent.width
          visible: !root.gifNeedsKey
          placeholderText: "Search GIPHY"
          placeholderTextColor: root.dim
          foreground: root.foreground
          onAccepted: root.searchGifs(text)
          onTextChanged: gifDebounce.restart()
          onActiveFocusChanged: root.composerFocus = activeFocus
        }

        Text {
          width: parent.width
          visible: root.gifNeedsKey
          wrapMode: Text.WordWrap
          text: "Add your own GIPHY API key in Settings to search GIFs. You can attach a local GIF without a key."
          color: root.theirsInk
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }

        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: "Open settings"
          visible: root.gifNeedsKey
          text: "Open settings"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: {
            root.gifPickerOpen = false
            if (root.host) root.host.settingsOpen = true
          }
        }

        Text {
          width: parent.width
          visible: root.gifError !== ""
          wrapMode: Text.WordWrap
          text: root.gifError
          color: root.errorInk
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }

        GridView {
          id: gifGrid
          activeFocusOnTab: true
          keyNavigationEnabled: true
          Keys.onReturnPressed: if (currentItem) root.pickGif(currentItem.modelData)
          Keys.onEnterPressed: if (currentItem) root.pickGif(currentItem.modelData)
          Keys.onEscapePressed: { root.gifPickerOpen = false; composer.forceActiveFocus() }
          highlight: Rectangle { color: "transparent"; border.width: 2; border.color: Color.accent }
          highlightFollowsCurrentItem: true
          width: parent.width
          height: Style.space(128)
          visible: !root.gifNeedsKey
          cellWidth: Style.space(88)
          cellHeight: Style.space(72)
          clip: true
          model: root.gifList
          delegate: Item {
            required property var modelData
            width: Style.space(84)
            height: Style.space(68)

            MediaThumb {
              anchors.fill: parent
              remoteUrl: modelData ? (modelData.previewURL || "") : ""
              playing: root.panelOpen && root.gifPickerOpen
              maxEdge: Style.space(84)
            }

            MouseArea {
              anchors.fill: parent
              cursorShape: Qt.PointingHandCursor
              onClicked: root.pickGif(modelData)
            }
          }
        }

        Text {
          width: parent.width
          visible: !root.gifNeedsKey
          text: root.gifAttribution
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }
      }

      Timer {
        id: gifDebounce
        interval: 280
        onTriggered: if (root.gifPickerOpen) root.searchGifs(gifSearchField.text)
      }
    }

    Rectangle {
      visible: root.emojiPickerOpen && root.selectedConvID !== ""
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(6)
      height: visible ? Style.space(280) : 0
      radius: Style.space(8)
      color: Color.popups.background
      border.width: 1
      border.color: Color.popups.border
      clip: true

      GridView {
        id: emojiGrid
        objectName: "emojiGrid"
        activeFocusOnTab: true
        keyNavigationEnabled: true
        Keys.onReturnPressed: if (currentItem) root.chooseEmoji(currentItem.text)
        Keys.onEnterPressed: if (currentItem) root.chooseEmoji(currentItem.text)
        Keys.onEscapePressed: { root.emojiPickerOpen = false; composer.forceActiveFocus() }
        highlight: Rectangle { color: "transparent"; border.width: 2; border.color: Color.accent }
        highlightFollowsCurrentItem: true
        anchors.fill: parent
        anchors.margins: Style.space(8)
        cellWidth: Style.space(52)
        cellHeight: Style.space(52)
        model: root.emojiList
        clip: true
        delegate: Text {
          required property var modelData
          width: Style.space(52)
          height: Style.space(52)
          horizontalAlignment: Text.AlignHCenter
          verticalAlignment: Text.AlignVCenter
          text: modelData.e || modelData.emoji || ""
          font.pixelSize: fs(Style.space(32))
          renderType: Text.NativeRendering
          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: {
              root.chooseEmoji(parent.text)
            }
          }
        }
      }
    }

    Text {
      anchors.horizontalCenter: parent.horizontalCenter
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(8)
      visible: root.copied
      text: "Copied"
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.caption)
    }

    Text {
      objectName: "threadErrorLabel"
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(4)
      visible: text !== "" && !attachmentBar.visible
      text: root.threadError || (root.service ? root.service.refreshError : "")
      color: root.errorInk
      wrapMode: Text.WordWrap
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.caption)
    }
  }

  ConfirmDialog {
    id: linkDialog
    anchors.fill: parent
    opened: root.linkConfirmOpen
    message: "Open this link in a browser?\n\n" + root.pendingUrl
    confirmText: "Open"
    cancelText: "Cancel"
    foreground: root.foreground
    background: Color.popups.background
    fontFamily: root.fontFamily
    onConfirmed: root.confirmOpenUrl()
    onCanceled: root.cancelOpenUrl()
    Keys.onPressed: function(event) {
      if (handleKey(event)) event.accepted = true
    }
  }

  onLinkConfirmOpenChanged: if (linkConfirmOpen) linkDialog.forceActiveFocus()
}
