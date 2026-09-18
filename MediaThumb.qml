import QtQuick
import qs.Commons
import "Model.js" as Model

// Still image or looping GIF. GIFs play only while `playing` is true so a
// closed panel does not keep decoding in the background.
Item {
  id: root

  property string path: ""
  property string mimeType: ""
  property string fileName: ""
  property bool playing: false
  property int maxEdge: Style.space(280)
  property bool clickable: path !== ""

  signal clicked()
  signal loadFailed()

  readonly property bool gif: Model.isGif(mimeType, fileName, path)
  readonly property string source: path !== "" ? "file://" + path : ""

  readonly property bool hasItemSize: loader.item && loader.item.implicitWidth > 0 && loader.item.implicitHeight > 0
  readonly property bool hasError: !!(loader.item && loader.item.status === (root.gif ? AnimatedImage.Error : Image.Error))
  readonly property bool ready: !!(loader.item && loader.item.status === (root.gif ? AnimatedImage.Ready : Image.Ready))

  implicitWidth: Math.min(maxEdge, Math.max(Style.space(96), hasItemSize ? loader.item.implicitWidth : Style.space(96)))
  implicitHeight: {
    var item = loader.item
    if (!hasItemSize) return Style.space(96)
    var h = item.implicitHeight * (implicitWidth / item.implicitWidth)
    return Math.min(maxEdge, Math.max(1, h))
  }
  width: implicitWidth
  height: implicitHeight

  Loader {
    id: loader
    anchors.fill: parent
    sourceComponent: root.gif ? gifComp : stillComp
  }

  Rectangle {
    id: errorSurface
    anchors.fill: parent
    visible: root.hasError
    color: Style.normalFillFor(Color.background, Color.accent)
    border.width: 1
    border.color: Color.popups.border
    radius: Style.space(6)
    Text {
      anchors.centerIn: parent
      text: "Media error"
      color: Model.readableInk(errorSurface.color, Color.popups.text)
      font.pixelSize: Style.font.caption
    }
  }

  MouseArea {
    anchors.fill: parent
    enabled: root.clickable
    cursorShape: Qt.PointingHandCursor
    onClicked: {
      if (root.hasError) root.loadFailed()
      else root.clicked()
    }
  }

  Component {
    id: stillComp
    Image {
      anchors.fill: parent
      asynchronous: true
      // Keep the thumbnail visible while a full-resolution replacement decodes.
      retainWhileLoading: true
      smooth: true
      fillMode: Image.PreserveAspectFit
      source: root.source
      onStatusChanged: if (status === Image.Error) root.loadFailed()
    }
  }

  Component {
    id: gifComp
    AnimatedImage {
      anchors.fill: parent
      asynchronous: true
      cache: false
      fillMode: Image.PreserveAspectFit
      playing: root.playing && status === AnimatedImage.Ready
      source: root.source
      onStatusChanged: if (status === AnimatedImage.Error) root.loadFailed()
    }
  }
}
