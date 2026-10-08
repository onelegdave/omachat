import QtQuick
import QtQuick.Controls
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root
  property var conversations: []
  property var service: null
  property var host: null
  property var settings: null
  property var lastConversations: ({})
  property bool viewActive: true
  property bool keepPreviousEmojiSearchText: false
  property bool sidebarCollapsed: false
  property int unreadRevision: 0
  property color foreground: "white"
  property string fontFamily: "sans-serif"
  property real uiScale: 1
  property var selectedConversation: null
  readonly property string detailNetwork: selectedConversation ? selectedConversation.network : "gmessages"
  readonly property string detailConversationID: selectedConversation ? selectedConversation.id : ""
  readonly property var detailView: detailInbox
  readonly property bool sidebarVisible: !sidebarCollapsed || !selectedConversation
  readonly property var keyboardInitialFocus: sidebarVisible ? convList : detailInbox.keyboardInitialFocus

  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }
  function focusConversationList() {
    if (!sidebarVisible) {
      if (detailInbox) detailInbox.focusConversationList()
      return
    }
    if (convList.count > 0 && selectedConversation) {
      for (var i = 0; i < visibleConversations.length; i++) {
        if (visibleConversations[i].key === selectedConversation.key) {
          convList.currentIndex = i
          break
        }
      }
    }
    convList.forceActiveFocus()
  }
  function isKeyboardEditing() {
    return searchField.activeFocus || (detailInbox && detailInbox.isKeyboardEditing())
  }
  function hasKeyboardModal() { return detailInbox && detailInbox.hasKeyboardModal() }
  function moveConversationCursor(delta) {
    if (convList.count < 1) return
    convList.currentIndex = Math.max(0, Math.min(convList.count - 1, convList.currentIndex + delta))
    convList.forceActiveFocus()
  }
  function handleKeyboardAction(actionId) {
    if (actionId === "search") { searchField.forceActiveFocus(); searchField.selectAll() }
    else if (actionId === "nextUnread") selectNextUnreadConversation()
    else if (detailInbox) detailInbox.handleKeyboardAction(actionId)
  }
  function refreshThread() {
    if (detailInbox && detailInbox.selectedConvID) detailInbox.refreshThread()
  }
  function selectConversation(conversation) {
    if (!conversation) return
    selectedConversation = conversation
    for (var i = 0; i < visibleConversations.length; i++) {
      if (visibleConversations[i].key === conversation.key) {
        convList.currentIndex = i
        break
      }
    }
    convList.positionViewAtIndex(convList.currentIndex, ListView.Contain)
  }
  function selectNextUnreadConversation() {
    var start = -1
    for (var i = 0; i < visibleConversations.length; i++) {
      if (selectedConversation && visibleConversations[i].key === selectedConversation.key) { start = i; break }
    }
    for (var step = 1; step <= visibleConversations.length; step++) {
      var index = (start + step + visibleConversations.length) % visibleConversations.length
      if (visibleConversations[index].unread === true) {
        convList.currentIndex = index
        selectConversation(visibleConversations[index])
        focusConversationList()
        return
      }
    }
  }
  function openFocusedConversation() {
    if (convList.currentIndex >= 0) selectConversation(visibleConversations[convList.currentIndex])
  }
  readonly property var visibleConversations: {
    var query = searchField.text.trim().toLowerCase()
    return conversations.filter(function(conversation) {
      return !query || String(conversation.name || "").toLowerCase().indexOf(query) >= 0
        || String(conversation.preview || "").toLowerCase().indexOf(query) >= 0
        || String(conversation.networkLabel || conversation.network || "").toLowerCase().indexOf(query) >= 0
    })
  }

  onConversationsChanged: {
    if (!selectedConversation) return
    selectedConversation = conversations.find(function(row) { return row.key === selectedConversation.key }) || null
  }
  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onConversationsChanged() { root.unreadRevision++ }
    function onConversationsWAChanged() { root.unreadRevision++ }
    function onConversationsTGChanged() { root.unreadRevision++ }
    function onConversationsFBChanged() { root.unreadRevision++ }
    function onConversationsSGChanged() { root.unreadRevision++ }
  }

  Item {
    id: leftPane
    objectName: "unifiedConversationSidebar"
    anchors.left: parent.left
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    width: visible ? Math.round(parent.width * 0.34) : 0
    visible: root.sidebarVisible

    PanelSectionHeader {
      id: inboxHeader
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: parent.top
      text: "ALL INBOXES"
      color: Model.readableInk(Color.popups.background, Color.muted)
      foreground: root.foreground
      fontFamily: root.fontFamily
    }

    TextField {
      id: searchField
      objectName: "unifiedSearchField"
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: inboxHeader.bottom
      anchors.topMargin: Style.space(6)
      placeholderText: "Search conversations"
      Accessible.name: "Search all conversations"
      font.family: root.fontFamily
      font.pixelSize: root.fs(Style.font.bodySmall)
      foreground: root.foreground
    }

    Rectangle {
      id: nextUnread
      objectName: "unifiedNextUnread"
      readonly property int unreadCount: {
        var revision = root.unreadRevision
        return root.conversations.filter(function(conversation) { return conversation.unread === true }).length
      }
      visible: unreadCount > 0
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
        onClicked: root.selectNextUnreadConversation()
      }
      Text {
        anchors.centerIn: parent
        text: parent.unreadCount === 1 ? "Next unread (1)" : "Next unread (" + parent.unreadCount + ")"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.caption)
        font.bold: true
      }
    }

    ListView {
      id: convList
      objectName: "unifiedConvList"
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.top: nextUnread.visible ? nextUnread.bottom : searchField.bottom
      anchors.bottom: parent.bottom
      anchors.topMargin: Style.space(8)
      clip: true
      spacing: Style.space(2)
      model: root.visibleConversations
      activeFocusOnTab: true
      Accessible.role: Accessible.List
      Accessible.name: "All service conversations"
      Keys.onReturnPressed: root.openFocusedConversation()
      Keys.onEnterPressed: root.openFocusedConversation()

      delegate: Rectangle {
        id: convItem
        required property int index
        required property var modelData
        readonly property bool selected: root.selectedConversation && root.selectedConversation.key === modelData.key
        width: convList.width
        height: Math.max(Style.space(60), root.fs(Style.font.bodySmall) + root.fs(Style.font.caption) + Style.space(24))
        radius: Style.space(4)
        color: selected
          ? Style.normalFillFor(root.foreground, Color.accent)
          : (convMouse.containsMouse ? Style.hoverFillFor(root.foreground, Color.accent) : "transparent")
        border.width: selected || (convItem.ListView.isCurrentItem && convList.activeFocus) ? 1 : 0
        border.color: convItem.ListView.isCurrentItem && convList.activeFocus ? Color.accent : (selected ? Color.accent : "transparent")

        MouseArea {
          id: convMouse
          anchors.fill: parent
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: {
            convList.currentIndex = index
            convList.forceActiveFocus()
            root.selectConversation(convItem.modelData)
          }
        }

        Avatar {
          id: convAvatar
          objectName: "unifiedSelectedAvatar"
          anchors.left: parent.left
          anchors.leftMargin: Style.space(6)
          anchors.verticalCenter: parent.verticalCenter
          implicitWidth: Style.space(32)
          implicitHeight: Style.space(32)
          imagePath: convItem.modelData.avatarPath || ""
          initials: convItem.modelData.initials || "#"
          hexColor: convItem.modelData.avatarColor || ""
          seed: convItem.modelData.key || convItem.modelData.id || ""
          fontFamily: root.fontFamily
        }

        Text {
          id: serviceBadge
          objectName: "unifiedServiceBadge"
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.top: parent.top
          anchors.topMargin: Style.space(6)
          text: convItem.modelData.networkLabel || convItem.modelData.network
          color: root.foreground
          elide: Text.ElideRight
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.caption)
        }

        Text {
          anchors.left: convAvatar.right
          anchors.leftMargin: Style.space(8)
          anchors.right: serviceBadge.left
          anchors.rightMargin: Style.space(6)
          anchors.top: parent.top
          anchors.topMargin: Style.space(7)
          text: convItem.modelData.name || "(no name)"
          color: root.foreground
          elide: Text.ElideRight
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.bodySmall)
          font.bold: convItem.modelData.unread === true
        }

        Text {
          anchors.left: convAvatar.right
          anchors.leftMargin: Style.space(8)
          anchors.right: parent.right
          anchors.rightMargin: Style.space(8)
          anchors.bottom: parent.bottom
          anchors.bottomMargin: Style.space(7)
          text: convItem.modelData.preview || ""
          color: Model.readableInk(Color.popups.background, Color.muted)
          elide: Text.ElideRight
          font.family: root.fontFamily
          font.pixelSize: root.fs(Style.font.caption)
        }
      }
    }
  }

  Rectangle {
    id: paneRule
    visible: root.sidebarVisible
    anchors.left: leftPane.right
    anchors.top: parent.top
    anchors.bottom: parent.bottom
    anchors.leftMargin: Style.space(8)
    width: 1
    color: Color.popups.border
  }

  Item {
    id: detailPane
    objectName: "unifiedDetailPane"
    anchors.left: root.sidebarVisible ? paneRule.right : parent.left
    anchors.leftMargin: root.sidebarVisible ? Style.space(10) : 0
    anchors.right: parent.right
    anchors.top: parent.top
    anchors.bottom: parent.bottom

    InboxView {
      id: detailInbox
      objectName: "unifiedDetailInbox"
      anchors.fill: parent
      service: root.service
      host: root.host
      settings: root.settings
      foreground: root.foreground
      fontFamily: root.fontFamily
      uiScale: root.uiScale
      network: root.detailNetwork
      detailOnly: true
      sidebarCollapsed: root.sidebarCollapsed
      detailConversationID: root.detailConversationID
      lastConversations: root.lastConversations
      keepPreviousEmojiSearchText: root.keepPreviousEmojiSearchText
      viewActive: root.viewActive
    }
  }
}
