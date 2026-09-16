import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Popup choices wrap on narrow panels and derive their ink from each fill.
Flow {
  id: root
  property var options: []
  property string value: ""
  property color foreground: Color.popups.text
  property color background: Color.popups.background
  property color accent: Color.accent
  property string fontFamily: Style.font.family
  property real fontSize: Style.font.body
  property bool focusable: true
  signal changed(string value)
  spacing: Style.space(8)
  Repeater {
    model: root.options
    Button {
      id: choice
      objectName: modelData.unread !== undefined ? "serviceTab-" + optionValue : ""
      required property var modelData
      readonly property string optionValue: String(modelData.value)
      readonly property bool checked: optionValue === root.value
      text: modelData.label
      iconText: modelData.icon || ""
      tooltipText: modelData.tooltip || ""
      bordered: true
      focusable: root.focusable
      fontFamily: root.fontFamily
      fontSize: root.fontSize
      color: Model.mix(root.background, root.accent, checked ? 0.28 : (hot ? 0.12 : 0))
      foreground: Model.readableInk(color, root.foreground)
      accent: Model.readableInk(color, root.accent)
      borderSpec: Border.controlSpec(checked ? "selected" : "normal", foreground, accent)
      onClicked: root.changed(optionValue)

      Rectangle {
        objectName: choice.modelData.unread !== undefined ? "serviceTabBadge-" + choice.optionValue : ""
        visible: Number(choice.modelData.unread || 0) > 0
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.rightMargin: -Style.space(3)
        anchors.topMargin: -Style.space(3)
        width: Math.max(Style.space(14), serviceBadgeText.implicitWidth + Style.space(6))
        height: Style.space(14)
        radius: height / 2
        color: Color.urgent

        Text {
          id: serviceBadgeText
          anchors.centerIn: parent
          text: Number(choice.modelData.unread || 0) > 9 ? "9+" : String(Number(choice.modelData.unread || 0))
          color: Model.readableInk(parent.color, Color.background)
          font.family: root.fontFamily
          font.pixelSize: Style.space(9)
          font.bold: true
        }
      }
    }
  }
}
