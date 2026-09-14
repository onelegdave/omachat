import QtQuick
import qs.Commons
import qs.Ui

Item {
  id: root

  property string serviceId: ""
  property color foreground: Color.foreground
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
        meta: "Paper planes stay grounded for now. Google and WhatsApp answer on the other tabs."
      }
    default:
      return {
        title: "Dark frequency",
        glyph: "󰭹",
        meta: "On the map. Not on the air."
      }
    }
  }

  Column {
    anchors.centerIn: parent
    spacing: Style.space(12)
    width: Math.min(parent.width - Style.space(40), Style.space(420))

    PanelHero {
      width: parent.width
      title: root.copy.title
      meta: root.copy.meta
      foreground: root.foreground
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
      text: "Hit Google or WhatsApp when you actually want to talk to a human."
      color: root.foreground
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
    }
  }
}
