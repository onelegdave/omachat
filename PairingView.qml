import QtQuick
import QtQuick.Controls as Controls
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

Item {
  id: root

  property var service: null
  property string network: "gmessages"
  property string fontFamily: Style.font.family
  property real uiScale: 1.0
  signal openSettingsRequested()
  function fs(n) { return Math.max(8, Math.round(Number(n) * uiScale)) }

  function readableInk(surface, preferred, minRatio) {
    return Model.readableInk(surface, preferred, minRatio)
  }

  readonly property color popupBg: Color.popups.background
  property color foreground: readableInk(popupBg, Color.popups.text, 7)
  readonly property color dim: readableInk(popupBg, Color.muted, 7)
  readonly property color accentInk: readableInk(popupBg, Color.accent, 4.5)
  readonly property color urgentInk: readableInk(popupBg, Color.urgent, 4.5)

  readonly property bool isWhatsApp: network === "whatsapp"
  readonly property bool isTelegram: network === "telegram"
  readonly property var status: service ? (typeof service.statusFor === "function" ? service.statusFor(network) : service.status) : ({})
  readonly property string connState: service ? (typeof service.stateFor === "function" ? service.stateFor(network) : (service.state || "")) : ""
  readonly property bool isGaia: !isWhatsApp && connState === "gaiaPairing"
  readonly property bool isQR: connState === "pairing"
  readonly property bool isError: connState === "error"
  readonly property string unpairWarning: connState === "unpaired" && status && status.error && status.error !== status.hint ? status.error : ""
  readonly property string emoji: status && status.emoji ? status.emoji : ""
  readonly property var profiles: service ? (service.browserProfiles || []) : []
  property bool profilePickerOpen: false
  property int qrRevision: 0
  property string qrError: ""

  readonly property string qrPath: {
    var cache = Quickshell.env("XDG_CACHE_HOME")
    var base = cache && cache.length > 0 ? cache : Quickshell.env("HOME") + "/.cache"
    return base + "/omachat/pairing-qr.png"
  }

  function renderQR(url) {
    if (!url) return
    qrError = ""
    qrProc.running = false
    qrProc.command = ["qrencode", "-o", root.qrPath, "-s", "6", "-m", "2", "--", url]
    qrProc.running = true
    qrTimeout.restart()
  }

  function syncQR() {
    var current = root.service
      ? (typeof root.service.statusFor === "function" ? root.service.statusFor(root.network) : root.service.status)
      : null
    if (current && current.qrURL) root.renderQR(current.qrURL)
  }

  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onStatusChanged() {
      root.syncQR()
    }
    function onStatusWAChanged() {
      root.syncQR()
    }
    function onStatusTGChanged() {
      root.syncQR()
    }
  }

  Component.onCompleted: root.syncQR()

  Process {
    id: qrProc
    objectName: "qrProcess"
    onExited: function(code) {
      qrTimeout.stop()
      if (code === 0) root.qrRevision++
      else root.qrError = "Could not draw the pairing QR code. Check qrencode in Settings > Tools, then retry."
    }
  }

  Timer {
    id: qrTimeout
    interval: 5000
    onTriggered: {
      qrProc.running = false
      root.qrError = "QR generation did not finish. Check qrencode in Settings > Tools, then retry."
    }
  }

  Flickable {
    id: scroller
    anchors.fill: parent
    clip: true
    boundsBehavior: Flickable.StopAtBounds
    contentWidth: width
    Controls.ScrollBar.vertical: Controls.ScrollBar { policy: Controls.ScrollBar.AsNeeded }
    contentHeight: Math.max(height, centerCol.implicitHeight + Style.space(32))

    Column {
      id: centerCol
      width: Math.min(parent.width - Style.space(40), Style.space(480))
      anchors.horizontalCenter: parent.horizontalCenter
      y: Math.max(Style.space(16), Math.round((scroller.height - implicitHeight) / 2))
      spacing: Style.space(14)

      ReadableHero {
        id: pairingHero
        objectName: "pairingHero"
        width: parent.width
        uiScale: root.uiScale
        title: {
          if (root.isTelegram) {
            if (root.isQR) return "Scan QR code with Telegram"
            if (root.isError) return root.status && root.status.error ? root.status.error : "Pairing failed"
            if (root.status && root.status.state === "connected") return "Telegram connected"
            return "Pair with Telegram"
          }
          if (root.isWhatsApp) {
            if (root.isQR) return "Scan QR code with WhatsApp"
            if (root.unpairWarning !== "") return "Device pairing notice"
            if (root.isError) return root.status && root.status.error ? root.status.error : "Pairing failed"
            return "Pair with WhatsApp"
          }
          if (root.isGaia) return "Confirm emoji on your phone"
          if (root.isQR) return "Scan QR code with Google Messages"
          if (root.unpairWarning !== "") return "Device pairing notice"
          if (root.isError) return root.status && root.status.error ? root.status.error : "Pairing failed"
          return "Pair with Google Messages"
        }
        meta: {
          if (root.isTelegram) {
            if (root.isQR) return "Open Telegram on your phone, go to Settings > Devices > Link Desktop Device, then scan this QR code."
            if (root.isError) return root.status && root.status.hint ? root.status.hint : "Check your connection and credentials, then try again."
            if (root.status && root.status.state === "connected") return "Telegram is connected and ready. Chats, media, and voice notes are available."
            return root.status && root.status.hint ? root.status.hint : "Configure your Telegram API credentials in Settings before pairing."
          }
          if (root.isWhatsApp) {
            if (root.isQR) return "Open WhatsApp on your phone, tap Menu or Settings, select Linked devices, then Link a device and scan this QR code."
            if (root.unpairWarning !== "") return ""
            if (root.isError) return root.status && root.status.hint ? root.status.hint : "Check your internet connection on both desktop and phone, then try again."
            return "Link this desktop to your WhatsApp account using a QR code. Your phone must stay connected to the internet."
          }
          if (root.isGaia) {
            return root.emoji !== ""
              ? "Google Messages on your phone is displaying a matching emoji. Tap the matching emoji on your phone to complete pairing."
              : "Signing in to your Google account..."
          }
          if (root.isQR) return "Open Google Messages on your phone, tap your account menu > Device pairing > QR code scanner, and scan this QR code. Standard camera apps and Google Lens open a help page and will not pair. If your app lacks a scanner, use Pair with Google instead."
          if (root.unpairWarning !== "") return ""
          if (root.isError) return root.status && root.status.hint ? root.status.hint : "Check browser cookies and keyring access, then try again."
          return "Sign in to Messages for web (messages.google.com/web) in your Chromium-family browser first. Ensure your desktop keyring is unlocked, then choose Pair with Google. Your phone must stay online."
        }
        foreground: root.isError || root.unpairWarning !== "" ? root.urgentInk : root.foreground
        metaColor: root.isError || root.unpairWarning !== "" ? root.urgentInk : root.dim
        fontFamily: root.fontFamily
        iconComponent: Component {
          OpticalGlyph {
            implicitWidth: root.fs(Style.font.display)
            implicitHeight: root.fs(Style.font.display)
            text: root.isTelegram ? "\uf2c6" : (root.isWhatsApp ? "󰖣" : "󰭹")
            color: root.accentInk
            fontFamily: root.fontFamily
            fontSize: root.fs(Style.font.display)
          }
        }
      }

      Text {
        id: unpairWarningText
        objectName: "unpairWarningText"
        width: parent.width
        visible: root.unpairWarning !== ""
        text: root.unpairWarning
        textFormat: Text.PlainText
        wrapMode: Text.Wrap
        color: root.urgentInk
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.body)
        lineHeight: 1.25
      }

      Text {
        objectName: "qrErrorText"
        width: parent.width
        visible: root.qrError !== "" && root.isQR
        text: root.qrError
        wrapMode: Text.Wrap
        color: root.urgentInk
        font.family: root.fontFamily
        font.pixelSize: root.fs(Style.font.body)
      }

      Button {
        visible: root.qrError !== "" && root.isQR
        anchors.horizontalCenter: parent.horizontalCenter
        text: "Retry QR code"
        foreground: root.foreground
        bordered: true
        onClicked: root.syncQR()
      }

      Text {
        anchors.horizontalCenter: parent.horizontalCenter
        visible: root.isGaia && root.emoji !== ""
        text: root.emoji
        font.pixelSize: root.fs(Style.space(72))
        font.family: root.fontFamily
      }

      Rectangle {
        visible: root.isQR && (root.isWhatsApp || root.isTelegram || !root.isGaia)
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
          source: root.qrRevision > 0 && root.qrError === "" ? "file://" + root.qrPath + "?v=" + root.qrRevision : ""
        }
      }

      Row {
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: Style.space(8)
        visible: !root.isGaia && !root.isWhatsApp && !root.isTelegram

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

      Row {
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: Style.space(8)
        visible: root.isTelegram && root.status && root.status.state !== "connected"

        Button {
          text: root.isQR ? "Pairing..." : (root.isError ? "Try again" : "Pair with Telegram")
          foreground: root.foreground
          fontFamily: root.fontFamily
          bordered: true
          enabled: !root.isQR
          onClicked: if (root.service) root.service.call("startPairing", null, null, "telegram")
        }

        Button {
          visible: !root.isQR
          text: "Telegram settings"
          foreground: root.dim
          fontFamily: root.fontFamily
          onClicked: root.openSettingsRequested()
        }
      }

      Column {
        width: parent.width
        spacing: Style.space(6)
        visible: !root.isGaia && !root.isWhatsApp && !root.isTelegram

        Row {
          anchors.horizontalCenter: parent.horizontalCenter
          spacing: Style.space(8)

          Text {
            anchors.verticalCenter: parent.verticalCenter
            text: {
              for (var i = 0; i < root.profiles.length; i++) {
                if (root.profiles[i].selected) return "Browser profile: " + root.profiles[i].name
              }
              return root.status && root.status.profile
                ? "Browser profile: " + root.status.profile + " (automatic)"
                : "Browser profile chosen automatically"
            }
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: root.fs(Style.font.caption)
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
            id: profileCard
            required property var modelData
            width: parent.width
            height: profileName.implicitHeight + profileReason.implicitHeight + Style.space(18)
            radius: Style.space(6)
            color: modelData.selected
              ? Style.selectedFillFor(root.foreground, Color.accent)
              : (profileHover.containsMouse ? Style.hoverFillFor(root.foreground, Color.accent) : "transparent")

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
              id: profileName
              anchors.left: parent.left
              anchors.leftMargin: Style.space(10)
              anchors.right: parent.right
              anchors.rightMargin: Style.space(10)
              wrapMode: Text.Wrap
              anchors.top: parent.top
              anchors.topMargin: Style.space(5)
              text: (modelData.usable ? "✓  " : "•  ") + modelData.name
              color: Model.readableInk(Model.mix(root.popupBg, profileCard.color, profileCard.color.a), root.foreground)
              font.family: root.fontFamily
              font.pixelSize: root.fs(Style.font.bodySmall)
              font.bold: modelData.selected === true
            }

            Text {
              id: profileReason
              anchors.left: parent.left
              anchors.leftMargin: Style.space(10)
              anchors.right: parent.right
              anchors.rightMargin: Style.space(10)
              anchors.top: profileName.bottom
              anchors.topMargin: Style.space(4)
              wrapMode: Text.Wrap
              text: modelData.usable
                ? modelData.cookies + " cookies, ready"
                : (modelData.reason || "unusable")
              color: Model.readableInk(Model.mix(root.popupBg, profileCard.color, profileCard.color.a), modelData.usable ? root.dim : root.urgentInk)
              font.family: root.fontFamily
              font.pixelSize: root.fs(Style.font.caption)
            }
          }
        }
      }
    }
  }
}
