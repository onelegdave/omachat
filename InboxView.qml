import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model
import "EmojiSearch.js" as EmojiSearch

Item {
  id: root

  property var service: null
  readonly property var keyboardInitialFocus: sidebarVisible ? convList : messageList
  property color foreground: Model.readableInk(Color.popups.background, Color.popups.text, 7)
  property string fontFamily: Style.font.family
  property real uiScale: 1
  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }
  property var host: null
  property var settings: null
  property var keyboardActionRouter: null
  property string network: "gmessages"
  property bool detailOnly: false
  property string detailConversationID: ""
  property bool sidebarCollapsed: false
  property var lastConversations: ({})
  readonly property bool sidebarVisible: !sidebarCollapsed || selectedConvID === ""
  signal conversationSelected(string network, string id)
  readonly property bool isWhatsApp: network === "whatsapp"
  readonly property bool isTelegram: network === "telegram"
  readonly property bool isMessenger: network === "messenger"
  readonly property bool isSignal: network === "signal"
  readonly property bool reactionsSupported: true
  property string networkLabel: isWhatsApp ? "WhatsApp" : (isTelegram ? "Telegram" : (isMessenger ? "Messenger" : (isSignal ? "Signal" : "Google Messages")))

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
  property string historyFetchState: ""
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
  property string reactingTo: ""
  property bool copied: false
  property bool composerFocus: false
  property bool linkConfirmOpen: false
  property bool newChatOpen: false
  property bool newChatGroup: false
  property bool newChatLoading: false
  property string newChatError: ""
  property var newChatTargets: []
  property var newChatSelected: ({})
  property string pendingUrl: ""
  property var emojiList: []
  property var emojiBundleRows: []
  property var emojiSearchIndex: null
  property var emojiSearchResults: []
  property string emojiSearchQuery: ""
  property bool keepPreviousEmojiSearchText: false
  property bool emojiSearchDataLoaded: false
  property var emojiPickerOpener: null
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

  readonly property var visibleNewChatTargets: {
    var query = newChatSearch ? newChatSearch.text.trim().toLowerCase() : ""
    if (query === "") return newChatTargets
    return newChatTargets.filter(function(target) {
      return String(target.name || "").toLowerCase().indexOf(query) >= 0
        || String(target.detail || "").toLowerCase().indexOf(query) >= 0
    })
  }

  readonly property int newChatSelectionCount: Object.keys(newChatSelected).length


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
    reactingTo = ""
    threadError = ""

    selectedConvID = ""
    messages = []
    grouped = []
    if (composer) composer.text = ""
    if (root.detailOnly) Qt.callLater(root.restoreConversation)
    else restoreConversation()
  }

  onConversationsChanged: restoreConversation()
  onDetailConversationIDChanged: {
    if (!root.detailOnly || !root.detailConversationID || root.detailConversationID === root.selectedConvID) return
    if (root.conversations.some(function(conv) { return conv.id === root.detailConversationID }))
      root.selectConversation(root.detailConversationID, false)
  }
  onLastConversationsChanged: restoreConversation()
  Component.onCompleted: restoreConversation()

  function restoreConversation() {
    if (!composer || selectedConvID !== "") return
    var sessionID = _selectedByNet[network] || ""
    var id = root.detailOnly ? root.detailConversationID : (sessionID || lastConversations[network] || "")
    var canRestore = root.detailOnly
      ? conversations.some(function(conv) { return conv.id === id })
      : (sessionID || conversations.some(function(conv) { return conv.id === id }))
    if (id && canRestore) {
      selectConversation(id, false)
      if (panelOpen && !sidebarVisible) Qt.callLater(function() {
        if (root.panelOpen && root.selectedConvID === id && !root.composerFocus) messageList.forceActiveFocus()
      })
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

  function isKeyboardEditing() {
    return composerFocus || linkConfirmOpen || newChatOpen || emojiPickerOpen
  }

  function hasKeyboardModal() { return linkConfirmOpen || newChatOpen || emojiPickerOpen }

  function updateEmojiSearchResults() {
    if (emojiSearchIndex) emojiSearchResults = EmojiSearch.search(emojiSearchIndex, emojiSearchQuery)
    else emojiSearchResults = fallbackEmoji.slice()
    if (emojiGrid) emojiGrid.currentIndex = emojiSearchResults.length ? 0 : -1
  }

  function rebuildEmojiSearchIndex() {
    var rows = []
    var byGlyph = Object.create(null)
    function add(source) {
      if (!source || !Array.isArray(source)) return
      for (var i = 0; i < source.length; i++) {
        var item = source[i] || {}
        var glyph = String(item.e || "")
        if (!glyph) continue
        var key = "$" + glyph.replace(/[\uFE0E\uFE0F]/g, "")
        var row = byGlyph[key]
        if (!row) {
          row = { e: glyph, label: String(item.label || ""), keywords: [], aliases: [] }
          byGlyph[key] = row
          rows.push(row)
        } else if (!row.label && item.label) row.label = String(item.label)
        var words = Array.isArray(item.keywords) ? item.keywords : String(item.k || "").split(/\s+/)
        words.forEach(function(word) {
          if (word && row.keywords.indexOf(String(word)) < 0) row.keywords.push(String(word))
        })
        ;(Array.isArray(item.aliases) ? item.aliases : []).forEach(function(alias) {
          if (alias && row.aliases.indexOf(String(alias)) < 0) row.aliases.push(String(alias))
        })
      }
    }
    add(emojiBundleRows)
    add(emojiList)
    add(fallbackEmoji)
    emojiSearchIndex = EmojiSearch.buildIndex(rows)
    updateEmojiSearchResults()
  }

  function closeEmojiPicker() {
    emojiPickerOpen = false
    emojiPickerForReact = false
    var opener = emojiPickerOpener
    emojiPickerOpener = null
    if (opener && opener.forceActiveFocus) opener.forceActiveFocus()
    else if (composer.enabled) composer.forceActiveFocus()
  }

  function openEmojiPicker(opener, forReaction) {
    emojiPickerForReact = forReaction === true
    emojiPickerOpener = opener || null
    var query = EmojiSearch.searchTextForOpen(emojiSearchField.text, keepPreviousEmojiSearchText)
    emojiSearchField.text = query
    emojiSearchQuery = query
    emojiPickerOpen = true
    updateEmojiSearchResults()
  }

  function focusEmojiGrid() {
    if (emojiGrid.count < 1) return
    emojiGrid.currentIndex = EmojiSearch.gridIndexForFocus(emojiGrid.currentIndex, emojiGrid.count)
    emojiGrid.forceActiveFocus()
  }

  function focusConversationList() {
    if (!sidebarVisible) {
      if (messageList) messageList.forceActiveFocus()
      return
    }
    if (!convList || convList.count < 1) return
    var selectedIndex = -1
    for (var i = 0; i < visibleConversations.length; i++) {
      if (visibleConversations[i].id === selectedConvID) { selectedIndex = i; break }
    }
    convList.currentIndex = selectedIndex >= 0 ? selectedIndex : Math.max(0, convList.currentIndex)
    convList.forceActiveFocus()
  }

  function moveConversationCursor(delta) {
    if (!convList || convList.count < 1) return
    var current = convList.currentIndex < 0 ? 0 : convList.currentIndex
    convList.currentIndex = Math.max(0, Math.min(convList.count - 1, current + delta))
    convList.forceActiveFocus()
  }

  function openFocusedConversation() {
    var conversation = visibleConversations[convList.currentIndex]
    if (!conversation) return
    selectConversation(conversation.id)
    if (composer.enabled) composer.forceActiveFocus()
    else if (messageList.count > 0) messageList.forceActiveFocus()
    else convList.forceActiveFocus()
  }

  function leaveKeyboardEditor() {
    if (emojiPickerOpen) {
      closeEmojiPicker()
      return
    }
    if (linkConfirmOpen) {
      cancelOpenUrl()
      if (composer.enabled) composer.forceActiveFocus()
      return
    }
    if (newChatOpen) {
      newChatOpen = false
      focusConversationList()
      return
    }
    if (composerFocus) focusConversationList()
  }

  function handleKeyboardAction(actionId) {
    if (actionId === "search") {
      searchField.forceActiveFocus()
      searchField.selectAll()
    } else if (actionId === "compose") {
      if (composer.enabled) composer.forceActiveFocus()
    } else if (actionId === "history") {
      messageList.forceActiveFocus()
      if (messageList.count > 0) messageList.currentIndex = messageList.count - 1
    } else if (actionId === "nextUnread") {
      if (conversations.length === 0) return
      var start = -1
      for (var i = 0; i < conversations.length; i++) {
        if (conversations[i].id === selectedConvID) { start = i; break }
      }
      for (var step = 1; step <= conversations.length; step++) {
        var index = (start + step + conversations.length) % conversations.length
        if (conversations[index].unread) {
          convList.currentIndex = index
          selectConversation(conversations[index].id)
          focusConversationList()
          return
        }
      }
    } else if (actionId === "newConversation") {
      openNewChat()
    } else if (actionId === "emojiPicker") {
      if (composer.enabled) {
        openEmojiPicker(composer, false)
      }
    } else if (actionId === "attach") {
      if (composer.enabled && !sendingMedia) attachFromDisk()
    }
  }

  function openNewChat() {
    if (!service || newChatLoading) return
    newChatOpen = true
    newChatGroup = false
    newChatError = ""
    newChatTargets = []
    newChatSelected = ({})
    newChatLoading = true
    service.call("conversationTargets", null, function(ok, result) {
      newChatLoading = false
      if (!newChatOpen) return
      if (!ok) { newChatError = String(result); return }
      newChatTargets = result || []
      Qt.callLater(function() { if (root.newChatOpen) newChatSearch.forceActiveFocus() })
    }, network)
  }

  function toggleNewChatTarget(id) {
    var selected = Object.assign({}, newChatSelected)
    if (!newChatGroup) selected = ({})
    if (selected[id]) delete selected[id]
    else selected[id] = true
    newChatSelected = selected
  }

  function createNewChat() {
    if (!service || newChatLoading || newChatSelectionCount < 1) return
    if (newChatGroup && newChatSelectionCount < 2) {
      newChatError = "Select at least two contacts for a group."
      return
    }
    var name = newChatGroup ? newChatName.text.trim() : ""
    if (newChatGroup && name === "") { newChatError = "Enter a group name."; return }
    newChatLoading = true
    newChatError = ""
    service.call("createConversation", { targetIDs: Object.keys(newChatSelected), name: name }, function(ok, result) {
      newChatLoading = false
      if (!ok) { newChatError = String(result); return }
      newChatOpen = false
      if (service.loadConversations) service.loadConversations(network)
      if (result && result.id) selectConversation(String(result.id))
    }, network)
  }

  function selectConversation(id, remember) {
    if (sendingMedia) return
    if (id === selectedConvID) return
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
    historyFetchState = ""
    historyCursors = ({})
    stopPlayback()
    if (recording) stopRecording(false)
    discardPendingCapture()
    emojiPickerOpen = false
    reactingTo = ""
    pendingAttachment = ""
    selectedConvID = id
    var nextSel = Object.assign({}, _selectedByNet)
    nextSel[network] = id
    _selectedByNet = nextSel
    if (remember !== false) conversationSelected(network, id)
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
    hasOlder = (res.hasMore === true || res.canFetchOlder === true || res.historyFetchState === "loading") && id !== ""
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
      historyFetchState = String(res.historyFetchState || "")
      if (!historyExpanded || historyCursorStalled) {
        readHistoryCursor(res, false)
      }
      markThreadRead()
    }, root.network)
  }

  function loadOlderMessages() {
    if (!service || selectedConvID === "" || loadingMessages || loadingOlder || historyFetchState === "loading" || !hasOlder) return
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
      historyFetchState = String(res.historyFetchState || "")
      if (historyFetchState === "loading") {
        hasOlder = true
        historyNotice = "Requesting older history from your phone…"
        return
      }
      if (historyFetchState === "unavailable") {
        hasOlder = false
        historyNotice = String(res.historyNotice || "Your phone did not provide an older history page.")
        return
      }
      if (historyFetchState === "failed") {
        hasOlder = true
        historyError = String(res.historyNotice || "Could not load older messages. Try again.")
        return
      }
      historyNotice = String(res.historyNotice || "")
      displayMessages(Model.mergePage(messages, res.messages || [], true), false)
      historyExpanded = true
      readHistoryCursor(res, true)
    }, root.network)
  }

  function markThreadRead() {
    if (!panelOpen || !service || selectedConvID === "" || !selectedConv || selectedConv.unread !== true) return
    var last = null
    for (var i = messages.length - 1; i >= 0; i--) {
      if (messages[i].id && !messages[i].provisional) { last = messages[i]; break }
    }
    // Telegram retains a dialog watermark even when its top item is a service
    // event that OmaChat does not render. Messenger likewise supports
    // conversation-level acknowledgement without a rendered message ID.
    if (!last && !root.isTelegram && !root.isMessenger) return
    service.call("markRead", { conversationID: selectedConvID, messageID: last ? last.id : "" }, function(ok, error) {
      if (!ok) root.threadError = String(error)
    }, root.network)
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
    var isReaction = root.emojiPickerForReact && root.reactingTo
    var opener = root.emojiPickerOpener
    if (isReaction) root.react(root.reactingTo, emoji)
    else {
      var start = composer.selectionStart >= 0 ? composer.selectionStart : composer.cursorPosition
      var end = composer.selectionEnd >= start ? composer.selectionEnd : start
      if (end > start) composer.remove(start, end)
      composer.insert(start, emoji)
      composer.cursorPosition = start + emoji.length
    }
    root.emojiPickerOpen = false
    root.emojiPickerForReact = false
    root.emojiPickerOpener = null
    if (!isReaction || !opener || !opener.forceActiveFocus) composer.forceActiveFocus()
    else opener.forceActiveFocus()
  }

  onEmojiPickerOpenChanged: if (emojiPickerOpen) Qt.callLater(function() {
    if (root.emojiPickerOpen) emojiSearchField.forceActiveFocus()
  })
  onEmojiSearchQueryChanged: updateEmojiSearchResults()
  onEmojiSearchIndexChanged: updateEmojiSearchResults()

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
  readonly property var statusSG: service && typeof service.statusFor === "function" ? service.statusFor("signal") : (service ? service.statusSG : null)
  onStatusSGChanged: if (statusSG && statusSG.state === "unpaired") root.clearNetwork("signal")
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
    function onHistoryUpdated(update, net) {
      if (net !== root.network || !update || update.conversationID !== root.selectedConvID) return
      root.historyFetchState = String(update.state || "")
      root.historyNotice = String(update.notice || "")
      if (update.state === "complete") {
        root.historyError = ""
        if (root.hasOlder && !root.loadingOlder) Qt.callLater(function() { root.loadOlderMessages() })
      } else if (update.state === "failed") {
        root.hasOlder = true
        root.historyError = root.historyNotice || "Could not load older messages. Try again."
      } else if (update.state === "unavailable") {
        root.hasOlder = false
      }
    }
  }

  function clearNetwork(net) {
    var targetNet = net || root.network
    conversationSelected(targetNet, "")
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
      root.rebuildEmojiSearchIndex()
    }
    onLoadFailed: { root.emojiList = root.fallbackEmoji; root.rebuildEmojiSearchIndex() }
  }

  FileView {
    path: Qt.resolvedUrl("data/emoji-search.json")
    onLoaded: {
      try {
        var parsed = JSON.parse(text())
        if (!Array.isArray(parsed) || parsed.length < 1000) throw new Error("incomplete bundled emoji data")
        root.emojiBundleRows = parsed
        root.emojiSearchDataLoaded = true
      } catch (e) {
        root.emojiBundleRows = []
        root.emojiSearchDataLoaded = false
      }
      root.rebuildEmojiSearchIndex()
    }
    onLoadFailed: {
      root.emojiBundleRows = []
      root.emojiSearchDataLoaded = false
      root.rebuildEmojiSearchIndex()
    }
  }

  Item {
    id: listPane
    objectName: "conversationSidebar"
    anchors.left: parent.left
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    width: visible ? Math.round(parent.width * 0.32) : 0
    visible: !root.detailOnly && root.sidebarVisible

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
      anchors.right: newChatButton.left
      anchors.rightMargin: Style.space(6)
      anchors.top: inboxHeader.bottom
      anchors.topMargin: Style.space(6)
      placeholderText: "Search conversations"
      placeholderTextColor: root.dim
      font.pixelSize: fs(Style.font.body)
      foreground: root.foreground
      onTextChanged: root.searchQuery = text
      onActiveFocusChanged: root.composerFocus = activeFocus
    }

    Button {
      id: newChatButton
      objectName: "newChatButton"
      anchors.right: parent.right
      anchors.verticalCenter: searchField.verticalCenter
      width: Style.space(34)
      height: Style.space(34)
      text: "+"
      enabled: root.service && !root.newChatLoading
      Accessible.name: "New conversation"
      onClicked: root.openNewChat()
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
          searchField.text = ""
          Qt.callLater(function() {
            for (var i = 0; i < root.visibleConversations.length; i++) {
              if (root.visibleConversations[i].unread === true) {
                if (root.visibleConversations[i].id === root.selectedConvID) root.markThreadRead()
                else root.selectConversation(root.visibleConversations[i].id)
                convList.currentIndex = i
                convList.positionViewAtIndex(i, ListView.Contain)
                return
              }
            }
          })
        }
      }

      Text {
        anchors.centerIn: parent
        text: root.unreadConversations === 1 ? "Next unread (1)" : "Next unread (" + root.unreadConversations + ")"
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
      Keys.onReturnPressed: root.openFocusedConversation()
      Keys.onEnterPressed: root.openFocusedConversation()
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
    visible: !root.detailOnly && root.sidebarVisible
    anchors.left: listPane.right
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    anchors.leftMargin: visible ? Style.space(8) : 0
    width: visible ? 1 : 0
    color: Color.popups.border
  }

  Item {
    id: threadPane
    objectName: "threadPane"
    anchors.left: root.detailOnly ? parent.left : paneRule.right
    anchors.leftMargin: !root.detailOnly && root.sidebarVisible ? Style.space(10) : 0
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
        Accessible.name: root.loadingOlder || root.historyFetchState === "loading" ? "Loading older messages..." : (root.historyFetchState === "failed" ? "Retry older messages" : "Load older messages")
        objectName: "loadOlderButton"
        anchors.verticalCenter: parent.verticalCenter
        visible: root.hasOlder || root.loadingOlder
        enabled: !root.loadingOlder && !root.loadingMessages && root.historyFetchState !== "loading"
        text: root.loadingOlder || root.historyFetchState === "loading" ? "Loading older messages..." : (root.historyFetchState === "failed" ? "Retry older messages" : "Load older messages")
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.loadOlderMessages()
      }
      Text {
        id: historyStatus
        objectName: "historyStatus"
        anchors.verticalCenter: parent.verticalCenter
        width: Math.max(0, parent.width - (parent.children[0].visible ? parent.children[0].width + parent.spacing : 0))
        text: root.historyError || root.historyNotice || (root.historyFetchState === "loading" ? "Requesting older history from your phone…" : (!root.hasOlder && !root.loadingMessages ? (root.isWhatsApp ? "No older cached messages are available." : "All available history loaded") : ""))
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
      property real previousHistoryContentY: contentY
      property bool historyAutoBlocked: false
      onContentYChanged: {
        var previous = previousHistoryContentY
        previousHistoryContentY = contentY
        if (!root.isWhatsApp || !moving) return
        if (contentY > height * 0.15) historyAutoBlocked = false
        else if (!historyAutoBlocked && contentY < previous && root.hasOlder && !root.loadingOlder && !root.loadingMessages) {
          historyAutoBlocked = true
          Qt.callLater(function() { root.loadOlderMessages() })
        }
      }
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
              id: moreEmojiReactionButton
              objectName: "moreEmojiReactionButton"
              focusable: true
              Accessible.role: Accessible.Button
              Accessible.name: "+"
              text: "+"
              bordered: true
              tooltipText: "More reactions"
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: {
                root.openEmojiPicker(moreEmojiReactionButton, true)
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
        id: emojiButton
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: "Insert emoji"
        anchors.left: micButton.visible ? micButton.right : attachButton.right
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
          if (root.emojiPickerOpen) root.closeEmojiPicker()
          else root.openEmojiPicker(emojiButton, false)
        }
      }

      TextField {
        id: composer
        objectName: "composer"
        Keys.priority: Keys.BeforeItem
        Keys.onPressed: function(event) {
          if ((event.modifiers & (Qt.ControlModifier | Qt.AltModifier | Qt.MetaModifier)) !== 0
              && root.keyboardActionRouter) root.keyboardActionRouter(event)
        }
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
      id: emojiPicker
      objectName: "emojiPicker"
      visible: root.emojiPickerOpen && root.selectedConvID !== ""
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: composerRow.top
      anchors.bottomMargin: Style.space(6)
      height: visible ? Style.space(312) : 0
      radius: Style.space(8)
      color: Color.popups.background
      border.width: 1
      border.color: Color.popups.border
      clip: true

      TextField {
        id: emojiSearchField
        objectName: "emojiSearchField"
        Accessible.name: "Search emoji by name, keyword, alias, or glyph"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Style.space(8)
        height: Style.space(36)
        placeholderText: "Search emoji"
        placeholderTextColor: root.dim
        foreground: root.foreground
        font.pixelSize: fs(Style.font.body)
        onTextChanged: root.emojiSearchQuery = text
        Keys.onDownPressed: function(event) {
          root.focusEmojiGrid()
          event.accepted = true
        }
        Keys.onTabPressed: function(event) {
          root.focusEmojiGrid()
          event.accepted = true
        }
        Keys.onReturnPressed: function(event) {
          if (emojiGrid.count > 0) root.chooseEmoji(emojiGrid.model[0].e)
          event.accepted = true
        }
        Keys.onEnterPressed: function(event) {
          if (emojiGrid.count > 0) root.chooseEmoji(emojiGrid.model[0].e)
          event.accepted = true
        }
      }

      GridView {
        id: emojiGrid
        objectName: "emojiGrid"
        activeFocusOnTab: true
        keyNavigationEnabled: true
        Keys.onReturnPressed: if (currentIndex >= 0 && currentIndex < count) root.chooseEmoji(model[currentIndex].e)
        Keys.onEnterPressed: if (currentIndex >= 0 && currentIndex < count) root.chooseEmoji(model[currentIndex].e)
        Keys.onEscapePressed: root.closeEmojiPicker()
        Keys.onUpPressed: function(event) {
          var columns = Math.max(1, Math.floor(emojiGrid.width / emojiGrid.cellWidth))
          if (EmojiSearch.gridUpReturnsToSearch(emojiGrid.currentIndex, columns)) {
            emojiSearchField.forceActiveFocus()
            event.accepted = true
          } else {
            event.accepted = false
          }
        }
        highlight: Rectangle { color: "transparent"; border.width: 2; border.color: Color.accent }
        highlightFollowsCurrentItem: true
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: emojiSearchField.bottom
        anchors.bottom: parent.bottom
        anchors.margins: Style.space(8)
        anchors.topMargin: Style.space(4)
        cellWidth: Style.space(52)
        cellHeight: Style.space(52)
        model: root.emojiSearchResults
        clip: true
        delegate: Text {
          required property var modelData
          Accessible.name: (modelData.label || modelData.k || "Emoji") + " " + modelData.e
          Accessible.role: Accessible.ListItem
          width: Style.space(52)
          height: Style.space(52)
          horizontalAlignment: Text.AlignHCenter
          verticalAlignment: Text.AlignVCenter
          text: modelData.e || ""
          ToolTip.visible: hoverHandler.hovered
          ToolTip.text: modelData.label || modelData.k || "Emoji"
          font.pixelSize: fs(Style.space(32))
          renderType: Text.NativeRendering
          HoverHandler { id: hoverHandler }
          MouseArea {
            anchors.fill: parent
            cursorShape: Qt.PointingHandCursor
            onClicked: root.chooseEmoji(parent.text)
          }
        }
      }

      Text {
        id: emojiNoResults
        objectName: "emojiNoResults"
        anchors.centerIn: emojiGrid
        visible: root.emojiSearchQuery.length > 0 && emojiGrid.count === 0
        text: "No emoji found"
        color: root.dim
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
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

  Popup {
    id: newChatOverlay
    objectName: "newChatOverlay"
    anchors.centerIn: Overlay.overlay
    width: Math.min(root.width - Style.space(32), Style.space(480))
    height: Math.min(root.height - Style.space(32), Style.space(520))
    visible: root.newChatOpen
    modal: true
    focus: true
    padding: Style.space(16)
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
    z: 100
    onOpened: Qt.callLater(function() { newChatSearch.forceActiveFocus() })
    onClosed: { root.newChatOpen = false; root.focusConversationList() }
    background: Rectangle {
      radius: Style.space(10)
      color: root.panelBg
      border.width: 1
      border.color: Color.popups.border
    }
    contentItem: Column {
        spacing: Style.space(10)

        Text {
          text: root.newChatGroup ? "New group" : "New conversation"
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.title)
          font.bold: true
        }

        Row {
          spacing: Style.space(8)
          Button { focusable: true; text: "Direct"; enabled: root.newChatGroup; onClicked: { root.newChatGroup = false; root.newChatSelected = ({}) } }
          Button { focusable: true; text: "Group"; enabled: !root.newChatGroup; onClicked: { root.newChatGroup = true; root.newChatSelected = ({}) } }
        }

        TextField {
          id: newChatName
          objectName: "newChatName"
          width: parent.width
          visible: root.newChatGroup
          placeholderText: "Group name"
          foreground: root.foreground
          font.pixelSize: fs(Style.font.body)
        }

        TextField {
          id: newChatSearch
          objectName: "newChatSearch"
          width: parent.width
          placeholderText: "Search contacts"
          foreground: root.foreground
          font.pixelSize: fs(Style.font.body)
          Keys.onDownPressed: {
            if (newChatTargetList.count > 0) {
              newChatTargetList.currentIndex = Math.max(0, newChatTargetList.currentIndex)
              newChatTargetList.forceActiveFocus()
            }
          }
        }

        ListView {
          id: newChatTargetList
          objectName: "newChatTargetList"
          activeFocusOnTab: true
          keyNavigationEnabled: true
          Accessible.role: Accessible.List
          Accessible.name: "Conversation contacts"
          function toggleCurrent() {
            var target = root.visibleNewChatTargets[currentIndex]
            if (target) root.toggleNewChatTarget(String(target.id))
          }
          Keys.onSpacePressed: toggleCurrent()
          Keys.onReturnPressed: { toggleCurrent(); if (!root.newChatGroup) root.createNewChat() }
          Keys.onEnterPressed: { toggleCurrent(); if (!root.newChatGroup) root.createNewChat() }
          width: parent.width
          height: parent.height - y - newChatActions.height - Style.space(34)
          clip: true
          spacing: Style.space(2)
          model: root.visibleNewChatTargets

          delegate: Rectangle {
            required property int index
            required property var modelData
            width: newChatTargetList.width
            height: Style.space(48)
            radius: Style.space(5)
            color: root.newChatSelected[String(modelData.id)] ? root.selectedFill : "transparent"
            border.width: newChatTargetList.activeFocus && newChatTargetList.currentIndex === index ? 2 : 0
            border.color: Color.accent
            Accessible.role: Accessible.ListItem
            Accessible.name: modelData.name
            Text {
              anchors.left: parent.left
              anchors.leftMargin: Style.space(10)
              anchors.right: check.left
              anchors.verticalCenter: parent.verticalCenter
              text: modelData.name + (modelData.detail ? "  ·  " + modelData.detail : "")
              color: root.newChatSelected[String(modelData.id)] ? root.selectedInk : root.foreground
              font.family: root.fontFamily
              font.pixelSize: fs(Style.font.bodySmall)
              elide: Text.ElideRight
            }
            Text {
              id: check
              anchors.right: parent.right
              anchors.rightMargin: Style.space(10)
              anchors.verticalCenter: parent.verticalCenter
              text: root.newChatSelected[String(modelData.id)] ? "✓" : ""
              color: root.selectedInk
              font.pixelSize: fs(Style.font.body)
            }
            MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: root.toggleNewChatTarget(String(modelData.id)) }
          }
        }

        Text {
          width: parent.width
          visible: root.newChatLoading || root.newChatError !== "" || (!root.newChatLoading && root.newChatTargets.length === 0)
          text: root.newChatLoading ? "Loading contacts…" : (root.newChatError || "No contacts available.")
          color: root.newChatError !== "" ? root.errorInk : root.dim
          wrapMode: Text.WordWrap
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.caption)
        }

        Row {
          id: newChatActions
          anchors.right: parent.right
          spacing: Style.space(8)
          Button { focusable: true; text: "Cancel"; onClicked: root.newChatOpen = false }
          Button {
            focusable: true
            objectName: "createConversationButton"
            text: root.newChatGroup ? "Create group" : "Start chat"
            enabled: !root.newChatLoading && root.newChatSelectionCount > 0
            onClicked: root.createNewChat()
          }
        }
      }
  }

  onLinkConfirmOpenChanged: if (linkConfirmOpen) linkDialog.forceActiveFocus()
}
