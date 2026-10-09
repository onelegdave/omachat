import QtQuick
import QtQuick.Window
import QtQuick.Controls as Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model
import "Keybindings.js" as Keybindings

Panel {
  id: root
  moduleName: "onelegdave.omachat"
  ipcTarget: "onelegdave.omachat"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  property var service: null
  readonly property var barIdentity: hostWidget || root

  property string activeService: "gmessages"
  property bool unifiedInboxEnabled: false
  property bool savingUnifiedInboxPreference: false
  property var notificationManager: null
  property string pendingConversationID: ""
  property string pendingConversationNetwork: ""
  property bool sidebarCollapsed: false
  property var lastConversations: ({})
  property bool chatViewLoaded: false
  property int chatViewRevision: 0
  property var pendingChatViewChanges: []
  property bool savingChatView: false
  property string chatViewError: ""
  property bool settingsOpen: false
  property var shortcutOverrides: ({})
  property bool unpairing: false
  property string unpairError: ""
  property int accountGeneration: 0
  property string popoutMode: "floating"
  property bool alwaysPopout: false
  property bool keepPreviousEmojiSearchText: false
  property bool panelConfigLoaded: false
  property bool openPendingConfig: false
  property bool configLoadPending: false
  property string appliedPopoutMode: ""
  property string popoutModeError: ""
  property string popoutAddress: ""
  property int popoutMapAttempts: 0
  property bool surfaceTransfer: false
  property bool restartResumeArmed: false
  property bool restartResumeCancelAfterArm: false
  readonly property int preferredPopoutWidth: Style.space(920)
  readonly property int preferredPopoutHeight: Style.space(580)
  readonly property bool popoutOpen: popoutWindow.visible
  readonly property bool anySurfaceOpen: opened || popoutOpen || surfaceTransfer
  readonly property bool restartResumeNeeded: anySurfaceOpen && service === null
  readonly property bool contentInPopout: keyCatcher.parent === popoutContentHost
  readonly property string restartResumeScript: {
    var url = String(Qt.resolvedUrl("scripts/restart_resume.py"))
    if (url.indexOf("file://") === 0) url = decodeURIComponent(url.substring("file://".length))
    return url
  }
  readonly property var allServiceTabs: [
    { value: "gmessages", label: "Google", icon: "󰭹", tooltip: "Google Messages" },
    { value: "whatsapp", label: "WhatsApp", icon: "󰖣", tooltip: "WhatsApp" },
    { value: "telegram", label: "Telegram", icon: "\uf2c6", tooltip: "Telegram" },
    { value: "messenger", label: "Messenger", icon: "󰈎", tooltip: "Facebook Messenger" },
    { value: "signal", label: "Signal", icon: "󰍡", tooltip: "Signal (unofficial integration)" }
  ]
  property int unreadRevision: 0
  readonly property var providerTabs: {
    var revision = unreadRevision
    return allServiceTabs.filter(function(tab) {
    return !root.service || !Array.isArray(root.service.enabledServices) || root.service.enabledServices.indexOf(tab.value) >= 0
    }).map(function(tab) {
      return {
        value: tab.value,
        label: tab.label,
        icon: tab.icon,
        tooltip: tab.tooltip,
        unread: root.service && typeof root.service.unreadFor === "function" ? root.service.unreadFor(tab.value) : 0
      }
    })
  }

  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onStatusChanged() { root.unreadRevision++ }
    function onStatusWAChanged() { root.unreadRevision++ }
    function onStatusTGChanged() { root.unreadRevision++ }
    function onStatusFBChanged() { root.unreadRevision++ }
    function onConnectedChanged() {
      if (root.service && root.service.connected) {
        root.loadConfig()
        root.flushChatViewChanges()
      }
    }
    function onStatusSGChanged() { root.unreadRevision++ }
    function onConversationsChanged() { root.unreadRevision++; root.selectPendingConversation() }
    function onConversationsWAChanged() { root.unreadRevision++; root.selectPendingConversation() }
    function onConversationsTGChanged() { root.unreadRevision++; root.selectPendingConversation() }
    function onConversationsFBChanged() { root.unreadRevision++; root.selectPendingConversation() }
    function onConversationsSGChanged() { root.unreadRevision++; root.selectPendingConversation() }
    function onPaired(network) { root.rememberConversation(network, "") }
  }
  readonly property var serviceTabs: unifiedInboxEnabled
    ? [{value:"all", label:"All", icon:"󰭹", tooltip:"All services", unread:0}].concat(providerTabs)
    : providerTabs
  readonly property var unifiedConversations: Keybindings.buildUnifiedConversations(
    providerTabs.map(function(tab) {
      return {network:tab.value, conversations:root.service && root.service.conversationsFor
        ? root.service.conversationsFor(tab.value) : []}
    }))
  readonly property bool unifiedActive: activeService === "all"
  readonly property var activeInboxItem: unifiedActive ? unifiedInboxLoader.item : inboxLoader.item
  readonly property bool noServices: providerTabs.length === 0
  onServiceTabsChanged: syncActiveService()
  function syncActiveService() {
    if (serviceTabs.some(function(tab) { return tab.value === root.activeService })) return
    activeService = serviceTabs.length ? serviceTabs[0].value : ""
    if(service) service.currentNetwork=activeService
  }
  property real uiScale: 1
  function fs(n) { return Math.max(8, Math.round(Number(n) * uiScale)) }
  readonly property bool serviceLive: activeService === "gmessages" || activeService === "whatsapp" || activeService === "messenger" || activeService === "signal"
  readonly property bool telegramLive: activeService === "telegram"

  readonly property var chat: service
  readonly property int unread: service && !unifiedActive ? (typeof service.unreadFor === "function" ? service.unreadFor(activeService) : (service.unread || 0)) : 0
  readonly property string connState: service && !unifiedActive ? (typeof service.stateFor === "function" ? service.stateFor(activeService) : (service.state || "")) : ""
  readonly property var activeStatus: service && !unifiedActive ? (typeof service.statusFor === "function" ? service.statusFor(activeService) : service.status) : null

  function readableInk(surface, preferred, minRatio) {
    return Model.readableInk(surface, preferred, minRatio)
  }

  readonly property color popupBg: Color.popups.background
  readonly property color foreground: readableInk(popupBg, Color.popups.text, 7)
  readonly property color mutedInk: readableInk(popupBg, Color.muted, 7)
  readonly property color accentInk: readableInk(popupBg, Color.accent, 4.5)
  readonly property color urgentInk: readableInk(popupBg, Color.urgent, 4.5)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  readonly property bool needsPair: connState === "unpaired" || connState === "pairing"
    || connState === "gaiaPairing" || connState === "error"
  readonly property bool linkUp: service && service.connected && connState === "connected"
  readonly property string linkLabel: {
    if (!service) return "SERVICE OFFLINE"
    if (!service.connected) return "HELPER OFFLINE"
    if (noServices) return service.servicesConfigLoaded === false ? "LOADING" : "SERVICES OFF"
    if (connState === "connected" && activeStatus && activeStatus.phoneOK === false)
      return "PHONE OFFLINE"
    if (connState === "connected") return "CONNECTED"
    if (connState === "connecting" || connState === "pairing" || connState === "gaiaPairing")
      return "CONNECTING"
    if (connState === "unpaired") return "NOT PAIRED"
    return Model.statusLine(activeStatus).toUpperCase()
  }

  function refresh() {
    if (noServices) return
    if (service && service.refreshConversations) {
      if (unifiedActive) providerTabs.forEach(function(tab) { service.refreshConversations(tab.value) })
      else service.refreshConversations(activeService)
    }
    if (activeInboxItem && activeInboxItem.refreshThread) activeInboxItem.refreshThread()
  }

  function cancelRestartResume() {
    restartResumeCancelAfterArm = false
    restartResumeArmed = false
    restartResumeProcess.operation = "cancel"
    restartResumeProcess.command = [
      "python3", restartResumeScript, "cancel",
      "--old-pid", String(Quickshell.processId), "--reason", "missing-service"
    ]
    restartResumeProcess.running = true
  }

  function syncRestartResume() {
    if (restartResumeNeeded) {
      if (restartResumeArmed || restartResumeProcess.running) return
      restartResumeProcess.operation = "arm"
      restartResumeProcess.command = [
        "python3", restartResumeScript, "arm",
        "--old-pid", String(Quickshell.processId), "--reason", "missing-service"
      ]
      restartResumeProcess.running = true
      return
    }
    if (restartResumeProcess.running && restartResumeProcess.operation === "arm") {
      restartResumeCancelAfterArm = true
      return
    }
    if (restartResumeArmed && !restartResumeProcess.running) cancelRestartResume()
  }

  function open() {
    if (popoutOpen) return
    if (!panelConfigLoaded) {
      openPendingConfig = true
      if (service) loadConfig(true)
      return
    }
    openPendingConfig = false
    if (alwaysPopout) openPopout()
    else root.controller.show()
  }

  function close() {
    if (openPendingConfig) {
      openPendingConfig = false
      return
    }
    if (popoutPrepareProcess.running) {
      popoutPrepareProcess.openRequested = false
      popoutPrepareProcess.running = false
      surfaceTransfer = false
      return
    }
    if (popoutOpen) {
      closePopout(false)
      return
    }
    root.controller.hide()
  }

  function toggle() {
    if (popoutPrepareProcess.running) root.close()
    else if (popoutOpen) closePopout(false)
    else if (opened) root.controller.hide()
    else root.open()
  }

  function openPopout() {
    if (popoutOpen || popoutPrepareProcess.running) return
    surfaceTransfer = true
    root.controller.hide()
    popoutModeError = ""
    appliedPopoutMode = ""
    popoutAddress = ""
    popoutPrepareProcess.openRequested = true
    popoutPrepareProcess.command = [
      "hyprctl", "eval",
      "if omachat_direct_open_rule == nil then "
        + "omachat_direct_open_rule = hl.window_rule({ name = \"omachat-direct-open\", "
        + "match = { initial_title = \"^OmaChat Pop-out$\" }, float = true, "
        + "size = { " + root.preferredPopoutWidth + ", " + root.preferredPopoutHeight + " } }) end; "
        + "omachat_direct_open_rule:set_enabled(" + (root.popoutMode === "floating" ? "true" : "false") + ")"
    ]
    popoutPrepareProcess.running = true
  }

  function showPreparedPopout() {
    popoutWindow.visible = true
    requestPopoutMode(popoutMode)
    Qt.callLater(function() {
      root.surfaceTransfer = false
      if (root.popoutOpen) root.focusInitialControl()
    })
  }

  function closePopout(reopenPanel) {
    popoutWindow.reopenPanelAfterClose = reopenPanel === true
    popoutWindow.visible = false
  }

  function returnToPanel() {
    surfaceTransfer = true
    popoutWindow.reopenPanelAfterClose = false
    popoutWindow.visible = false
    root.controller.show()
    Qt.callLater(function() {
      root.surfaceTransfer = false
      if (root.opened) root.focusInitialControl()
    })
  }

  function setPopoutMode(mode) {
    if (mode !== "tiled" && mode !== "floating") return
    if (popoutOpen && popoutMode === mode && appliedPopoutMode === mode) return
    popoutMode = mode
    if (popoutOpen) requestPopoutMode(mode)
  }

  function requestPopoutMode(mode) {
    if (mode !== "tiled" && mode !== "floating") return
    popoutModeError = ""
    appliedPopoutMode = ""
    popoutMapAttempts = 0
    modeMapTimer.requestedMode = mode
    modeMapTimer.restart()
  }

  function dispatchPopoutMode(mode, address) {
    var safeAddress = String(address || "")
    if (!/^0x[0-9a-f]+$/i.test(safeAddress)) {
      popoutModeError = "Could not identify the OmaChat window. Try the layout choice again."
      return
    }
    if (popoutModeProcess.running) return
    popoutModeProcess.requestedMode = mode
    popoutModeProcess.command = [
      "hyprctl", "dispatch",
      "hl.dsp.window.float({ action = \"" + (mode === "floating" ? "on" : "off")
        + "\", window = \"address:" + safeAddress + "\" })"
    ]
    popoutModeProcess.running = true
  }

  function setActiveService(v, preserveSettings) {
    if (!serviceTabs.some(function(tab) { return tab.value === v })) return
    var wasUnified = unifiedActive
    if (preserveSettings !== true) root.settingsOpen = false
    root.activeService = v
    if (v !== "all") saveChatView({lastService: v}, v)
    if (root.service) {
      if (v !== "all") root.service.currentNetwork = v
      if (v === "all") root.providerTabs.forEach(function(tab) { root.service.loadConversations(tab.value) })
      else root.service.loadConversations(v)
    }
    if (preserveSettings !== true && (wasUnified || v === "all"))
      Qt.callLater(root.focusInitialControl)
  }

  function setUnifiedInboxEnabled(enabled) {
    if (!service || savingUnifiedInboxPreference) return
    savingUnifiedInboxPreference = true
    service.call("setUnifiedInboxPreference", {enabled: enabled === true}, function(ok, res) {
      root.savingUnifiedInboxPreference = false
      if (!ok || !res) return
      root.unifiedInboxEnabled = res.unifiedInboxEnabled === true
      if (root.unifiedInboxEnabled && !root.unifiedActive) root.setActiveService("all")
      else if (!root.unifiedInboxEnabled && root.unifiedActive)
        root.setActiveService(root.providerTabs.length ? root.providerTabs[0].value : "")
    }, "gmessages")
  }


  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed
    saveChatView({sidebarCollapsed: sidebarCollapsed}, activeService)
    focusInitialControl()
  }

  function rememberConversation(network, id) {
    if ((lastConversations[network] || "") === id) return
    var next = Object.assign({}, lastConversations)
    if (id === "") delete next[network]
    else next[network] = id
    lastConversations = next
    saveChatView({conversationID: id}, network)
  }

  function saveChatView(params, network) {
    chatViewRevision++
    pendingChatViewChanges = pendingChatViewChanges.concat([{params: params, network: network}])
    flushChatViewChanges()
  }

  function flushChatViewChanges() {
    if (savingChatView || !service || !service.connected || !pendingChatViewChanges.length) return
    var change = pendingChatViewChanges[0]
    pendingChatViewChanges = pendingChatViewChanges.slice(1)
    savingChatView = true
    service.call("setChatView", change.params, function(ok) {
      root.savingChatView = false
      root.chatViewError = ok ? "" : "Could not remember the chat view. Rebuild the helper if it needs updating, then try again."
      root.flushChatViewChanges()
    }, change.network)
  }

  function isReadingConversation(network, id) {
    var inbox = unifiedActive && unifiedInboxLoader.item
      ? unifiedInboxLoader.item.detailView : inboxLoader.item
    var inboxVisible = unifiedActive ? unifiedInboxLoader.visible : inboxLoader.visible
    return anySurfaceOpen && inboxVisible && inbox
      && inbox.network === network && inbox.selectedConvID === id
      && keyCatcher.Window.window && keyCatcher.Window.window.active
  }

  function showConversation(network, id) {
    if (!serviceTabs.some(function(tab) { return tab.value === network })) return
    setActiveService(network)
    pendingConversationNetwork = network
    pendingConversationID = id
    open()
    if (popoutOpen && popoutContentHost.Window.window) popoutContentHost.Window.window.requestActivate()
    Qt.callLater(selectPendingConversation)
  }

  function selectPendingConversation() {
    if (!pendingConversationID || activeService !== pendingConversationNetwork || !inboxLoader.visible || !inboxLoader.item) return
    if (!inboxLoader.item.conversations.some(function(conv) { return conv.id === root.pendingConversationID })) return
    inboxLoader.item.selectConversation(pendingConversationID)
    if (inboxLoader.item.selectedConvID === pendingConversationID) pendingConversationID = ""
  }

  function showNotificationSettings() {
    settingsOpen = true
    open()
    Qt.callLater(function() { if (bodyLoader.item) bodyLoader.item.showNotifications() })
  }

  function registerNotificationView() {
    if (notificationManager) notificationManager.unregisterView(root)
    notificationManager = service && service.notifications ? service.notifications : null
    if (notificationManager) notificationManager.registerView(root)
  }

  function switchService(direction) {
    if (root.serviceTabs.length < 2) return
    var current = root.serviceTabs.findIndex(function(tab) { return tab.value === root.activeService })
    if (current < 0) current = 0
    var next = (current + direction + root.serviceTabs.length) % root.serviceTabs.length
    root.setActiveService(root.serviceTabs[next].value)
  }

  function activateKeyboardAction(actionId) {
    if (actionId.indexOf("service.") === 0) {
      root.setActiveService(actionId.substring("service.".length))
      return
    }
    if (actionId === "help") { actionMenu.open(); return }
    if (actionId === "settings") { root.settingsOpen = !root.settingsOpen; Qt.callLater(root.focusInitialControl); return }
    if (actionId === "refresh") { root.refresh(); return }
    if (actionId === "toggleUnifiedInbox") { root.setUnifiedInboxEnabled(!root.unifiedInboxEnabled); return }
    if (root.settingsOpen || !root.activeInboxItem) return
    if (typeof root.activeInboxItem.handleKeyboardAction === "function")
      root.activeInboxItem.handleKeyboardAction(actionId)
  }

  function focusInitialControl() {
    if (settingsOpen && bodyLoader.item && bodyLoader.item.focusInitialControl) {
      bodyLoader.item.focusInitialControl()
      return
    }
    if (activeInboxItem && (inboxLoader.visible || unifiedInboxLoader.visible) && activeInboxItem.focusConversationList) {
      activeInboxItem.focusConversationList()
      return
    }
    keyCatcher.forceActiveFocus()
  }

  function routeKeyboardEvent(event) {
    var inbox = (inboxLoader.visible || unifiedInboxLoader.visible) ? activeInboxItem : null
    var editing = !!(inbox && inbox.isKeyboardEditing && inbox.isKeyboardEditing())
    var chord = {
      text: event.text,
      key: event.key,
      ctrl: (event.modifiers & Qt.ControlModifier) !== 0,
      alt: (event.modifiers & Qt.AltModifier) !== 0,
      meta: (event.modifiers & Qt.MetaModifier) !== 0,
      shift: (event.modifiers & Qt.ShiftModifier) !== 0,
      isComposing: event.isComposing === true
    }
    if (event.key === Qt.Key_Escape) {
      if (actionMenu.visible) actionMenu.close()
      else if (root.settingsOpen) {
        var settingsView = bodyLoader.item
        if (settingsView && settingsView.isKeyboardEditing && settingsView.isKeyboardEditing())
          settingsView.leaveKeyboardEditor()
        else root.settingsOpen = false
      }
      else if (editing && inbox && inbox.leaveKeyboardEditor) inbox.leaveKeyboardEditor()
      else root.close()
      event.accepted = true
      return
    }
    if (root.settingsOpen && bodyLoader.item && bodyLoader.item.isCapturingShortcut
        && bodyLoader.item.isCapturingShortcut()) return
    if (inbox && inbox.hasKeyboardModal && inbox.hasKeyboardModal()) return
    if ((event.modifiers & Qt.ControlModifier) !== 0
        && (event.modifiers & (Qt.AltModifier | Qt.MetaModifier | Qt.ShiftModifier)) === 0
        && event.key >= Qt.Key_1 && event.key <= Qt.Key_9) {
      var tabIndex = event.key - Qt.Key_1
      if (tabIndex < root.serviceTabs.length)
        root.setActiveService(root.serviceTabs[tabIndex].value)
      event.accepted = true
      return
    }
    if ((event.modifiers & Qt.ControlModifier) !== 0
        && (event.modifiers & (Qt.AltModifier | Qt.MetaModifier | Qt.ShiftModifier)) === 0
        && event.key === Qt.Key_Tab) {
      root.switchService(1)
      event.accepted = true
      return
    }
    var result = Keybindings.resolve(chord, editing ? "editing" : "navigation", {
      overrides: root.shortcutOverrides,
      enabledServices: root.serviceTabs.map(function(tab) { return tab.value })
    })

    if (result.id) {
      root.activateKeyboardAction(result.id)
      event.accepted = true
      return
    }
    if (!editing && !chord.isComposing && !chord.ctrl && !chord.alt && !chord.meta && !chord.shift
        && (String(chord.text || "").toLowerCase() === "j" || String(chord.text || "").toLowerCase() === "k")
        && inbox && inbox.moveConversationCursor) {
      inbox.moveConversationCursor(String(chord.text).toLowerCase() === "j" ? 1 : -1)
      event.accepted = true
    }
  }

  function loadConfig(openAfter) {
    if (!service || configLoadPending) {
      panelConfigLoaded = false
      return
    }
    var target = service
    var chatViewRevisionAtRequest = chatViewRevision
    configLoadPending = true
    service.call("config", null, function(ok, res) {
      if (root.service !== target) return
      root.configLoadPending = false
      if (!ok || !res) {
        root.panelConfigLoaded = false
        if (root.openPendingConfig) configRetryTimer.restart()
        return
      }
      if (typeof target.applyServiceConfig === "function" && !target.savingServices) target.applyServiceConfig(res)
      var s = Number(res.uiScale)
      if (isFinite(s) && s > 0) root.uiScale = s
      var validated = Keybindings.validateOverrides(res.keyboardShortcuts || ({}))
      root.shortcutOverrides = validated.ok ? validated.overrides : ({})
      root.alwaysPopout = res.alwaysPopout === true
      root.keepPreviousEmojiSearchText = res.keepPreviousEmojiSearchText === true
      root.popoutMode = res.popoutMode === "tiled" ? "tiled" : "floating"
      root.unifiedInboxEnabled = res.unifiedInboxEnabled === true
      if (!root.chatViewLoaded) {
        root.chatViewLoaded = true
        root.lastConversations = Object.assign({}, res.lastConversations || {}, root.lastConversations)
        if (chatViewRevisionAtRequest === root.chatViewRevision) {
          root.sidebarCollapsed = res.sidebarCollapsed === true
          if (root.serviceTabs.some(function(tab) { return tab.value === res.lastService })) {
            root.activeService = res.lastService
            target.currentNetwork = res.lastService
            target.loadConversations(res.lastService)
          }
        }
      }
      root.panelConfigLoaded = true
      if (openAfter || root.openPendingConfig) root.open()
    }, "gmessages")
  }

  Timer {
    id: configRetryTimer
    interval: 100
    repeat: false
    onTriggered: {
      if (root.openPendingConfig && !root.panelConfigLoaded) root.loadConfig(true)
    }
  }

  onAnySurfaceOpenChanged: {
    syncRestartResume()
    if (!anySurfaceOpen || !service) return
    loadConfig()
    if (needsPair && activeService === "gmessages") service.loadProfiles()
    if (!noServices) service.loadConversations(activeService)
    Qt.callLater(root.focusInitialControl)
  }

  onSettingsOpenChanged: Qt.callLater(root.focusInitialControl)

  onServiceChanged: {
    registerNotificationView()
    syncRestartResume()
    syncActiveService()
    accountGeneration++
    unpairing = false
    unpairError = ""
    panelConfigLoaded = false
    configLoadPending = false
    loadConfig(openPendingConfig)
  }

  Component.onCompleted: { syncRestartResume(); registerNotificationView() }
  Component.onDestruction: if (notificationManager) notificationManager.unregisterView(root)

  onConnStateChanged: {
    if (connState === "pairing" || connState === "gaiaPairing" || connState === "connecting") {
      accountGeneration++
      unpairing = false
      unpairError = ""
    }
    if (connState === "unpaired" && inboxLoader.item && inboxLoader.item.clearNetwork) {
      inboxLoader.item.clearNetwork(activeService)
    }
  }

  states: State {
    name: "popout"
    when: root.popoutOpen
    ParentChange {
      target: keyCatcher
      parent: popoutContentHost
    }
  }

  FloatingWindow {
    id: popoutWindow
    objectName: "omachatPopoutWindow"
    property bool reopenPanelAfterClose: false
    title: "OmaChat Pop-out"
    color: root.popupBg
    implicitWidth: root.preferredPopoutWidth
    implicitHeight: root.preferredPopoutHeight
    minimumSize: Qt.size(Style.space(640), Style.space(420))
    visible: false

    onVisibleChanged: {
      if (visible) return
      modeMapTimer.stop()
      if (popoutResolveProcess.running) popoutResolveProcess.running = false
      if (popoutModeProcess.running) popoutModeProcess.running = false
      if (popoutResizeProcess.running) popoutResizeProcess.running = false
      root.popoutAddress = ""
      root.appliedPopoutMode = ""
      if (reopenPanelAfterClose) {
        reopenPanelAfterClose = false
        Qt.callLater(function() { root.open() })
      }
    }

    Item {
      id: popoutContentHost
      anchors.fill: parent
      anchors.margins: Style.spacing.popupPadding
    }
  }

  Timer {
    id: modeMapTimer
    property string requestedMode: "floating"
    interval: 75
    repeat: false
    onTriggered: {
      if (!root.popoutOpen || popoutResolveProcess.running) return
      popoutResolveProcess.command = ["hyprctl", "-j", "clients"]
      popoutResolveProcess.running = true
    }
  }

  Process {
    id: popoutPrepareProcess
    objectName: "popoutPrepareProcess"
    property bool openRequested: false
    onExited: function(code) {
      if (!openRequested) return
      openRequested = false
      // Mapping still proceeds if Hyprland rejects the preparatory rule; the
      // address-targeted correction below remains the compatibility fallback.
      root.showPreparedPopout()
    }
  }

  Process {
    id: restartResumeProcess
    objectName: "restartResumeProcess"
    property string operation: ""
    onExited: function(code) {
      var completed = operation
      operation = ""
      if (completed === "arm") {
        root.restartResumeArmed = code === 0
        if (root.restartResumeCancelAfterArm || !root.restartResumeNeeded) {
          root.restartResumeCancelAfterArm = false
          if (root.restartResumeArmed) Qt.callLater(root.cancelRestartResume)
        }
      } else if (completed === "cancel") {
        root.restartResumeArmed = false
      }
    }
  }

  Process {
    id: popoutResolveProcess
    stdout: StdioCollector { id: popoutResolveStdout; waitForEnd: true }
    stderr: StdioCollector { id: popoutResolveStderr; waitForEnd: true }
    onExited: function(code) {
      if (!root.popoutOpen) return
      var address = ""
      if (code === 0) {
        try {
          var clients = JSON.parse(String(popoutResolveStdout.text || "[]"))
          var matchedClient = null
          for (var i = 0; i < clients.length; i++) {
            var client = clients[i]
            if (client && client.mapped !== false
                && String(client.title) === popoutWindow.title
                && String(client.initialTitle) === popoutWindow.title) {
              address = String(client.address || "")
              matchedClient = client
              break
            }
          }
        } catch (error) {
          address = ""
        }
      }
      if (/^0x[0-9a-f]+$/i.test(address)) {
        root.popoutAddress = address
        var requestedMode = modeMapTimer.requestedMode
        var alreadyFloating = matchedClient && matchedClient.floating === true
        if ((requestedMode === "floating") === alreadyFloating) {
          if (requestedMode === "floating"
              && matchedClient.size
              && (matchedClient.size[0] !== root.preferredPopoutWidth
                || matchedClient.size[1] !== root.preferredPopoutHeight)) {
            popoutResizeProcess.command = [
              "hyprctl", "dispatch",
              "hl.dsp.window.resize({ x = " + root.preferredPopoutWidth
                + ", y = " + root.preferredPopoutHeight
                + ", relative = false, window = \"address:" + root.popoutAddress + "\" })"
            ]
            popoutResizeProcess.running = true
          } else {
            root.appliedPopoutMode = requestedMode
            root.popoutModeError = ""
          }
          return
        }
        root.dispatchPopoutMode(modeMapTimer.requestedMode, address)
        return
      }
      root.popoutMapAttempts++
      if (root.popoutMapAttempts >= 40) {
        var detail = String(popoutResolveStderr.text || "").trim()
        root.popoutModeError = detail !== ""
          ? "OmaChat could not identify its pop-out window: " + detail
          : "OmaChat could not apply the window layout. Choose Tiled or Floating to retry."
        return
      }
      modeMapTimer.restart()
    }
  }

  Process {
    id: popoutModeProcess
    objectName: "popoutModeProcess"
    property string requestedMode: ""
    stdout: StdioCollector { id: popoutModeStdout; waitForEnd: true }
    stderr: StdioCollector { id: popoutModeStderr; waitForEnd: true }
    onExited: function(code) {
      if (!root.popoutOpen) return
      if (code === 0) {
        if (requestedMode === "floating") {
          popoutResizeProcess.command = [
            "hyprctl", "dispatch",
            "hl.dsp.window.resize({ x = " + root.preferredPopoutWidth
              + ", y = " + root.preferredPopoutHeight
              + ", relative = false, window = \"address:" + root.popoutAddress + "\" })"
          ]
          popoutResizeProcess.running = true
          return
        }
        root.appliedPopoutMode = requestedMode
        root.popoutModeError = ""
      } else {
        var detail = String(popoutModeStderr.text || popoutModeStdout.text || "").trim()
        root.popoutModeError = detail !== ""
          ? "Could not apply the window layout: " + detail
          : "Could not apply the window layout. Choose Tiled or Floating to retry."
      }
    }
  }

  Process {
    id: popoutResizeProcess
    stdout: StdioCollector { id: popoutResizeStdout; waitForEnd: true }
    stderr: StdioCollector { id: popoutResizeStderr; waitForEnd: true }
    onExited: function(code) {
      if (!root.popoutOpen) return
      if (code === 0) {
        root.appliedPopoutMode = "floating"
        root.popoutModeError = ""
      } else {
        var detail = String(popoutResizeStderr.text || popoutResizeStdout.text || "").trim()
        root.popoutModeError = detail !== ""
          ? "OmaChat floated, but could not restore its pop-out size: " + detail
          : "OmaChat floated, but could not restore its pop-out size."
      }
    }
  }

  function unpair() {
    if (!service || unpairing) return
    settingsOpen = false
    var currentNet = activeService
    var target = service
    var generation = accountGeneration
    unpairing = true
    unpairError = ""
    target.call("unpair", null, function(ok, res) {
      if (root.service !== target || root.accountGeneration !== generation) return
      root.unpairing = false
      if (ok) root.rememberConversation(currentNet, "")
      if (!ok) {
        var advice = currentNet === "whatsapp"
          ? " Check WhatsApp on your phone under Linked devices."
          : (currentNet === "telegram"
            ? " Check Telegram on your phone or desktop."
            : (currentNet === "messenger"
              ? " Check Facebook or Messenger for active sessions."
              : (currentNet === "signal"
                ? " Remove OmaChat from Signal on your phone under Linked devices."
                : " Check Google Messages on your phone under Device pairing.")))
        root.unpairError = "Unpair did not complete successfully: " + String(res) + advice
      }
    }, currentNet)
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    centerOnBar: true
    focusTarget: root.activeInboxItem && (inboxLoader.visible || unifiedInboxLoader.visible) ? root.activeInboxItem.keyboardInitialFocus : keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(920))
    contentHeight: panel.cappedContentHeight(Style.space(580))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      objectName: "chatKeyCatcher"
      // Let Tab, arrows, Enter and Space reach the focused shared control.
      // The stock catcher consumes them for panels with a custom cursor model.
      Keys.onPressed: function(event) { root.routeKeyboardEvent(event) }

      ActionMenu {
        id: actionMenu
        enabledServices: root.serviceTabs.map(function(tab) { return tab.value })
        overrides: root.shortcutOverrides
        foreground: root.foreground
        mutedColor: root.mutedInk
        accentColor: root.accentInk
        surfaceColor: root.popupBg
        fontFamily: root.fontFamily
        fontSize: root.fs(Style.font.body)
        onTriggered: function(actionId) { root.activateKeyboardAction(actionId) }
      }

      Column {
        id: headerCol
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        spacing: Style.space(8)

        Item {
          width: parent.width
          height: Style.space(28)

          PanelActionButton {
            id: sidebarBtn
            objectName: "sidebarToggleButton"
            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            visible: inboxLoader.visible || unifiedInboxLoader.visible
            enabled: root.unifiedActive
              ? unifiedInboxLoader.item && unifiedInboxLoader.item.detailConversationID !== ""
              : inboxLoader.item && inboxLoader.item.selectedConvID !== ""
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: tooltipText
            iconText: root.sidebarCollapsed ? "󰍜" : "󰁍"
            tooltipText: root.sidebarCollapsed ? "Show conversations" : "Hide conversations"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.toggleSidebar()
          }

          OpticalGlyph {
            id: brandGlyph
            anchors.left: sidebarBtn.visible ? sidebarBtn.right : parent.left
            anchors.leftMargin: sidebarBtn.visible ? Style.space(8) : 0
            anchors.verticalCenter: parent.verticalCenter
            width: Style.space(22)
            height: Style.space(22)
            text: root.activeService === "whatsapp" ? "󰖣" : (root.activeService === "telegram" ? "\uf2c6" : (root.activeService === "messenger" ? "󰈎" : (root.activeService === "signal" ? "󰍡" : "󰭹")))
            color: root.accentInk
            fontFamily: root.fontFamily
            fontSize: root.fs(Style.font.heading)
          }

          Text {
            anchors.left: brandGlyph.right
            anchors.leftMargin: Style.space(8)
            anchors.verticalCenter: parent.verticalCenter
            text: "OmaChat"
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.heading)
            font.bold: true
          }

          PanelActionButton {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: tooltipText
            id: refreshBtn
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            iconText: "󰑐"
            tooltipText: "Refresh"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.refresh()
          }

          PanelActionButton {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: tooltipText
            id: settingsBtn
            anchors.right: refreshBtn.left
            anchors.rightMargin: Style.space(2)
            anchors.verticalCenter: parent.verticalCenter
            iconText: root.settingsOpen ? "󰁍" : "󰒓"
            tooltipText: root.settingsOpen ? "Back to chats" : "Settings"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.settingsOpen = !root.settingsOpen
          }

          Button {
            id: popoutBtn
            objectName: "popoutButton"
            visible: !root.alwaysPopout
            anchors.right: settingsBtn.left
            anchors.rightMargin: Style.space(2)
            anchors.verticalCenter: parent.verticalCenter
            text: root.popoutOpen ? "Return to panel" : "Pop out"
            iconText: root.popoutOpen ? "󰖲" : "󰏌"
            tooltipText: root.popoutOpen ? "Return to panel" : "Open in window"
            foreground: root.foreground
            fontFamily: root.fontFamily
            fontSize: root.fs(Style.font.bodySmall)
            iconSize: root.fs(Style.font.iconSmall)
            horizontalPadding: Style.space(7)
            verticalPadding: Style.space(4)
            bordered: true
            focusable: true
            onClicked: root.popoutOpen ? root.returnToPanel() : root.openPopout()
          }

          PanelActionButton {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: tooltipText
            id: unpairBtn
            objectName: "unpairButton"
            enabled: !root.unpairing
            visible: root.linkUp
            anchors.right: root.alwaysPopout ? settingsBtn.left : popoutBtn.left
            anchors.rightMargin: Style.space(2)
            anchors.verticalCenter: parent.verticalCenter
            iconText: "󰍃"
            tooltipText: "Unpair this desktop"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.unpair()
          }

          Rectangle {
            id: linkChip
            anchors.right: unpairBtn.visible ? unpairBtn.left : (root.alwaysPopout ? settingsBtn.left : popoutBtn.left)
            anchors.rightMargin: Style.space(8)
            anchors.verticalCenter: parent.verticalCenter
            height: Style.space(20)
            width: chipRow.implicitWidth + Style.space(12)
            radius: height / 2
            color: Style.normalFillFor(root.foreground, root.accentInk)
            border.width: 1
            border.color: root.linkUp ? root.accentInk : root.urgentInk

            Row {
              id: chipRow
              anchors.centerIn: parent
              spacing: Style.space(6)

              Rectangle {
                width: Style.space(6)
                height: Style.space(6)
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: root.linkUp ? root.accentInk : root.urgentInk
              }

              Text {
                anchors.verticalCenter: parent.verticalCenter
                text: root.linkLabel
                color: root.linkUp ? root.accentInk : root.urgentInk
                font.family: root.fontFamily
                font.pixelSize: root.fs(Style.font.caption)
                font.bold: true
              }
            }
          }
        }

        Text {
          objectName: "chatViewErrorLabel"
          width: parent.width
          visible: root.chatViewError !== ""
          text: root.chatViewError
          color: root.urgentInk
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.bodySmall)
          wrapMode: Text.Wrap
        }

        Text {
          objectName: "unpairErrorLabel"
          width: parent.width
          visible: root.unpairError !== "" && !(root.needsPair && !root.settingsOpen && root.serviceLive && root.service && root.service.status && root.service.status.error)
          text: root.unpairError
          textFormat: Text.PlainText
          color: root.urgentInk
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.bodySmall)
          wrapMode: Text.Wrap
        }

        Flow {
          width:parent.width
          spacing:Style.space(8)
          visible:!!root.service && (!!root.service.helperNeedsRebuild || (!!root.service.updates && root.service.updates.noticeVisible))
          Button {
            objectName:"updateNoticeButton"
            text:root.service && root.service.helperNeedsRebuild ? "Rebuild helper to finish update" : "Update available"
            focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
            Accessible.role:Accessible.Button
            Accessible.name:text
            fontSize:root.fs(Style.font.bodySmall)
            onClicked:{
              root.settingsOpen=true
              Qt.callLater(function(){ if(bodyLoader.item && typeof bodyLoader.item.showUpdates === "function") bodyLoader.item.showUpdates() })
            }
          }
          Button {
            text:"Dismiss"
            Accessible.role:Accessible.Button
            Accessible.name:"Dismiss update notice"
            fontSize:root.fs(Style.font.bodySmall)
            visible:!!root.service && !root.service.helperNeedsRebuild && !!root.service.updates && root.service.updates.noticeVisible
            enabled:visible && !root.service.updates.busy
            focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
            onClicked:root.service.updates.run("dismiss")
          }
        }

        ChoiceGroup {
          objectName: "serviceTabs"
          width: parent.width
          options: root.serviceTabs
          value: root.activeService
          foreground: root.foreground
          background: Color.popups.background
          accent: root.accentInk
          fontFamily: root.fontFamily
          fontSize: root.fs(Style.font.body)
          focusable: true
          onChanged: function(v) { root.setActiveService(v) }
        }

        Row {
          visible: root.popoutOpen
          width: parent.width
          height: visible ? Math.max(windowLayoutLabel.implicitHeight, windowLayoutChoices.implicitHeight) : 0
          spacing: Style.space(10)

          Text {
            id: windowLayoutLabel
            anchors.verticalCenter: parent.verticalCenter
            text: "Window layout"
            color: root.mutedInk
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.bodySmall)
          }

          ChoiceGroup {
            id: windowLayoutChoices
            objectName: "windowLayoutChoices"
            enabled: !popoutResolveProcess.running && !popoutModeProcess.running && !popoutResizeProcess.running
            options: [
              { value: "tiled", label: "Tiled" },
              { value: "floating", label: "Floating" }
            ]
            value: root.popoutMode
            foreground: root.foreground
            background: Color.popups.background
            accent: root.accentInk
            fontFamily: root.fontFamily
            fontSize: root.fs(Style.font.bodySmall)
            focusable: true
            onChanged: function(v) { root.setPopoutMode(v) }
          }
        }

        Text {
          objectName: "popoutModeErrorLabel"
          width: parent.width
          visible: root.popoutOpen && root.popoutModeError !== ""
          text: root.popoutModeError
          textFormat: Text.PlainText
          color: root.urgentInk
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.bodySmall)
          wrapMode: Text.Wrap
        }
      }

      PanelSeparator {
        id: headerSep
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerCol.bottom
        anchors.topMargin: Style.space(8)
        foreground: root.foreground
      }

      readonly property bool anyAccountReady: {
        if (!root.service) return false
        var g = typeof root.service.stateFor === "function" ? root.service.stateFor("gmessages") : (root.service.state || "")
        var w = typeof root.service.stateFor === "function" ? root.service.stateFor("whatsapp") : ""
        var t = typeof root.service.stateFor === "function" ? root.service.stateFor("telegram") : ""
        var m = typeof root.service.stateFor === "function" ? root.service.stateFor("messenger") : ""
        var s = typeof root.service.stateFor === "function" ? root.service.stateFor("signal") : ""
        var enabled = function(net) {
          return !Array.isArray(root.service.enabledServices) || root.service.enabledServices.indexOf(net) >= 0
        }
        var isReady = function(net, st) {
          return enabled(net) && st !== "disabled" && st !== "unpaired" && st !== "pairing" && st !== "gaiaPairing" && st !== "error" && st !== ""
        }
        return isReady("gmessages", g) || isReady("whatsapp", w) || isReady("telegram", t) || isReady("messenger", m) || isReady("signal", s)
      }

      // Retain drafts and selection while Settings or another tab is shown.
      // Unpairing or losing the helper destroys this view and its account data.
      Loader {
        id: inboxLoader
        onLoaded: Qt.callLater(root.selectPendingConversation)
        onVisibleChanged: if (visible) Qt.callLater(function() {
          root.selectPendingConversation()
          root.focusInitialControl()
        })
        objectName: "inboxLoader"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerSep.bottom
        anchors.bottom: parent.bottom
        anchors.topMargin: Style.space(10)
        active: root.service && (root.service.connected || root.service.restartingServices === true) && (parent.anyAccountReady || !root.needsPair)
        visible: active && root.service.connected && !root.service.restartingServices && !root.noServices && (root.serviceLive || root.telegramLive) && !root.unifiedActive && !root.settingsOpen && !root.needsPair
        sourceComponent: inboxView
      }

      Loader {
        id: unifiedInboxLoader
        objectName: "unifiedInboxLoader"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerSep.bottom
        anchors.bottom: parent.bottom
        anchors.topMargin: Style.space(10)
        active: root.service && (root.service.connected || root.service.restartingServices === true)
        visible: active && root.service.connected && !root.service.restartingServices && !root.noServices && root.unifiedActive && !root.settingsOpen
        sourceComponent: unifiedInboxView
      }

      Loader {
        id: bodyLoader
        objectName: "bodyLoader"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerSep.bottom
        anchors.bottom: parent.bottom
        anchors.topMargin: Style.space(10)
        sourceComponent: {
          if (root.settingsOpen) return settingsView
          if (root.unifiedActive) return null
          if (!root.service) return missingServiceView
          if (!root.service.connected) return helperView
          if (root.noServices) return servicesOffView
          if (!root.serviceLive && !root.telegramLive) return comingSoonView
          if (root.telegramLive && root.connState !== "connected") return pairingView
          if (root.needsPair) return pairingView
          return null
        }
      }
    }
  }

  Component {
    id: servicesOffView
    Flickable {
      clip:true
      contentWidth:width
      contentHeight:chooser.implicitHeight
      Controls.ScrollBar.vertical: Controls.ScrollBar { policy:Controls.ScrollBar.AsNeeded }
      Column {
        id:chooser
        width:parent.width
        spacing:Style.space(14)
        Text {
          objectName:"servicesOffLabel"
          width:parent.width; wrapMode:Text.Wrap
          text:root.service && root.service.servicesConfigLoaded === false ? "Loading service choices from the helper. If the helper needs an update, open Settings and rebuild it." : (root.service && root.service.serviceSelectionRequired ? "Choose the services you want to use. Nothing connects until you apply your choices." : "All services are turned off. Your saved accounts are kept. Choose services below or open Settings.")
          color:root.foreground; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
        }
        ServiceOptions { width:parent.width; service:root.service; fontFamily:root.fontFamily; uiScale:root.uiScale }
      }
    }
  }

  Component {
    id: settingsView
    SettingsView {
      service: root.service
      alwaysPopout: root.alwaysPopout
      keepPreviousEmojiSearchText: root.keepPreviousEmojiSearchText
      unifiedInboxEnabled: root.unifiedInboxEnabled
      popoutMode: root.popoutMode
      foreground: root.foreground
      fontFamily: root.fontFamily
      uiScale: root.uiScale
      onScaleSaved: function(s) { root.uiScale = s }
      onWindowPreferencesSaved: function(always, mode) {
        root.alwaysPopout = always
        root.panelConfigLoaded = true
        root.setPopoutMode(mode)
      }
      onEmojiSearchPreferenceSaved: function(keep) { root.keepPreviousEmojiSearchText = keep }
      onUnifiedInboxPreferenceSaved: function(enabled) {
        root.unifiedInboxEnabled = enabled
        if (enabled) root.setActiveService("all", true)
        else if (root.unifiedActive) root.setActiveService(root.providerTabs.length ? root.providerTabs[0].value : "", true)
      }
      onKeyboardShortcutsSaved: function(shortcuts) {
        var validated = Keybindings.validateOverrides(shortcuts || ({}))
        if (validated.ok) root.shortcutOverrides = validated.overrides
      }
    }
  }

  Component {
    id: comingSoonView
    ComingSoon {
      serviceId: root.activeService
      foreground: root.foreground
      fontFamily: root.fontFamily
    }
  }

  Component {
    id: missingServiceView
    Item {
      Flickable {
        anchors.fill: parent
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        contentWidth: width
        contentHeight: Math.max(height, missingCol.implicitHeight + Style.space(32))

        Column {
          id: missingCol
          anchors.horizontalCenter: parent.horizontalCenter
          y: Math.max(Style.space(16), Math.round((parent.height - implicitHeight) / 2))
          spacing: Style.space(12)
          width: Math.min(parent.width - Style.space(40), Style.space(460))

          Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            text: "OmaChat service is not loaded"
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.heading)
            font.bold: true
          }

          Text {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            text: "Enable the plugin, then restart the shell:\nomarchy plugin enable onelegdave.omachat\nomarchy restart shell\n\nKeep this setup panel open when you restart. OmaChat will reopen once the new shell is ready."
            color: root.mutedInk
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.body)
            lineHeight: 1.25
          }
        }
      }
    }
  }

  Component {
    id: helperView
    Item {
      readonly property bool needGo: !!(root.service && !root.service.goPresent && !root.service.helperPresent)
      readonly property bool canBuild: !!(root.service && root.service.goPresent && !root.service.helperPresent)
      readonly property bool connecting: !!(root.service && !root.service.connected && !root.service.building
        && (root.service.helperState === "starting" || root.service.helperState === "running" || root.service.helperState === "restarting"))

      Flickable {
        anchors.fill: parent
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        contentWidth: width
        contentHeight: Math.max(height, helperCol.implicitHeight + Style.space(32))

        Column {
          id: helperCol
          anchors.horizontalCenter: parent.horizontalCenter
          y: Math.max(Style.space(16), Math.round((parent.height - implicitHeight) / 2))
          spacing: Style.space(14)
          width: Math.min(parent.width - Style.space(40), Style.space(480))

          ReadableHero {
            objectName: "helperBuildHero"
            width: parent.width
            uiScale: root.uiScale
            title: {
              if (!root.service) return "Helper not running"
              if (root.service.building) return "Building helper"
              if (root.service.helperState === "starting" || root.service.helperState === "running")
                return "Connecting to helper"
              if (needGo) return "Build tools required"
              if (canBuild) return "Build protocol helper"
              return "Helper not running"
            }
            meta: {
              if (root.service && root.service.helperError) return root.service.helperError
              if (root.service && root.service.building)
                return "Building OmaChat's messaging helper on this computer. The first build can take several minutes, depending on your hardware. Go may show no output while compiling; a quiet screen does not mean the build has stopped. Please wait and do not restart the Omarchy shell during the build. The helper starts automatically when compilation succeeds, or a build error appears here if it fails. This uses the included source without installing packages or downloading modules."
              if (connecting)
                return "OmaChat's messaging helper is starting and connecting. This normally takes only a moment. No action is needed while the activity indicator is moving."
              if (needGo)
                return "OmaChat needs a small background program, the messaging helper (omachatd), to connect your chosen services. It is built on your computer from the included source. Building requires Go 1.27+, Python 3, and a C compiler (gcc or clang). Open Settings > Tools to check what is missing, review its source, and choose whether to install it. Then return here, choose Retry to recheck Go, and select Build helper."
              if (canBuild)
                return "Go was found. Check Settings > Tools to confirm Go 1.27+, Python 3, and a C compiler (gcc or clang) are available. Choose Build helper to compile the included source for your selected services. The first build can take several minutes and may show no output. The helper starts automatically on success. Rebuild after updates that change the helper. No dependencies are installed automatically."
              return "The protocol helper runs as a background process owned by the Omarchy shell."
            }
            foreground: root.foreground
            metaColor: root.mutedInk
            fontFamily: root.fontFamily
            iconComponent: Component {
              Item {
                implicitWidth: root.fs(Style.font.display)
                implicitHeight: root.fs(Style.font.display)

                OpticalGlyph {
                  id: helperHeroGlyph
                  anchors.centerIn: parent
                  implicitWidth: root.fs(Style.font.display)
                  implicitHeight: root.fs(Style.font.display)
                  text: root.service && (root.service.building || connecting) ? "󰑐" : "󰭹"
                  color: root.accentInk
                  fontFamily: root.fontFamily
                  fontSize: root.fs(Style.font.display)
                  transformOrigin: Item.Center
                  rotation: 0

                  RotationAnimation on rotation {
                    from: 0
                    to: 360
                    duration: 1200
                    loops: Animation.Infinite
                    running: !!(root.service && (root.service.building || connecting))
                  }
                }
              }
            }
          }

          Item {
            id: buildActivityTrack
            objectName: "buildActivityIndicator"
            visible: !!(root.service && (root.service.building || connecting))
            width: parent.width
            height: Style.space(4)
            clip: true

            Rectangle {
              anchors.fill: parent
              radius: height / 2
              color: root.foreground
              opacity: 0.12
            }

            Rectangle {
              id: buildActivityBar
              height: parent.height
              radius: height / 2
              color: root.accentInk
              width: Math.max(Style.space(64), parent.width * 0.3)
              x: -width

              SequentialAnimation on x {
                running: !!(root.service && (root.service.building || connecting))
                loops: Animation.Infinite

                NumberAnimation {
                  from: -buildActivityBar.width
                  to: buildActivityTrack.width
                  duration: 1400
                  easing.type: Easing.InOutQuad
                }
              }
            }
          }

          Row {
            visible: !!(root.service && (root.service.building || connecting))
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: Style.space(8)

            OpticalGlyph {
              anchors.verticalCenter: parent.verticalCenter
              implicitWidth: root.fs(Style.font.caption)
              implicitHeight: root.fs(Style.font.caption)
              text: "󰑐"
              color: root.accentInk
              fontFamily: root.fontFamily
              fontSize: root.fs(Style.font.caption)
              transformOrigin: Item.Center
              rotation: 0

              RotationAnimation on rotation {
                from: 0
                to: 360
                duration: 900
                loops: Animation.Infinite
                running: !!(root.service && (root.service.building || connecting))
              }
            }

            Text {
              anchors.verticalCenter: parent.verticalCenter
              text: connecting ? "Connecting to helper..." : "Compiling helper in background..."
              color: root.mutedInk
              font.family: root.fontFamily
              font.pixelSize: root.fs(Style.font.caption)
            }
          }

          Text {
            width: parent.width
            visible: needGo
            wrapMode: Text.Wrap
            horizontalAlignment: Text.AlignHCenter
            text: "In your terminal:\nomarchy pkg add go gcc\n\nPackage information: archlinux.org extra/go\nOmaChat never installs software automatically. You choose whether to install tools and enable services."
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.body)
            lineHeight: 1.25
          }

          Row {
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: Style.space(8)

            Button {
              focusable: true
              Accessible.role: Accessible.Button
              Accessible.name: text
              visible: needGo
              text: "Open Go package"
              bordered: true
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: Util.execArgv(["xdg-open", "https://archlinux.org/packages/extra/x86_64/go/"])
            }

            Button {
              focusable: true
              Accessible.role: Accessible.Button
              Accessible.name: text
              visible: canBuild || (!!(root.service && root.service.building))
              objectName: "buildHelperButton"
              text: root.service && root.service.building ? "Building..." : "Build helper"
              iconText: root.service && root.service.building ? "󰑐" : ""
              iconSpinning: !!(root.service && root.service.building)
              bordered: true
              enabled: !!(root.service && !root.service.building && root.service.goPresent)
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: if (root.service) root.service.buildHelper()
            }

            Button {
              focusable: true
              Accessible.role: Accessible.Button
              Accessible.name: text
              text: "Retry"
              objectName: "helperRetryButton"
              visible: !connecting && !!(root.service && !root.service.building)
              enabled: visible
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: {
                if (!root.service) return
                root.service.checkGo()
                if (root.service.helperPresent) {
                  root.service.rebuildSocket()
                  root.service.startHelper()
                }
              }
            }
          }
        }
      }
    }
  }

  Component {
    id: pairingView
    PairingView {
      service: root.service
      network: root.activeService
      foreground: root.foreground
      fontFamily: root.fontFamily
      uiScale: root.uiScale
      onOpenSettingsRequested: root.settingsOpen = true
    }
  }

  Component {
    id: unifiedInboxView
    UnifiedInbox {
      service: root.service
      host: surfaceHost
      settings: root.settings
      lastConversations: root.lastConversations
      keepPreviousEmojiSearchText: root.keepPreviousEmojiSearchText
      sidebarCollapsed: root.sidebarCollapsed
      viewActive: unifiedInboxLoader.visible
      conversations: root.unifiedConversations.map(function(row) {
        var copy = Object.assign({}, row)
        var tab = root.allServiceTabs.find(function(candidate) { return candidate.value === row.network })
        copy.networkLabel = tab ? tab.label : row.network
        return copy
      })
      foreground: root.foreground
      fontFamily: root.fontFamily
      uiScale: root.uiScale
    }
  }

  Component {
    id: inboxView
    InboxView {
      service: root.service
      network: root.activeService
      foreground: root.foreground
      fontFamily: root.fontFamily
      host: surfaceHost
      keyboardActionRouter: function(event) { root.routeKeyboardEvent(event) }
      viewActive: inboxLoader.visible
      settings: root.settings
      keepPreviousEmojiSearchText: root.keepPreviousEmojiSearchText
      sidebarCollapsed: root.sidebarCollapsed
      lastConversations: root.lastConversations
      onConversationSelected: function(network, id) { root.rememberConversation(network, id) }
      onSendingMediaChanged: if (!sendingMedia) Qt.callLater(root.selectPendingConversation)
      networkLabel: root.activeService === "whatsapp" ? "WhatsApp" : (root.activeService === "telegram" ? "Telegram" : (root.activeService === "messenger" ? "Messenger" : (root.activeService === "signal" ? "Signal" : "Google Messages")))
      uiScale: root.uiScale
    }
  }

  QtObject {
    id: surfaceHost
    readonly property bool opened: root.anySurfaceOpen
    property alias settingsOpen: root.settingsOpen
  }
}
