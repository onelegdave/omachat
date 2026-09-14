import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root

  property string serviceId: ""
  property color foreground: Model.readableInk(Color.popups.background, Color.popups.text, 7)
  property string fontFamily: Style.font.family

  readonly property var copy: {
    switch (serviceId) {
    case "whatsapp":
      return {
        title: "WhatsApp",
        glyph: "󰖣",
        meta: "WhatsApp lives on the WhatsApp tab after you link a device with a QR code."
      }
    case "telegram":
      return {
        title: "Telegram",
        glyph: "\uf2c6",
        meta: "Telegram is available after API credential setup and QR pairing."
      }
    default:
      return {
        title: "Service unavailable",
        glyph: "󰭹",
        meta: "Choose an available service from the tabs above."
      }
    }
  }

  Column {
    anchors.centerIn: parent
    spacing: Style.space(12)
    width: Math.min(parent.width - Style.space(40), Style.space(420))

    ReadableHero {
      width: parent.width
      title: root.copy.title
      meta: root.copy.meta
      foreground: root.foreground
      metaColor: Model.readableInk(Color.popups.background, Color.muted)
      fontFamily: root.fontFamily
      iconOpacity: 0.7
      iconComponent: Component {
        OpticalGlyph {
          implicitWidth: Style.font.display
          implicitHeight: Style.font.display
          text: root.copy.glyph
          color: Color.accent
          fontFamily: root.fontFamily
          fontSize: Style.font.display
        }
      }
    }

    Text {
      width: parent.width
      horizontalAlignment: Text.AlignHCenter
      wrapMode: Text.WordWrap
      text: "Google Messages, WhatsApp, and Telegram have separate pairing and account settings."
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
    }
  }
}
