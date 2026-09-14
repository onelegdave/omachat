import QtQuick
import QtQuick.Controls as Controls
import qs.Commons
import qs.Ui
import "Model.js" as Model

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
  property bool settingsOpen: false
  property bool unpairing: false
  property string unpairError: ""
  property int accountGeneration: 0
  readonly property var allServiceTabs: [
    { value: "gmessages", label: "Google", icon: "󰭹", tooltip: "Google Messages" },
    { value: "whatsapp", label: "WhatsApp", icon: "󰖣", tooltip: "WhatsApp" },
    { value: "telegram", label: "Telegram", icon: "\uf2c6", tooltip: "Telegram" }
  ]
  readonly property var serviceTabs: allServiceTabs.filter(function(tab) {
    return !root.service || !Array.isArray(root.service.enabledServices) || root.service.enabledServices.indexOf(tab.value) >= 0
  })
  readonly property bool noServices: serviceTabs.length === 0
  onServiceTabsChanged: syncActiveService()
  function syncActiveService() {
    if (serviceTabs.some(function(tab) { return tab.value === root.activeService })) return
    activeService = serviceTabs.length ? serviceTabs[0].value : ""
    if(service) service.currentNetwork=activeService
  }
  property real uiScale: 1
  function fs(n) { return Math.max(8, Math.round(Number(n) * uiScale)) }
  readonly property bool serviceLive: activeService === "gmessages" || activeService === "whatsapp"
  readonly property bool telegramLive: activeService === "telegram"

  readonly property var chat: service
  readonly property int unread: service ? (typeof service.unreadFor === "function" ? service.unreadFor(activeService) : (service.unread || 0)) : 0
  readonly property string connState: service ? (typeof service.stateFor === "function" ? service.stateFor(activeService) : (service.state || "")) : ""
  readonly property var activeStatus: service ? (typeof service.statusFor === "function" ? service.statusFor(activeService) : service.status) : null

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
    if (service && service.refreshConversations) service.refreshConversations(activeService)
    if (inboxLoader.item) inboxLoader.item.refreshThread()
  }

  function setActiveService(v) {
    if (!serviceTabs.some(function(tab) { return tab.value === v })) return
    root.settingsOpen = false
    root.activeService = v
    if (root.service) {
      root.service.currentNetwork = v
      if (v === "gmessages" || v === "whatsapp" || v === "telegram") root.service.loadConversations(v)
    }
  }

  function loadConfig() {
    if (!service) return
    service.call("config", null, function(ok, res) {
      if (!ok || !res) return
      if (typeof service.applyServiceConfig === "function" && !service.savingServices) service.applyServiceConfig(res)
      var s = Number(res.uiScale)
      if (isFinite(s) && s > 0) root.uiScale = s
    }, "gmessages")
  }

  onOpenedChanged: {
    if (!opened || !service) return
    loadConfig()
    if (needsPair && activeService === "gmessages") service.loadProfiles()
    if (!noServices) service.loadConversations(activeService)
  }

  onServiceChanged: {
    syncActiveService()
    accountGeneration++
    unpairing = false
    unpairError = ""
    loadConfig()
  }

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
      if (!ok) {
        var advice = currentNet === "whatsapp"
          ? " Check WhatsApp on your phone under Linked devices."
          : (currentNet === "telegram"
            ? " Check Telegram on your phone or desktop."
            : " Check Google Messages on your phone under Device pairing.")
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
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(920))
    contentHeight: panel.cappedContentHeight(Style.space(580))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: inboxLoader.visible && inboxLoader.item && (inboxLoader.item.composerFocus === true || inboxLoader.item.linkConfirmOpen === true)
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "1") root.setActiveService("gmessages")
        else if (t === "2") root.setActiveService("whatsapp")
        else if (t === "3") root.setActiveService("telegram")
        else if (t === "r" || t === "R") root.refresh()
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

          OpticalGlyph {
            id: brandGlyph
            anchors.left: parent.left
            anchors.verticalCenter: parent.verticalCenter
            width: Style.space(22)
            height: Style.space(22)
            text: root.activeService === "whatsapp" ? "󰖣" : (root.activeService === "telegram" ? "\uf2c6" : "󰭹")
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

          PanelActionButton {
            id: unpairBtn
            objectName: "unpairButton"
            enabled: !root.unpairing
            visible: root.linkUp
            anchors.right: settingsBtn.left
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
            anchors.right: unpairBtn.visible ? unpairBtn.left : settingsBtn.left
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

        ChoiceGroup {
          width: parent.width
          options: root.serviceTabs
          value: root.activeService
          foreground: root.foreground
          background: Color.popups.background
          accent: root.accentInk
          fontFamily: root.fontFamily
          fontSize: root.fs(Style.font.body)
          focusable: false
          onChanged: function(v) { root.setActiveService(v) }
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
        var isReady = function(st) { return st !== "unpaired" && st !== "pairing" && st !== "gaiaPairing" && st !== "error" && st !== "" }
        return isReady(g) || isReady(w) || isReady(t)
      }

      // Retain drafts and selection while Settings or another tab is shown.
      // Unpairing or losing the helper destroys this view and its account data.
      Loader {
        id: inboxLoader
        objectName: "inboxLoader"
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerSep.bottom
        anchors.bottom: parent.bottom
        anchors.topMargin: Style.space(10)
        active: root.service && (root.service.connected || root.service.restartingServices === true) && (parent.anyAccountReady || !root.needsPair)
        visible: active && root.service.connected && !root.service.restartingServices && !root.noServices && (root.serviceLive || root.telegramLive) && !root.settingsOpen && !root.needsPair
        sourceComponent: inboxView
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
      foreground: root.foreground
      fontFamily: root.fontFamily
      uiScale: root.uiScale
      onScaleSaved: function(s) { root.uiScale = s }
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
            text: "Enable the plugin, then restart the shell:\nomarchy plugin enable onelegdave.omachat\nomarchy restart shell"
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
      readonly property bool needGo: !!root.service && !root.service.goPresent && !root.service.helperPresent
      readonly property bool canBuild: !!root.service && root.service.goPresent && !root.service.helperPresent

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
                return "Compiling omachatd from the vendored source files in this plugin. No external downloads are performed."
              if (needGo)
                return "OmaChat requires a locally compiled helper (omachatd) to connect to Google Messages, WhatsApp, and Telegram. Install Go and a C compiler (gcc or clang) using your system package manager, then choose Build helper below."
              if (canBuild)
                return "Go is installed. Build the shared helper from this plugin's vendored source. All services also require a C compiler (gcc or clang) to build. Rebuild after updates that change the helper."
              return "The protocol helper runs as a background process owned by the Omarchy shell."
            }
            foreground: root.foreground
            metaColor: root.mutedInk
            fontFamily: root.fontFamily
            iconComponent: Component {
              OpticalGlyph {
                implicitWidth: root.fs(Style.font.display)
                implicitHeight: root.fs(Style.font.display)
                text: "󰭹"
                color: root.accentInk
                fontFamily: root.fontFamily
                fontSize: root.fs(Style.font.display)
              }
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
              visible: needGo
              text: "Open Go package"
              bordered: true
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: Util.execArgv(["xdg-open", "https://archlinux.org/packages/extra/x86_64/go/"])
            }

            Button {
              visible: canBuild || (!!root.service && root.service.building)
              objectName: "buildHelperButton"
              text: root.service && root.service.building ? "Building" : "Build helper"
              bordered: true
              enabled: !!root.service && !root.service.building && root.service.goPresent
              foreground: root.foreground
              fontFamily: root.fontFamily
              onClicked: if (root.service) root.service.buildHelper()
            }

            Button {
              text: "Retry"
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
    }
  }

  Component {
    id: inboxView
    InboxView {
      service: root.service
      network: root.activeService
      foreground: root.foreground
      fontFamily: root.fontFamily
      host: root
      viewActive: inboxLoader.visible
      settings: root.settings
      networkLabel: root.activeService === "whatsapp" ? "WhatsApp" : (root.activeService === "telegram" ? "Telegram" : "Google Messages")
      uiScale: root.uiScale
    }
  }
}
