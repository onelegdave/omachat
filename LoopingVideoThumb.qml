import QtQuick
import QtMultimedia
import qs.Commons

// Facebook sometimes transports an animated message as a short MP4/F4V
// rather than an image GIF. Keep those clips silent, inline, and looping.
Item {
  id: root
  objectName: "loopingVideoThumb"

  property string path: ""
  property bool playing: false
  property int maxEdge: Style.space(280)

  signal loadFailed()

  readonly property string source: path !== "" ? "file://" + path : ""
  readonly property bool hasError: player.error !== MediaPlayer.NoError
  readonly property bool loopsForever: player.loops === MediaPlayer.Infinite
  readonly property bool ready: player.mediaStatus === MediaPlayer.LoadedMedia
    || player.mediaStatus === MediaPlayer.BufferingMedia
    || player.mediaStatus === MediaPlayer.BufferedMedia

  implicitWidth: maxEdge
  implicitHeight: Math.max(Style.space(96), Math.round(implicitWidth * 9 / 16))
  width: implicitWidth
  height: implicitHeight

  function syncPlayback() {
    if (playing && ready) player.play()
    else player.pause()
  }

  onPlayingChanged: syncPlayback()
  onSourceChanged: syncPlayback()

  MediaPlayer {
    id: player
    source: root.source
    videoOutput: output
    loops: MediaPlayer.Infinite
    audioOutput: AudioOutput { muted: true }
    onMediaStatusChanged: root.syncPlayback()
    onErrorOccurred: root.loadFailed()
  }

  VideoOutput {
    id: output
    anchors.fill: parent
    fillMode: VideoOutput.PreserveAspectFit
  }
}
