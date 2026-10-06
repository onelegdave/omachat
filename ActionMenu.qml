import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Controls
import QtQuick.Layouts
import qs.Commons
import "Keybindings.js" as Keybindings

Controls.Popup {
  id: root
  objectName: "keyboardActionMenu"
  property var enabledServices: []
  property var overrides: ({})
  property color foreground: "white"
  property color mutedColor: "#aaaaaa"
  property color accentColor: "#7aa2f7"
  property color surfaceColor: "#20242b"
  property string fontFamily: "sans-serif"
  property int fontSize: 14
  readonly property var filteredActions: Keybindings.actionsForQuery(search.text, overrides, enabledServices)
  signal triggered(string actionId)

  modal: true
  focus: true
  closePolicy: Controls.Popup.CloseOnEscape | Controls.Popup.CloseOnPressOutside
  anchors.centerIn: Overlay.overlay
  width: Math.min(Style.space(520), Math.max(Style.space(320), Overlay.overlay ? Overlay.overlay.width - Style.space(32) : Style.space(520)))
  height: Math.min(Style.space(560), Math.max(Style.space(250), Overlay.overlay ? Overlay.overlay.height - Style.space(48) : Style.space(560)))
  padding: Style.space(14)

  background: Rectangle {
    radius: Style.space(12)
    color: root.surfaceColor
    border.width: 1
    border.color: root.accentColor
  }

  onOpened: Qt.callLater(function() { search.forceActiveFocus() })
  onClosed: search.text = ""

  contentItem: ColumnLayout {
    spacing: Style.space(10)

    RowLayout {
      Layout.fillWidth: true
      Text {
        text: "Keyboard actions"
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: root.fontSize + 2
        font.bold: true
        Layout.fillWidth: true
      }
      Text {
        text: "Esc to close"
        color: root.mutedColor
        font.family: root.fontFamily
        font.pixelSize: root.fontSize - 1
      }
    }

    Controls.TextField {
      id: search
      objectName: "keyboardActionSearch"
      Layout.fillWidth: true
      placeholderText: "Search actions…"
      Accessible.name: "Search keyboard actions"
      color: root.foreground
      placeholderTextColor: root.mutedColor
      selectByMouse: true
      font.family: root.fontFamily
      font.pixelSize: root.fontSize
      background: Rectangle {
        radius: Style.space(8)
        color: root.surfaceColor
        border.width: search.activeFocus ? 2 : 1
        border.color: search.activeFocus ? root.accentColor : root.mutedColor
      }
      Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Down && actionList.count > 0) {
          actionList.currentIndex = Math.max(0, actionList.currentIndex)
          actionList.forceActiveFocus()
          event.accepted = true
        } else if (event.key === Qt.Key_Escape) {
          root.close()
          event.accepted = true
        }
      }
    }

    ListView {
      id: actionList
      objectName: "keyboardActionList"
      Layout.fillWidth: true
      Layout.fillHeight: true
      clip: true
      model: root.filteredActions
      currentIndex: count > 0 ? 0 : -1
      keyNavigationEnabled: true
      keyNavigationWraps: false
      Accessible.role: Accessible.List
      Accessible.name: "Keyboard actions"
      Controls.ScrollBar.vertical: Controls.ScrollBar {}

      delegate: Controls.ItemDelegate {
        id: actionRow
        width: actionList.width
        height: Style.space(48)
        Accessible.role: Accessible.Button
        Accessible.name: modelData.label + (modelData.shortcut ? ", " + modelData.shortcut : "")
        onClicked: {
          root.close()
          root.triggered(modelData.id)
        }
        background: Rectangle {
          radius: Style.space(6)
          color: actionRow.highlighted || actionRow.activeFocus ? Qt.rgba(root.accentColor.r, root.accentColor.g, root.accentColor.b, 0.22) : "transparent"
        }
        contentItem: RowLayout {
          spacing: Style.space(10)
          Text {
            Layout.fillWidth: true
            text: modelData.label
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: root.fontSize
            elide: Text.ElideRight
          }
          Text {
            text: modelData.shortcut
            color: root.accentColor
            font.family: root.fontFamily
            font.pixelSize: root.fontSize - 1
            visible: text !== ""
          }
        }
      }

      Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Enter || event.key === Qt.Key_Return) {
          if (currentIndex >= 0 && currentIndex < count) {
            var action = model[currentIndex]
            root.close()
            root.triggered(action.id)
          }
          event.accepted = true
        } else if (event.key === Qt.Key_Escape) {
          root.close()
          event.accepted = true
        } else if (event.key === Qt.Key_Up && currentIndex <= 0) {
          search.forceActiveFocus()
          event.accepted = true
        }
      }
    }

    Text {
      Layout.fillWidth: true
      visible: actionList.count === 0
      text: "No actions match your search."
      color: root.mutedColor
      font.family: root.fontFamily
      font.pixelSize: root.fontSize
      horizontalAlignment: Text.AlignHCenter
    }
  }
}
