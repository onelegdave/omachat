import QtQuick
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

BarWidget {
  id: root
  moduleName: "onelegdave.omachat"

  readonly property var chat: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor("onelegdave.omachat")
    : null

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    if ("bar" in target) target.bar = root.bar
    if ("settings" in target) target.settings = root.settings
    if ("anchorItem" in target) target.anchorItem = button
    if ("hostWidget" in target) target.hostWidget = root
    if ("service" in target) target.service = root.chat
  }

  function togglePanel() {
    if (panelLoader.item && panelLoader.item.toggle) panelLoader.item.toggle()
  }

  function refresh() {
    if (panelLoader.item && panelLoader.item.refresh) panelLoader.item.refresh()
    else if (root.chat && root.chat.refreshConversations) root.chat.refreshConversations()
  }

  readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false
  readonly property int unread: chat ? (chat.unread || 0) : 0
  readonly property string connState: chat ? (chat.state || "") : ""
  readonly property bool live: chat && chat.connected && connState === "connected"

  function open() { if (panelLoader.item && panelLoader.item.open) panelLoader.item.open() }
  function close() { if (panelLoader.item && panelLoader.item.close) panelLoader.item.close() }

  function openSettings() {
    if (!panelLoader.item) return
    panelLoader.item.settingsOpen = true
    open()
  }

  IpcHandler {
    target: "onelegdave.omachat"
    function showSettings(): void { root.openSettings() }
    function showService(network: string): void {
      if (["gmessages", "whatsapp", "telegram"].indexOf(network) < 0 || !panelLoader.item) return
      panelLoader.item.setActiveService(network)
      root.open()
    }
  }

  readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false
  function closeForPopoutSwitch() { if (panelLoader.item) panelLoader.item.closeForPopoutSwitch() }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onBarChanged: injectPanel()
  onSettingsChanged: injectPanel()
  onChatChanged: injectPanel()

  Timer {
    interval: 500
    running: root.chat === null
    repeat: true
    onTriggered: root.injectPanel()
  }

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "󰭹"
    slotSize: Style.bar.statusSlot
    tooltipText: root.unread > 0
      ? root.unread + (root.unread === 1 ? " unread conversation" : " unread conversations")
      : "OmaChat"
    opacity: root.live ? 1.0 : 0.55

    onPressed: function(b) {
      if (b === Qt.MiddleButton) root.refresh()
      else root.togglePanel()
    }

    Rectangle {
      visible: root.unread > 0
      anchors.right: parent.right
      anchors.top: parent.top
      anchors.rightMargin: Style.space(2)
      anchors.topMargin: Style.space(2)
      width: Math.max(Style.space(14), badgeText.implicitWidth + Style.space(6))
      height: Style.space(14)
      radius: height / 2
      color: Color.urgent

      Text {
        id: badgeText
        anchors.centerIn: parent
        text: root.unread > 9 ? "9+" : String(root.unread)
        color: Model.readableInk(parent.color, Color.background)
        font.family: root.bar ? root.bar.fontFamily : Style.font.family
        font.pixelSize: Style.space(9)
        font.bold: true
      }
    }
  }
}
