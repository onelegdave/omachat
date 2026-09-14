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
  property string remoteUrl: ""
  property bool playing: false
  property int maxEdge: Style.space(280)
  property bool clickable: path !== ""

  signal clicked()

  readonly property bool gif: Model.isGif(mimeType, fileName, path) || Model.isGif("", "", remoteUrl)
  readonly property string localSource: path !== "" ? "file://" + path : ""
  readonly property string source: localSource !== "" ? localSource : root.safeRemote
  readonly property string safeRemote: {
    var u = String(remoteUrl || "")
    if (u.indexOf("https://") !== 0) return ""
    if (u.indexOf("giphy.com") === -1) return ""
    return u
  }

  implicitWidth: Math.min(maxEdge, Math.max(1, loader.item ? loader.item.implicitWidth : maxEdge))
  implicitHeight: {
    var item = loader.item
    if (!item || item.implicitWidth <= 0) return Style.space(96)
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

  MouseArea {
    anchors.fill: parent
    enabled: root.clickable
    cursorShape: Qt.PointingHandCursor
    onClicked: root.clicked()
  }

  Component {
    id: stillComp
    Image {
      asynchronous: true
      smooth: true
      fillMode: Image.PreserveAspectFit
      source: root.source
    }
  }

  Component {
    id: gifComp
    AnimatedImage {
      asynchronous: true
      cache: false
      fillMode: Image.PreserveAspectFit
      playing: root.playing && status === AnimatedImage.Ready
      source: root.source
    }
  }
}
