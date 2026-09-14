import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Item {
  id: root

  property var service: null
  property string network: "gmessages"
  property color foreground: Color.foreground
  property string fontFamily: Style.font.family

  readonly property color dim: Color.muted
  readonly property bool isWhatsApp: network === "whatsapp"
  readonly property var status: service ? (typeof service.statusFor === "function" ? service.statusFor(network) : service.status) : ({})
  readonly property string connState: service ? (typeof service.stateFor === "function" ? service.stateFor(network) : (service.state || "")) : ""
  readonly property bool isGaia: !isWhatsApp && connState === "gaiaPairing"
  readonly property bool isQR: connState === "pairing"
  readonly property bool isError: connState === "error"
  readonly property string unpairWarning: connState === "unpaired" && status && status.error ? status.error : ""
  readonly property string emoji: status && status.emoji ? status.emoji : ""
  readonly property var profiles: service ? (service.browserProfiles || []) : []
  property bool profilePickerOpen: false
  property int qrRevision: 0

  readonly property string qrPath: {
    var cache = Quickshell.env("XDG_CACHE_HOME")
    var base = cache && cache.length > 0 ? cache : Quickshell.env("HOME") + "/.cache"
    return base + "/omachat/pairing-qr.png"
  }

  function renderQR(url) {
    if (!url) return
    qrProc.running = false
    qrProc.command = ["qrencode", "-o", root.qrPath, "-s", "6", "-m", "2", "--", url]
    qrProc.running = true
  }

  Connections {
    target: root.service
    function onStatusChanged() {
      if (root.status && root.status.qrURL) root.renderQR(root.status.qrURL)
    }
  }

  Process {
    id: qrProc
    onExited: root.qrRevision++
  }

  Column {
    anchors.centerIn: parent
    spacing: Style.space(12)
    width: Math.min(parent.width - Style.space(40), Style.space(460))

    PanelHero {
      objectName: "pairingHero"
      width: parent.width
      title: {
        if (root.isWhatsApp) {
          if (root.isQR) return "Scan with WhatsApp on your phone"
          if (root.unpairWarning !== "") return "Attention required"
          if (root.isError) return root.status && root.status.error ? root.status.error : "Pairing failed"
          return "Link with WhatsApp"
        }
        if (root.isGaia) return "Tap this emoji on your phone"
        if (root.isQR) return "Use the Messages pairing scanner"
        if (root.unpairWarning !== "") return "Attention required"
        if (root.isError) return root.status && root.status.error ? root.status.error : "Pairing failed"
        return "Handshake required"
      }
      meta: {
        if (root.isWhatsApp) {
          if (root.isQR) return "Open WhatsApp on your phone, tap Menu or Settings, select Linked devices, then Link a device and scan this QR code."
          if (root.unpairWarning !== "") return ""
          if (root.isError) return root.status && root.status.hint ? root.status.hint : ""
          return "Link this desktop to your WhatsApp account using a QR code. Your phone must stay connected to the internet."
        }
        if (root.isGaia) {
          return root.emoji !== ""
            ? "Google Messages on your phone is showing several emoji. Tap the matching one. It expires in about a minute."
            : "Signing in to your Google account"
        }
        if (root.isQR) return "Open Google Messages, menu, Device pairing, then QR code scanner. The phone camera and Google Lens open a help page and will not pair. Newer Messages builds have no scanner; use Pair with Google instead."
        if (root.unpairWarning !== "") return ""
        if (root.isError) return root.status && root.status.hint ? root.status.hint : ""
        return "Pair this desktop with the Google account already signed in to Messages in your browser. Your phone must stay online. Do not scan a QR with the camera app."
      }
      foreground: root.isError || root.unpairWarning !== "" ? Color.urgent : root.foreground
      fontFamily: root.fontFamily
      iconComponent: Component {
        OpticalGlyph {
          implicitWidth: Style.font.display
          implicitHeight: Style.font.display
          text: root.isWhatsApp ? "󰖣" : "󰭹"
          color: Color.accent
          fontFamily: root.fontFamily
          fontSize: Style.font.display
        }
      }
    }

    Text {
      objectName: "unpairWarningText"
      width: parent.width
      visible: root.unpairWarning !== ""
      text: root.unpairWarning
      textFormat: Text.PlainText
      wrapMode: Text.WordWrap
      color: Color.urgent
      font.family: root.fontFamily
      font.pixelSize: Style.font.body
    }

    Text {
      anchors.horizontalCenter: parent.horizontalCenter
      visible: root.isGaia && root.emoji !== ""
      text: root.emoji
      font.pixelSize: Style.space(72)
      font.family: root.fontFamily
    }

    Rectangle {
      visible: root.isQR
      anchors.horizontalCenter: parent.horizontalCenter
      width: Style.space(200)
      height: width
      radius: Style.space(6)
      color: "#ffffff"

      Image {
        anchors.centerIn: parent
        width: parent.width - Style.space(12)
        height: width
        asynchronous: true
        cache: false
        fillMode: Image.PreserveAspectFit
        source: root.qrRevision > 0 ? "file://" + root.qrPath + "?v=" + root.qrRevision : ""
      }
    }

    Row {
      anchors.horizontalCenter: parent.horizontalCenter
      spacing: Style.space(8)
      visible: !root.isGaia && !root.isWhatsApp

      Button {
        text: root.isQR ? "Pair with Google instead" : (root.isError ? "Try again" : "Pair with Google")
        foreground: root.foreground
        fontFamily: root.fontFamily
        bordered: true
        onClicked: if (root.service) root.service.call("pairFromBrowser", null, null, "gmessages")
      }

      Button {
        visible: !root.isQR
        text: "Use a QR code"
        foreground: root.dim
        fontFamily: root.fontFamily
        onClicked: if (root.service) root.service.call("startPairing", null, null, "gmessages")
      }
    }

    Row {
      anchors.horizontalCenter: parent.horizontalCenter
      spacing: Style.space(8)
      visible: root.isWhatsApp && !root.isQR

      Button {
        text: root.isError ? "Try again" : "Use a QR code"
        foreground: root.foreground
        fontFamily: root.fontFamily
        bordered: true
        onClicked: if (root.service) root.service.call("startPairing", null, null, "whatsapp")
      }
    }

    Column {
      width: parent.width
      spacing: Style.space(4)
      visible: !root.isGaia && !root.isWhatsApp

      Row {
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: Style.space(6)

        Text {
          anchors.verticalCenter: parent.verticalCenter
          text: {
            for (var i = 0; i < root.profiles.length; i++) {
              if (root.profiles[i].selected) return "Using " + root.profiles[i].name
            }
            return root.status && root.status.profile
              ? "Using " + root.status.profile + " (automatic)"
              : "Browser profile chosen automatically"
          }
          color: root.dim
          font.family: root.fontFamily
          font.pixelSize: Style.font.caption
        }

        Button {
          anchors.verticalCenter: parent.verticalCenter
          visible: root.profiles.length > 1
          text: root.profilePickerOpen ? "Hide" : "Change"
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: {
            root.profilePickerOpen = !root.profilePickerOpen
            if (root.profilePickerOpen && root.service) root.service.loadProfiles()
          }
        }
      }

      Repeater {
        model: root.profilePickerOpen ? root.profiles : []
        delegate: Rectangle {
          required property var modelData
          width: parent.width
          height: Style.space(38)
          radius: Style.space(6)
          color: modelData.selected
            ? Style.selectedFillFor(root.foreground, Color.accent)
            : (profileHover.containsMouse ? Style.hoverFillFor(root.foreground, Color.accent) : "transparent")
          opacity: modelData.usable ? 1.0 : 0.65

          MouseArea {
            id: profileHover
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: if (root.service) root.service.call("setProfile", { name: modelData.name }, function(ok, res) {
              if (ok && res) root.service.browserProfiles = res
              root.profilePickerOpen = false
            }, "gmessages")
          }

          Text {
            anchors.left: parent.left
            anchors.leftMargin: Style.space(8)
            anchors.top: parent.top
            anchors.topMargin: Style.space(5)
            text: (modelData.usable ? "✓  " : "•  ") + modelData.name
            color: root.foreground
            font.family: root.fontFamily
            font.pixelSize: Style.font.bodySmall
            font.bold: modelData.selected === true
          }

          Text {
            anchors.left: parent.left
            anchors.leftMargin: Style.space(8)
            anchors.right: parent.right
            anchors.rightMargin: Style.space(8)
            anchors.bottom: parent.bottom
            anchors.bottomMargin: Style.space(5)
            elide: Text.ElideRight
            text: modelData.usable
              ? modelData.cookies + " cookies, ready"
              : (modelData.reason || "unusable")
            color: modelData.usable ? root.dim : Color.urgent
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }
        }
      }
    }
  }
}
