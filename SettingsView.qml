import QtQuick
import QtQuick.Controls as Controls
import Quickshell
import qs.Commons
import qs.Ui
import "Model.js" as Model

Flickable {
  id: root

  property var service: null
  property string fontFamily: Style.font.family
  property real uiScale: 1
  signal scaleSaved(real scale)
  function fs(n) { return Math.max(12, Math.round(Number(n) * uiScale)) }

  function readableInk(surface, preferred, minRatio) {
    return Model.readableInk(surface, preferred, minRatio)
  }

  readonly property color popupBg: Color.popups.background
  property color foreground: readableInk(popupBg, Color.popups.text, 7)
  readonly property color copyColor: readableInk(popupBg, Color.popups.text, 7)
  readonly property color mutedColor: readableInk(popupBg, Color.muted, 7)
  readonly property color accentColor: readableInk(popupBg, Color.accent, 4.5)
  readonly property color urgentColor: readableInk(popupBg, Color.urgent, 4.5)

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
  contentHeight: col.implicitHeight + Style.space(32)
  Controls.ScrollBar.vertical: Controls.ScrollBar { policy: Controls.ScrollBar.AsNeeded }

  function jumpTo(section) {
    contentY = Math.max(0, Math.min(section.y, contentHeight - height))
  }

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
      root.statusText = "GIPHY API key saved."
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
      root.statusText = "GIPHY API key removed."
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
    spacing: Style.space(16)

    // Section 1: Header
    Column {
      width: parent.width
      spacing: Style.space(4)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Settings"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Preferences, dependency guidance, service documentation, and upstream attribution. Use the service tabs above to return to your conversations."
        color: root.mutedColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    Flow {
      width: parent.width
      spacing: Style.space(8)
      Repeater {
        model: [
          {label:"Services", section:serviceChoices},
          {label:"Service guides", section:servicesSection},
          {label:"Tools", section:toolsSection},
          {label:"GIF search", section:gifSection},
          {label:"Credits", section:creditsSection},
          {label:"About", section:aboutSection}
        ]
        Button {
          required property var modelData
          text: modelData.label
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.jumpTo(modelData.section)
        }
      }
    }

    ServiceOptions {
      id:serviceChoices
      width:parent.width
      service:root.service
      fontFamily:root.fontFamily
      uiScale:root.uiScale
    }

    // Section 2: Interface Scale
    Column {
      id: appearanceSection
      width: parent.width
      spacing: Style.space(8)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Text size"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Choose a comfortable reading size. Setup instructions wrap and scroll at every size."
        color: root.mutedColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
      }

      ChoiceGroup {
        width: parent.width
        options: [
          { value: "0.85", label: "Small" },
          { value: "1", label: "Default" },
          { value: "1.15", label: "Large" },
          { value: "1.3", label: "Extra Large" }
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
        accent: root.accentColor
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
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 3: Dependencies and User Choice
    Column {
      id: toolsSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Dependencies and User Choice"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "OmaChat never installs packages automatically. Check the tools below, review their source, and choose Install only for tools you want. The terminal asks for confirmation before invoking sudo; the package manager asks again before making changes. Accounts and API credentials remain your choice."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      DependencyChecklist {
        width: parent.width
        fontFamily: root.fontFamily
        copyColor: root.copyColor
        mutedColor: root.mutedColor
        accentColor: root.accentColor
        urgentColor: root.urgentColor
        foreground: root.foreground
        fs: root.fs
        openUrl: root.openUrl
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 4: Service Guides
    Column {
      id: servicesSection
      width: parent.width
      spacing: Style.space(12)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Service Guides"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "OmaChat supports Google Messages, WhatsApp, and Telegram with isolated credentials, caches, and drafts. Each service connects only when you explicitly configure it."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      Column {
        width: parent.width
        spacing: Style.space(6)
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "Google Messages"
          color: root.copyColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          font.bold: true
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "• Setup: Open Messages for web (messages.google.com/web) in your Chromium browser and sign in. Unlock your desktop keyring, select Pair with Google in OmaChat, and confirm the matching emoji on your phone. Keep your Android phone online.\n• Features: 50-conversation inbox, older history paging, text, photos, captions, reactions, local GIF files, inline GIF playback, and voice notes (with ffmpeg/ffplay).\n• Limitations: Calling is unavailable. The QR pairing fallback only works with older Messages builds that include an in-app scanner; standard camera apps will not pair."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          lineHeight: 1.25
        }
      }

      Column {
        width: parent.width
        spacing: Style.space(6)
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "WhatsApp"
          color: root.copyColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          font.bold: true
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "• Setup: Select WhatsApp in OmaChat, choose Use a QR code, open WhatsApp on your phone > Linked devices > Link a device, and scan the displayed QR code. Keep your phone online during linking and initial sync.\n• Features: Real-time conversation sync, text, photos, captions, local GIF files, retryable media downloads, and incoming static WebP stickers.\n• Limitations: History reflects initial phone sync and live messages; additional history cannot be requested from the phone, but cached history can be paged. Ephemeral and view-once media are intentionally not saved or reopened. Reactions, voice notes, GIF search, and calling are currently unavailable."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          lineHeight: 1.25
        }
      }

      Column {
        width: parent.width
        spacing: Style.space(6)
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "Telegram"
          color: root.copyColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          font.bold: true
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "• Setup: Obtain your api_id and api_hash from my.telegram.org. Run 'python3 scripts/configure-telegram.py' in the plugin folder and restart the Omarchy shell. Then choose Pair with Telegram and scan the QR code from Telegram > Settings > Devices on your phone.\n• Features: Dialog synchronization, older history paging, text, photos, captions, static WebP stickers, read receipts, and voice notes (with ffmpeg/ffplay).\n• Limitations: Animated TGS and video stickers are unsupported. Self-destructing/TTL media is not saved. Calling is unsupported."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          lineHeight: 1.25
        }
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 5: GIPHY GIF Search
    Column {
      id: gifSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Google Messages GIF search (optional)"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: root.giphyKeySet
          ? "A personal GIPHY API key is currently configured in ~/.local/share/omachat/config.json. Enter a new key below to update it, or choose Delete key to remove it."
          : "In-app GIF search requires a personal GIPHY API key. Sending local GIF files from your computer works without an API key."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      Column {
        width: parent.width
        spacing: Style.space(4)

        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "1. Open the GIPHY Developer Dashboard and create a free account."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "2. Create a new app to generate an API key."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "3. Copy the API key, paste it below, and choose Save. OmaChat stores the key only in your local configuration."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
      }

      Button {
        text: "Open GIPHY Dashboard"
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
          placeholderText: root.giphyKeySet ? "Replacement API key" : "Paste GIPHY API key"
          placeholderTextColor: root.mutedColor
          font.pixelSize: fs(Style.font.body)
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
        foreground: root.urgentColor
        fontFamily: root.fontFamily
        onClicked: root.clearKey()
      }

      Text {
        width: parent.width
        visible: root.statusText !== ""
        wrapMode: Text.Wrap
        text: root.statusText
        color: root.statusText.indexOf("fail") >= 0 || root.statusText.indexOf("error") >= 0
          ? root.urgentColor : root.accentColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 6: Upstream Credits and Honest Attribution
    Column {
      id: creditsSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Upstream Credits and Licenses"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Original OmaChat code is MIT licensed. Included third-party code retains its own terms, including libgm's AGPL license and whatsmeow's MPL license."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      Column {
        width: parent.width
        spacing: Style.space(8)

        Column {
          width: parent.width
          spacing: Style.space(2)
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Daemon Architecture"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
            font.bold: true
          }
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Helper architecture adapted from Marc Ford's gmessages-omarchy-plugin (MIT)."
            color: root.mutedColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }
        }

        Column {
          width: parent.width
          spacing: Style.space(2)
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Google Messages"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
            font.bold: true
          }
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Google Messages uses the mautrix project's libgm client. Its AGPL license and upstream exception notices remain in vendor/go.mau.fi/mautrix-gmessages/."
            color: root.mutedColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }
        }

        Column {
          width: parent.width
          spacing: Style.space(2)
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "WhatsApp"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
            font.bold: true
          }
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "WhatsApp support vendors whatsmeow (go.mau.fi/whatsmeow) under the Mozilla Public License 2.0 (MPL-2.0). WhatsApp session database uses go-sqlite3 (MIT) via CGO."
            color: root.mutedColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }
        }

        Column {
          width: parent.width
          spacing: Style.space(2)
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Telegram"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
            font.bold: true
          }
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Telegram uses gotd/td (MIT, Aleksandr Razumov and contributors). Its dependencies retain their own license notices."
            color: root.mutedColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }
        }

        Column {
          width: parent.width
          spacing: Style.space(2)
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Desktop Environment"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
            font.bold: true
          }
          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Native Omarchy desktop plugin kit and Quickshell QtQuick components."
            color: root.mutedColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }
        }
      }

      Flow {
        width: parent.width
        spacing: Style.space(8)
        Button {
          text: "Marc Ford"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/MarcFord/gmessages-omarchy-plugin")
        }
        Button {
          text: "mautrix"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/mautrix/gmessages")
        }
        Button {
          text: "whatsmeow"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/tulir/whatsmeow")
        }
        Button {
          text: "gotd/td"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/gotd/td")
        }
        Button {
          text: "Omarchy"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://omarchy.org/")
        }
        Button {
          text: "All credits and licenses"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.repoUrl + "/blob/main/CREDITS.md")
        }
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 7: About and Maintenance
    Column {
      id: aboutSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "About OmaChat"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "OmaChat (onelegdave.omachat) is a Native Omarchy Plugin providing unified messaging across Google Messages, WhatsApp, and Telegram. Created and maintained by OneLegDave, with AI assistance from Codex."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
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
          text: "System QuikView"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/onelegdave/system-quikview")
        }
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 8: Feedback and Bug Reports
    Column {
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Feedback and Bug Reports"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Report issues or submit pull requests on GitHub. Please include your Omarchy version, desktop environment, and helper status."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      Row {
        spacing: Style.space(8)
        Button {
          text: "Report an issue"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.issuesUrl)
        }
        Button {
          text: "GitHub repository"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.repoUrl)
        }
      }
    }
  }
}
