import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts
import qs.Commons
import "Keybindings.js" as Keybindings

RowLayout {
  id: root
  required property var action
  property string binding: ""
  property color foreground: "white"
  property color mutedColor: "#aaaaaa"
  property color accentColor: "#7aa2f7"
  property string fontFamily: "sans-serif"
  property int fontSize: 14
  property bool recording: false
  signal saveRequested(string actionId, string shortcut)
  signal resetRequested(string actionId)

  spacing: Style.space(8)
  Accessible.name: action.label + " shortcut"

  Text {
    Layout.fillWidth: true
    Layout.minimumWidth: Style.space(135)
    text: root.action.label
    color: root.foreground
    font.family: root.fontFamily
    font.pixelSize: root.fontSize
    elide: Text.ElideRight
    Accessible.ignored: true
  }

  Controls.TextField {
    id: shortcutInput
    objectName: "shortcutInput_" + root.action.id
    Layout.preferredWidth: Style.space(142)
    Accessible.name: "Shortcut for " + root.action.label
    text: root.binding
    placeholderText: "Unbound"
    color: root.foreground
    placeholderTextColor: root.mutedColor
    selectByMouse: true
    font.family: root.fontFamily
    font.pixelSize: root.fontSize - 1
    background: Rectangle {
      radius: Style.space(6)
      color: "transparent"
      border.width: shortcutInput.activeFocus ? 2 : 1
      border.color: shortcutInput.activeFocus ? root.accentColor : root.mutedColor
    }
    onEditingFinished: root.saveRequested(root.action.id, text.trim())
  }

  Controls.Button {
    id: recordButton
    objectName: "recordShortcut_" + root.action.id
    focusPolicy: Qt.StrongFocus
    Accessible.name: root.recording ? "Press the shortcut for " + root.action.label + ", Escape cancels" : "Record shortcut for " + root.action.label
    text: root.recording ? "Press keys…" : "Record"
    onClicked: {
      root.recording = true
      Qt.callLater(function() { root.forceActiveFocus() })
    }
  }

  Controls.Button {
    objectName: "resetShortcut_" + root.action.id
    focusPolicy: Qt.StrongFocus
    Accessible.name: "Reset shortcut for " + root.action.label
    text: "Reset"
    onClicked: root.resetRequested(root.action.id)
  }

  focus: recording
  activeFocusOnTab: true
  Keys.onPressed: function(event) {
    if (!root.recording) return
    if (event.key === Qt.Key_Escape) {
      root.recording = false
      event.accepted = true
      return
    }
    var shortcut = Keybindings.eventShortcut({
      text: event.text,
      key: event.key,
      ctrl: (event.modifiers & Qt.ControlModifier) !== 0,
      alt: (event.modifiers & Qt.AltModifier) !== 0,
      meta: (event.modifiers & Qt.MetaModifier) !== 0,
      shift: (event.modifiers & Qt.ShiftModifier) !== 0,
      isComposing: event.isComposing === true
    })
    if (shortcut) {
      root.recording = false
      root.saveRequested(root.action.id, shortcut)
    }
    event.accepted = true
  }
}
