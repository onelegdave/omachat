import QtQuick
import qs.Commons
import qs.Ui

Column {
  id: root
  property var service: null
  readonly property var notifications: service && service.notifications ? service.notifications : null
  property string fontFamily: Style.font.family
  property real uiScale: 1
  property color foreground: Color.popups.text
  spacing: Style.space(8)
  function fs(n) { return Math.max(12, Math.round(n * uiScale)) }

  Text {
    width: parent.width
    text: "Desktop notifications"
    color: root.foreground
    font.family: root.fontFamily
    font.pixelSize: root.fs(Style.font.body)
    font.bold: true
  }
  Text {
    width: parent.width
    text: "Alerts appear for new incoming messages while OmaChat is running. The chat you are actively reading stays quiet. Click an alert to open its conversation. Your desktop controls Do Not Disturb and notification sounds."
    textFormat: Text.PlainText
    wrapMode: Text.Wrap
    color: root.foreground
    font.family: root.fontFamily
    font.pixelSize: root.fs(Style.font.bodySmall)
  }
  Toggle {
    objectName: "desktopNotificationsToggle"
    width: parent.width
    label: "Notify about new messages"
    checked: !!root.notifications && root.notifications.enabled
    enabled: !!root.notifications && root.notifications.loaded && !root.notifications.saving && root.notifications.connected
    foreground: root.foreground
    fontFamily: root.fontFamily
    titleSize: root.fs(Style.font.body)
    onClicked: root.notifications.savePreferences({enabled: !checked})
  }
  Toggle {
    objectName: "notificationPreviewsToggle"
    width: parent.width
    label: "Show sender and message previews"
    description: "Off by default. Enabling this displays conversation names and message text in desktop alerts and notification history. Cached profile photos can appear even when text previews are off."
    checked: !!root.notifications && root.notifications.previews
    enabled: !!root.notifications && root.notifications.loaded && root.notifications.enabled && !root.notifications.saving && root.notifications.connected
    foreground: root.foreground
    fontFamily: root.fontFamily
    titleSize: root.fs(Style.font.body)
    descriptionSize: root.fs(Style.font.caption)
    onClicked: root.notifications.savePreferences({previews: !checked})
  }
  Button {
    objectName: "testNotificationButton"
    text: "Send test notification"
    enabled: !!root.notifications && root.notifications.enabled && root.notifications.connected
    focusable: true
    bordered: true
    foreground: root.foreground
    fontFamily: root.fontFamily
    fontSize: root.fs(Style.font.bodySmall)
    onClicked: root.notifications.test()
  }
  Text {
    width: parent.width
    visible: text !== ""
    text: !root.notifications || !root.notifications.loaded ? "Rebuild the helper to load notification preferences."
      : root.notifications.errorText
    textFormat: Text.PlainText
    wrapMode: Text.Wrap
    color: root.foreground
    font.family: root.fontFamily
    font.pixelSize: root.fs(Style.font.bodySmall)
  }
}
