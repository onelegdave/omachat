import QtQuick
import Quickshell
import qs.Commons
import qs.Ui

Flickable {
  id: root

  property var service: null
  property color foreground: Color.foreground
  property string fontFamily: Style.font.family
  property real uiScale: 1
  signal scaleSaved(real scale)
  function fs(n) { return Math.max(8, Math.round(Number(n) * uiScale)) }
  readonly property color copyColor: Color.foreground
  readonly property string siteUrl: "https://www.onelegdave.dev/"
  readonly property string repoUrl: "https://github.com/onelegdave/omachat"
  readonly property string issuesUrl: "https://github.com/onelegdave/omachat/issues"

  property bool giphyKeySet: false
  property string keyDraft: ""
  property string statusText: ""
  property bool saving: false

  clip: true
  boundsBehavior: Flickable.StopAtBounds
  contentWidth: width
  contentHeight: col.implicitHeight + Style.space(16)

  function load() {
    if (!service) return
    service.call("config", null, function(ok, res) {
      if (!ok || !res) return
      root.giphyKeySet = res.giphyKeySet === true
      var s = Number(res.uiScale)
      if (isFinite(s) && s > 0) root.uiScale = s
    }, "gmessages")
  }

  function saveKey() {
    var k = keyDraft.trim()
    if (!k || !service) return
    saving = true
    statusText = ""
    service.call("setGiphyKey", { key: k }, function(ok, res) {
      root.saving = false
      if (!ok) {
        root.statusText = String(res)
        return
      }
      root.giphyKeySet = res && res.giphyKeySet === true
      root.keyDraft = ""
      root.statusText = "Key locked in. Try not to paste it in a group chat."
    }, "gmessages")
  }

  function clearKey() {
    if (!service) return
    saving = true
    service.call("setGiphyKey", { key: "" }, function(ok, res) {
      root.saving = false
      if (!ok) {
        root.statusText = String(res)
        return
      }
      root.giphyKeySet = false
      root.keyDraft = ""
      root.statusText = "Key vaporized."
    }, "gmessages")
  }

  function openUrl(url) {
    Util.execArgv(["xdg-open", url])
  }

  Component.onCompleted: load()
  onServiceChanged: load()

  Column {
    id: col
    width: root.width
    spacing: Style.space(14)

    Text {
      text: "The lab"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.heading)
      font.bold: true
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Knobs that actually do something. Tabs up top still dump you back to a network if you get lost in here."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Type size"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }

    ButtonGroup {
      width: parent.width
      options: [
        { value: "0.85", label: "Tiny" },
        { value: "1", label: "Sane" },
        { value: "1.15", label: "Loud" },
        { value: "1.3", label: "Billboard" }
      ]
      value: {
        var s = Number(root.uiScale).toFixed(2)
        if (s === "0.85") return "0.85"
        if (s === "1.15") return "1.15"
        if (s === "1.30" || s === "1.3") return "1.3"
        return "1"
      }
      foreground: root.foreground
      background: Color.popups.background
      accent: Color.accent
      fontFamily: root.fontFamily
      fontSize: fs(Style.font.body)
      focusable: false
      onChanged: function(v) {
        var n = Number(v)
        if (!isFinite(n) || !root.service) return
        root.service.call("setUiScale", { scale: n }, function(ok, res) {
          if (!ok) {
            root.statusText = String(res)
            return
          }
          var s = res && res.uiScale ? Number(res.uiScale) : n
          root.uiScale = s
          root.scaleSaved(s)
        }, "gmessages")
      }
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "GIPHY (optional, chaotic)"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: root.giphyKeySet
        ? "A key is already living in ~/.local/share/omachat. Paste a new one to rotate it."
        : "Search is locked until you bring your own GIPHY key. You can still pick a GIF to send. This is not Omarchy Setup. This is the only place the key goes."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Column {
      width: parent.width
      spacing: Style.space(6)

      Text {
        width: parent.width
        wrapMode: Text.WordWrap
        text: "1. Hit Open GIPHY and make a free account."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
      }
      Text {
        width: parent.width
        wrapMode: Text.WordWrap
        text: "2. Create an app. Name it after your cat. GIPHY does not care."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
      }
      Text {
        width: parent.width
        wrapMode: Text.WordWrap
        text: "3. Copy the API key, paste it below, Save. Do not commit it. Do not text it to yourself on a group thread."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
      }
    }

    Button {
      text: "Open GIPHY"
      bordered: true
      foreground: root.foreground
      fontFamily: root.fontFamily
      onClicked: root.openUrl("https://developers.giphy.com/dashboard/")
    }

    Row {
      spacing: Style.space(6)
      width: parent.width
      TextField {
        id: keyField
        objectName: "keyField"
        width: parent.width - saveBtn.implicitWidth - Style.space(6)
        placeholderText: root.giphyKeySet ? "Replacement key" : "Paste API key"
        foreground: root.foreground
        password: true
        onAccepted: root.saveKey()
        onTextChanged: root.keyDraft = text
      }
      Button {
        id: saveBtn
        text: root.saving ? "Saving" : "Save"
        bordered: true
        enabled: !root.saving && root.keyDraft.trim() !== ""
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.saveKey()
      }
    }

    Button {
      visible: root.giphyKeySet
      text: "Delete key"
      foreground: root.copyColor
      fontFamily: root.fontFamily
      onClicked: root.clearKey()
    }

    Text {
      width: parent.width
      visible: root.statusText !== ""
      wrapMode: Text.WordWrap
      text: root.statusText
      color: root.statusText.indexOf("fail") >= 0 || root.statusText.indexOf("error") >= 0
        ? Color.urgent : Color.accent
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    Text {
      text: "Field manual"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.heading)
      font.bold: true
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "The panel says Go is required."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "All services share a helper that needs Go and a C compiler (gcc or clang) to build. If you want to use OmaChat, install missing tools yourself, then press Build helper. For example: omarchy pkg add go gcc. Build helper only compiles vendored source; OmaChat never installs dependencies."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Pairing does nothing."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "For Google Messages, open messages.google.com/web in your selected Chromium-family browser first. Browser pairing needs sqlite3, secret-tool (libsecret), and an unlocked desktop keyring. Install missing tools yourself only if you choose to use this service."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "The QR opened a help page."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "That is Google being Google. Use Pair with Google. Camera apps and Lens will never pair this desktop."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Voice is dead."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Google Messages and Telegram voice notes need ffmpeg and ffplay. If you want voice notes, you can install them yourself with omarchy pkg add ffmpeg. Text and photos work without them. WhatsApp voice notes are unavailable. OmaChat never installs dependencies."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "WhatsApp and Telegram tabs."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "QR pairing needs qrencode, which you can install yourself if you want to link a service. WhatsApp links through Linked devices on your phone. For Telegram, create your own API credentials at my.telegram.org, run python3 scripts/configure-telegram.py from the plugin folder, then restart the shell before pairing. The script needs Python 3. Each service has its own session, cache, and drafts. OmaChat never installs tools or creates accounts for you."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    Text {
      text: "About"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.heading)
      font.bold: true
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "OmaChat is a Native Omarchy Plugin for Omarchy. It uses QML for the panel and one shell-owned helper process for service connections. No WebEngine and no systemd unit are required."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Author"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "OneLegDave. I ship it. I break it. I patch it."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }
    Row {
      spacing: Style.space(8)
      Button {
        text: "onelegdave.dev"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl(root.siteUrl)
      }
      Button {
        text: "OmaDroid"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://github.com/onelegdave/omadroid")
      }
      Button {
        text: "QuikView"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://github.com/onelegdave/system-quikview")
      }
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Credits"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Protocol guts: mautrix libgm (go.mau.fi/mautrix-gmessages). Helper shape adapted from Marc Ford's gmessages-omarchy-plugin (MIT). Desktop kit: Omarchy and Quickshell. GIF search, when you opt in: GIPHY."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Row {
      spacing: Style.space(8)
      Button {
        text: "mautrix"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://github.com/mautrix/gmessages")
      }
      Button {
        text: "Marc Ford"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://github.com/MarcFord/gmessages-omarchy-plugin")
      }
      Button {
        text: "Omarchy"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://omarchy.org/")
      }
    }

    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "Bugs, patches, unhinged ideas"
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
      font.bold: true
    }
    Text {
      width: parent.width
      wrapMode: Text.WordWrap
      text: "File an issue or a PR on GitHub. Include Omarchy version, what you clicked, and whether the helper was actually running. Screenshots of dead air welcome."
      color: root.copyColor
      font.family: root.fontFamily
      font.pixelSize: fs(Style.font.body)
    }

    Row {
      spacing: Style.space(8)
      Button {
        text: "File a bug"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl(root.issuesUrl)
      }
      Button {
        text: "Open the repo"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl(root.repoUrl)
      }
    }
  }
}
