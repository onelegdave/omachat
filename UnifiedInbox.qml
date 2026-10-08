import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root
  property var conversations: []
  property color foreground: "white"
  property string fontFamily: "sans-serif"
  property real uiScale: 1
  signal conversationActivated(var conversation)
  readonly property var keyboardInitialFocus: convList
  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }
  function focusConversationList() { convList.forceActiveFocus() }
  function isKeyboardEditing() { return searchField.activeFocus }
  function hasKeyboardModal() { return false }
  function moveConversationCursor(delta) {
    if (convList.count < 1) return
    convList.currentIndex = Math.max(0, Math.min(convList.count - 1, convList.currentIndex + delta))
    convList.forceActiveFocus()
  }
  function handleKeyboardAction(actionId) {
    if (actionId === "search") { searchField.forceActiveFocus(); searchField.selectAll() }
  }
  readonly property var visibleConversations: {
    var query = searchField.text.trim().toLowerCase()
    return conversations.filter(function(conversation) {
      return !query || String(conversation.name || "").toLowerCase().indexOf(query) >= 0
        || String(conversation.preview || "").toLowerCase().indexOf(query) >= 0
        || String(conversation.network || "").toLowerCase().indexOf(query) >= 0
    })
  }

  TextField {
    id: searchField
    objectName: "unifiedSearchField"
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: parent.top
    placeholderText: "Search all conversations"
    Accessible.name: "Search all conversations"
    font.family: root.fontFamily
    font.pixelSize: root.fs(Style.font.bodySmall)
  }

  ListView {
    id: convList
    objectName: "unifiedConvList"
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.top: searchField.bottom
    anchors.bottom: parent.bottom
    anchors.topMargin: Style.space(8)
    clip: true
    spacing: Style.space(2)
    model: root.visibleConversations
    activeFocusOnTab: true
    Accessible.role: Accessible.List
    Accessible.name: "All service conversations"
    Keys.onReturnPressed: if (currentIndex >= 0) root.conversationActivated(root.visibleConversations[currentIndex])
    Keys.onEnterPressed: if (currentIndex >= 0) root.conversationActivated(root.visibleConversations[currentIndex])

    delegate: Rectangle {
      required property int index
      required property var modelData
      readonly property bool selected: convList.currentIndex === index
      width: convList.width
      height: Math.max(Style.space(60), root.fs(Style.font.bodySmall) + root.fs(Style.font.caption) + Style.space(24))
      radius: Style.space(4)
      color: selected ? Style.hoverFillFor(root.foreground, Color.accent) : "transparent"
      border.width: selected && convList.activeFocus ? 1 : 0
      border.color: Color.accent
      MouseArea {
        anchors.fill: parent
        onClicked: {
          convList.currentIndex = index
          convList.forceActiveFocus()
          root.conversationActivated(modelData)
        }
      }
      Text {
        anchors.left: parent.left
        anchors.right: serviceBadge.left
        anchors.leftMargin: Style.space(8)
        anchors.rightMargin: Style.space(8)
        anchors.top: parent.top
        anchors.topMargin: Style.space(7)
        text: modelData.name || "(no name)"
        color: root.foreground
        elide: Text.ElideRight
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.bodySmall)
        font.bold: modelData.unread === true
      }
      Text {
        id: serviceBadge
        anchors.right: parent.right
        anchors.rightMargin: Style.space(8)
        anchors.verticalCenter: parent.verticalCenter
        text: modelData.networkLabel || modelData.network
        color: Color.accent
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.caption)
      }
      Text {
        anchors.left: parent.left
        anchors.right: serviceBadge.left
        anchors.leftMargin: Style.space(8)
        anchors.rightMargin: Style.space(8)
        anchors.bottom: parent.bottom
        anchors.bottomMargin: Style.space(7)
        text: modelData.preview || ""
        color: Model.readableInk(Color.popups.background, Color.muted)
        elide: Text.ElideRight
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.caption)
      }
    }
  }
}
