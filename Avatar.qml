import QtQuick
import qs.Commons
import "Model.js" as Model

Rectangle {
  id: root

  property string imagePath: ""
  property string initials: "#"
  property string hexColor: ""
  property string seed: ""
  property color fallbackColor: Color.accent
  property string fontFamily: Style.font.family

  implicitWidth: Style.space(36)
  implicitHeight: Style.space(36)
  radius: width / 2
  color: Model.avatarColor(hexColor, seed, fallbackColor)
  clip: true

  Text {
    anchors.centerIn: parent
    visible: avatarImage.status !== Image.Ready
    text: root.initials
    color: Model.inkOn(root.color, Color.foreground, Color.background)
    font.family: root.fontFamily
    font.pixelSize: Math.round(root.height * 0.4)
    font.bold: true
  }

  Image {
    id: avatarImage
    anchors.fill: parent
    asynchronous: true
    cache: true
    fillMode: Image.PreserveAspectCrop
    visible: status === Image.Ready
    source: root.imagePath !== "" ? "file://" + root.imagePath : ""
  }
}
