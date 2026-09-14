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
    }
  }
}
