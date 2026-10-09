import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts
import qs.Commons
import "Keybindings.js" as Keybindings

Column {
  id: root
  property var service: null
  property var overrides: ({})
  property color foreground: "white"
  property color mutedColor: "#aaaaaa"
  property color accentColor: "#7aa2f7"
  property color errorColor: "#d65a5a"
  property string fontFamily: "sans-serif"
  property int fontSize: 14
  property bool saving: false
  property bool captureActive: false
  property string statusText: ""
  property string errorText: ""
  spacing: Style.space(8)
  signal saved(var shortcuts)

  function replaceShortcut(actionId, shortcut) {
    if (!root.service || root.saving) return
    var next = Object.assign({}, root.overrides)
    next[actionId] = shortcut
    var result = Keybindings.validateOverrides(next)
    if (!result.ok) {
      root.errorText = result.error
      root.statusText = ""
      return
    }
    save(next)
  }

  function resetShortcut(actionId) {
    if (!root.service || root.saving) return
    var next = Object.assign({}, root.overrides)
    delete next[actionId]
    save(next)
  }

  function resetAll() { if (root.service && !root.saving) save(({})) }

  function cancelRecording() {
    for (var i = 0; i < shortcutRepeater.count; i++) {
      var row = shortcutRepeater.itemAt(i)
      if (row && row.recording) {
        row.recording = false
        root.captureActive = false
        return true
      }
    }
    return false
  }

  function save(next) {
    root.saving = true
    root.errorText = ""
    root.statusText = ""
    root.service.call("setKeyboardShortcuts", { shortcuts: next }, function(ok, result) {
      root.saving = false
      if (!ok || !result) {
        root.errorText = String(result || "Could not save keyboard shortcuts.")
        return
      }
      var validation = Keybindings.validateOverrides(result.keyboardShortcuts || ({}))
      if (!validation.ok) {
        root.errorText = validation.error
        return
      }
      root.overrides = result.keyboardShortcuts || ({})
      root.statusText = "Keyboard shortcuts saved."
      root.saved(result.keyboardShortcuts || ({}))
    }, "gmessages")
  }

  RowLayout {
    width: parent.width
    Text {
      Layout.fillWidth: true
      text: "Keyboard shortcuts"
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: root.fontSize + 2
      font.bold: true
    }
    Controls.Button {
      text: "Reset all"
      Accessible.name: "Reset all keyboard shortcuts to defaults"
      enabled: !root.saving
      onClicked: root.resetAll()
    }
  }

  Text {
    width: parent.width
    wrapMode: Text.Wrap
    text: "Record a chord or type one directly. Clear a field and press Enter to unbind that action. Tab, arrows, Enter, and Escape remain available for navigation."
    color: root.mutedColor
    font.family: root.fontFamily
    font.pixelSize: root.fontSize
  }

  Repeater {
    id: shortcutRepeater
    model: Keybindings.actions()
    delegate: KeyboardShortcutRow {
      required property var modelData
      width: root.width
      action: modelData
      binding: Object.prototype.hasOwnProperty.call(root.overrides, modelData.id)
        ? String(root.overrides[modelData.id] || "")
        : [modelData.defaultShortcut, modelData.alternateShortcut].filter(function(value) { return !!value }).join(" / ")
      foreground: root.foreground
      mutedColor: root.mutedColor
      accentColor: root.accentColor
      fontFamily: root.fontFamily
      fontSize: root.fontSize
      onSaveRequested: function(id, shortcut) { root.replaceShortcut(id, shortcut) }
      onResetRequested: function(actionId) { root.resetShortcut(actionId) }
      onRecordingChanged: root.captureActive = recording
    }
  }

  Text {
    width: parent.width
    visible: root.errorText !== "" || root.statusText !== ""
    wrapMode: Text.Wrap
    text: root.errorText !== "" ? root.errorText : root.statusText
    color: root.errorText !== "" ? root.errorColor : root.accentColor
    font.family: root.fontFamily
    font.pixelSize: root.fontSize

  }
}
