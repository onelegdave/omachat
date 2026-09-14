import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root

  property Component iconComponent: null
  property string title: ""
  property string meta: ""
  property string detail: ""
  property color foreground: Color.foreground
  property color metaColor: Color.muted
  property string fontFamily: Style.font.family
  property real iconSize: Style.font.display
  property real iconOpacity: 1.0
  property real metaOpacity: 1.0
  property Component trailingControl: null
  property real uiScale: 1.0
  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }

  readonly property real trailingInset: trailingLoader.item && trailingLoader.item.visible ? trailingLoader.width + Style.space(12) : 0

  width: parent ? parent.width : implicitWidth
  implicitWidth: Style.space(360)
  implicitHeight: Math.max(
    iconLoader.implicitHeight,
    heroLabels.implicitHeight,
    trailingLoader.implicitHeight
  )

  Loader {
    id: iconLoader
    sourceComponent: root.iconComponent
    anchors.left: parent.left
    anchors.top: parent.top
    anchors.topMargin: Style.space(2)
    opacity: root.iconOpacity
  }

  Column {
    id: heroLabels
    anchors.left: iconLoader.item ? iconLoader.right : parent.left
    anchors.leftMargin: iconLoader.item ? Style.space(14) : 0
    anchors.right: parent.right
    anchors.rightMargin: root.trailingInset
    y: Math.max(0, Math.round((root.implicitHeight - implicitHeight) / 2))
    spacing: Style.space(6)

    Row {
      id: titleRow
      visible: root.title !== "" || detailPill.visible
      width: parent.width
      spacing: Style.space(8)

      Text {
        textFormat: Text.PlainText
        visible: root.title !== ""
        text: root.title
        width: Math.min(implicitWidth, Math.max(0, parent.width - (detailPill.visible ? detailPill.implicitWidth + Style.space(8) : 0)))
        wrapMode: Text.Wrap
        color: root.foreground
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.title)
        font.bold: true
      }

      BorderSurface {
        id: detailPill
        visible: root.detail !== ""
        implicitWidth: detailText.implicitWidth + Style.space(10)
        implicitHeight: detailText.implicitHeight + Style.space(4)
        anchors.verticalCenter: parent.verticalCenter
        color: "transparent"
        borderSpec: Border.controlSpec("normal", root.foreground, Color.accent)
        radius: Style.cornerRadius

        Text {
          id: detailText
          textFormat: Text.PlainText
          anchors.centerIn: parent
          text: root.detail
          color: root.metaColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.bodySmall)
          font.bold: true
        }
      }
    }

    Text {
      id: metaText
      textFormat: Text.PlainText
      width: parent.width
      text: root.meta
      visible: text !== ""
      color: root.metaColor
      opacity: root.metaOpacity
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      wrapMode: Text.Wrap
      lineHeight: 1.25
    }
  }

  Loader {
    id: trailingLoader
    sourceComponent: root.trailingControl
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
  }
}
