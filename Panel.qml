import QtQuick
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
  readonly property var serviceTabs: [
    { value: "gmessages", label: "Google", icon: "󰭹", tooltip: "Google Messages" },
    { value: "whatsapp", label: "WhatsApp", icon: "󰖣", tooltip: "Coming later" },
    { value: "telegram", label: "Telegram", icon: "\uf2c6", tooltip: "Coming later" }
  ]
  property real uiScale: 1
  function fs(n) { return Math.max(8, Math.round(Number(n) * uiScale)) }
  readonly property bool serviceLive: activeService === "gmessages"

  readonly property var chat: service
  readonly property int unread: service ? (service.unread || 0) : 0
  readonly property string connState: service ? (service.state || "") : ""
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property bool needsPair: connState === "unpaired" || connState === "pairing"
    || connState === "gaiaPairing" || connState === "error"
  readonly property bool linkUp: service && service.connected && connState === "connected"
  readonly property string linkLabel: {
    if (!service) return "GHOST PROC"
    if (!service.connected) return "NO CARRIER"
    if (connState === "connected" && service.status && service.status.phoneOK === false)
      return "HANDSET GHOST"
    if (connState === "connected") return "ON THE WIRE"
    if (connState === "connecting" || connState === "pairing" || connState === "gaiaPairing")
      return "HANDSHAKING"
    if (connState === "unpaired") return "AIR GAP"
    return Model.statusLine(service.status).toUpperCase()
  }

  function refresh() {
    if (service && service.refreshConversations) service.refreshConversations()
    if (bodyLoader.item && bodyLoader.item.loadMessages) bodyLoader.item.loadMessages()
  }

  function loadConfig() {
    if (!service) return
    service.call("config", null, function(ok, res) {
      if (!ok || !res) return
      var s = Number(res.uiScale)
      if (isFinite(s) && s > 0) root.uiScale = s
    })
  }

  onOpenedChanged: {
    if (!opened || !service) return
    loadConfig()
    if (needsPair) service.loadProfiles()
    service.refreshConversations()
  }

  onServiceChanged: loadConfig()

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
      blocked: bodyLoader.item && (bodyLoader.item.composerFocus === true || bodyLoader.item.linkConfirmOpen === true)
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "1") { root.settingsOpen = false; root.activeService = "gmessages" }
        else if (t === "2") { root.settingsOpen = false; root.activeService = "whatsapp" }
        else if (t === "3") { root.settingsOpen = false; root.activeService = "telegram" }
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
            text: "󰭹"
            color: Color.accent
            fontFamily: root.fontFamily
            fontSize: fs(Style.font.heading)
          }

          Text {
            anchors.left: brandGlyph.right
            anchors.leftMargin: Style.space(8)
            anchors.verticalCenter: parent.verticalCenter
            text: "OmaChat"
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.heading)
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
            tooltipText: root.settingsOpen ? "Leave the lab" : "Settings"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.settingsOpen = !root.settingsOpen
          }

          PanelActionButton {
            id: unpairBtn
            visible: root.linkUp
            anchors.right: settingsBtn.left
            anchors.rightMargin: Style.space(2)
            anchors.verticalCenter: parent.verticalCenter
            iconText: "󰍃"
            tooltipText: "Unpair this desktop"
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: if (root.service) root.service.call("unpair", null, null)
          }

          Rectangle {
            id: linkChip
            anchors.right: unpairBtn.visible ? unpairBtn.left : settingsBtn.left
            anchors.rightMargin: Style.space(8)
            anchors.verticalCenter: parent.verticalCenter
            height: Style.space(20)
            width: chipRow.implicitWidth + Style.space(12)
            radius: height / 2
            color: Style.normalFillFor(root.foreground, Color.accent)
            border.width: 1
            border.color: root.linkUp ? Color.accent : Color.urgent

            Row {
              id: chipRow
              anchors.centerIn: parent
              spacing: Style.space(6)

              Rectangle {
                width: Style.space(6)
                height: Style.space(6)
                radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: root.linkUp ? Color.accent : Color.urgent
              }

              Text {
                anchors.verticalCenter: parent.verticalCenter
                text: root.linkLabel
                color: root.linkUp ? Color.accent : Color.urgent
                font.family: root.fontFamily
                font.pixelSize: fs(Style.font.caption)
                font.bold: true
              }
            }
          }
        }

        ButtonGroup {
          width: parent.width
          options: root.serviceTabs
          value: root.activeService
          foreground: root.foreground
          background: Color.popups.background
          accent: Color.accent
          fontFamily: root.fontFamily
          fontSize: fs(Style.font.body)
          focusable: false
          onChanged: function(v) {
            root.settingsOpen = false
            root.activeService = v
          }
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

      Loader {
        id: bodyLoader
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: headerSep.bottom
        anchors.bottom: parent.bottom
        anchors.topMargin: Style.space(10)
        sourceComponent: {
          if (root.settingsOpen) return settingsView
          if (!root.serviceLive) return comingSoonView
          if (!root.service) return missingServiceView
          if (!root.service.connected) return helperView
          if (root.needsPair) return pairingView
          return inboxView
        }
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
      Column {
        anchors.centerIn: parent
        spacing: Style.space(10)
        width: Math.min(parent.width - Style.space(40), Style.space(420))
        Text {
          width: parent.width
          horizontalAlignment: Text.AlignHCenter
          wrapMode: Text.WordWrap
          text: "OmaChat service is not loaded"
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.heading)
          font.bold: true
        }
        Text {
          width: parent.width
          horizontalAlignment: Text.AlignHCenter
          wrapMode: Text.WordWrap
          text: "Enable the plugin, then restart the shell: omarchy plugin enable onelegdave.omachat"
          color: Color.muted
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.bodySmall)
        }
      }
    }
  }

  Component {
    id: helperView
    Item {
      readonly property bool needGo: root.service && !root.service.goPresent && !root.service.helperPresent
      readonly property bool canBuild: root.service && root.service.goPresent && !root.service.helperPresent

      Column {
        anchors.centerIn: parent
        spacing: Style.space(12)
        width: Math.min(parent.width - Style.space(40), Style.space(460))

        PanelHero {
          width: parent.width
          title: {
            if (!root.service) return "Helper not running"
            if (root.service.building) return "Building helper"
            if (root.service.helperState === "starting" || root.service.helperState === "running")
              return "Connecting"
            if (needGo) return "Go is required once"
            if (canBuild) return "Build the helper"
            return "Helper not running"
          }
          meta: {
            if (root.service && root.service.helperError) return root.service.helperError
            if (root.service && root.service.building)
              return "Compiling omachatd from this plugin folder. No extra downloads."
            if (needGo)
              return "OmaChat talks to Google Messages through a small helper. It is not on a default Omarchy install. Install Go yourself, then come back and build."
            if (canBuild)
              return "Go is installed. Build omachatd from the files in this plugin. That happens once."
            return "The protocol helper runs as a child of the Omarchy shell."
          }
          foreground: root.foreground
          fontFamily: root.fontFamily
          iconComponent: Component {
            OpticalGlyph {
              implicitWidth: fs(Style.font.display)
              implicitHeight: fs(Style.font.display)
              text: "󰭹"
              color: Color.accent
              fontFamily: root.fontFamily
              fontSize: fs(Style.font.display)
            }
          }
        }

        Text {
          width: parent.width
          visible: needGo
          wrapMode: Text.WordWrap
          horizontalAlignment: Text.AlignHCenter
          text: "In a terminal:\nomarchy pkg add go\n\nPackage page: archlinux.org extra/go\nNothing is installed for you."
          color: root.foreground
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
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
            visible: canBuild || (root.service && root.service.building)
            text: root.service && root.service.building ? "Building" : "Build helper"
            bordered: true
            enabled: root.service && !root.service.building && root.service.goPresent
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

  Component {
    id: pairingView
    PairingView {
      service: root.service
      foreground: root.foreground
      fontFamily: root.fontFamily
    }
  }

  Component {
    id: inboxView
    InboxView {
      service: root.service
      foreground: root.foreground
      fontFamily: root.fontFamily
      host: root
      settings: root.settings
      networkLabel: "Google Messages"
      uiScale: root.uiScale
    }
  }
}
