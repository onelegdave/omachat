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
  readonly property string githubProfileUrl: "https://github.com/onelegdave"
  readonly property string xProfileUrl: "https://x.com/OneLegDavePDX"
  readonly property string coffeeUrl: "https://buymeacoffee.com/onelegdave"

  property bool giphyKeySet: false
  property string keyDraft: ""
  property string statusText: ""
  property bool saving: false

  property bool telegramConfigured: false
  property int telegramApiId: 0
  property string telegramIdDraft: ""
  property string telegramHashDraft: ""
  property string telegramStatusText: ""
  property bool savingTelegram: false

  clip: true
  boundsBehavior: Flickable.StopAtBounds
  contentWidth: width
  contentHeight: col.implicitHeight + Style.space(32)
  Controls.ScrollBar.vertical: Controls.ScrollBar { policy: Controls.ScrollBar.AsNeeded }

  function showUpdates() { jumpTo(updatesSection) }

  function jumpTo(section) {
    contentY = Math.max(0, Math.min(section.y, contentHeight - height))
  }

  function load() {
    if (!service) return
    service.call("config", null, function(ok, res) {
      if (!ok || !res) return
      root.giphyKeySet = res.giphyKeySet === true
      root.telegramConfigured = res.telegramConfigured === true
      root.telegramApiId = Number(res.telegramApiId) || 0
      var s = Number(res.uiScale)
      if (isFinite(s) && s > 0) root.uiScale = s
    }, "gmessages")
  }

  function saveTelegramCredentials() {
    if (!service) return
    var idStr = telegramIdDraft.trim()
    var hashStr = telegramHashDraft.trim()
    var idNum = parseInt(idStr, 10)
    if ((!idNum || isNaN(idNum) || idNum <= 0) && root.telegramConfigured && root.telegramApiId > 0 && idStr === "") {
      idNum = root.telegramApiId
    }
    if (!idNum || isNaN(idNum) || idNum <= 0) {
      root.telegramStatusText = "Error: api_id must be a positive integer."
      return
    }
    if (!hashStr && root.telegramConfigured) {
      // Keep existing hash
    } else if (hashStr.length !== 32 || !/^[0-9a-fA-F]{32}$/.test(hashStr)) {
      root.telegramStatusText = "Error: api_hash must be a 32-character hexadecimal string."
      return
    }
    savingTelegram = true
    telegramStatusText = ""
    service.call("setTelegramCredentials", { apiId: idNum, apiHash: hashStr }, function(ok, res) {
      root.savingTelegram = false
      if (!ok) {
        root.telegramStatusText = String(res)
        return
      }
      root.telegramConfigured = res && res.telegramConfigured === true
      root.telegramApiId = res && res.telegramApiId ? Number(res.telegramApiId) : idNum
      root.telegramIdDraft = ""
      root.telegramHashDraft = ""
      root.telegramStatusText = "Telegram API credentials saved."
    }, "telegram")
  }

  function clearTelegramCredentials() {
    if (!service) return
    savingTelegram = true
    telegramStatusText = ""
    service.call("setTelegramCredentials", { apiId: 0, apiHash: "" }, function(ok, res) {
      root.savingTelegram = false
      if (!ok) {
        root.telegramStatusText = String(res)
        return
      }
      root.telegramConfigured = false
      root.telegramApiId = 0
      root.telegramIdDraft = ""
      root.telegramHashDraft = ""
      root.telegramStatusText = "Telegram API credentials removed."
    }, "telegram")
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
          {label:"Updates", section:updatesSection},
          {label:"Services", section:serviceChoices},
          {label:"Service guides", section:servicesSection},
          {label:"Tools", section:toolsSection},
          {label:"Telegram API", section:telegramSection},
          {label:"GIF search", section:gifSection},
          {label:"Credits", section:creditsSection},
          {label:"About", section:aboutSection}
        ]
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          required property var modelData
          text: modelData.label
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.jumpTo(modelData.section)
        }
      }
    }

    UpdateSettings {
      id:updatesSection
      objectName:"updatesSection"
      width:parent.width
      service:root.service
      fontFamily:root.fontFamily
      uiScale:root.uiScale
      foreground:root.foreground
      mutedColor:root.mutedColor
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
        focusable: true
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
        text: "OmaChat supports Google Messages, WhatsApp, Telegram, and Messenger with separate credentials, caches, and drafts in one shared helper. Choose enabled services in Settings. Previously paired, enabled services reconnect automatically. Service separation is not a security sandbox."
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
          text: "• Setup: Obtain your api_id and api_hash from my.telegram.org. These are application credentials, not your Telegram login password.\n1. Configure credentials directly in Settings > Telegram API credentials below, or in a terminal run:\npython3 ~/.config/omarchy/plugins/onelegdave.omachat/scripts/configure-telegram.py\n2. In OmaChat, select Telegram > Pair with Telegram. On your phone, open Telegram > Settings > Devices > Link Desktop Device and scan the QR code. Saving credentials alone does not pair your account.\n• Features: Dialog synchronization, older history paging, text, photos, captions, static WebP stickers, read receipts, and voice notes (with ffmpeg/ffplay).\n• Limitations: Animated TGS and video stickers are unsupported. Self-destructing/TTL media is not saved. Calling is unsupported."
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
          text: "Messenger"
          color: root.copyColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          font.bold: true
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "• Setup: Sign in to facebook.com or messenger.com in Chrome, Chromium, or Brave, unlock the desktop keyring, then select Pair from browser in the Messenger tab. Pairing copies only the required session cookies into Messenger's private session store.\n• Features: Encrypted personal and group conversations, history, text and media sending, read receipts, reactions, voice notes (with ffmpeg/ffplay), and optional GIPHY search.\n• Limitations: Meta does not provide a personal-inbox API, so this uses an unofficial protocol client that may break or require re-pairing when Meta changes its service. Calling is unavailable."
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

    // Section 5: Telegram API Credentials
    Column {
      id: telegramSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Telegram API credentials"
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.heading)
        font.bold: true
      }

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: root.telegramConfigured
          ? "Telegram API credentials are configured (API ID: " + root.telegramApiId + "). Enter replacement values below to update them, or choose Delete credentials to remove them."
          : "Telegram requires your own application credentials from my.telegram.org. Enter your api_id and api_hash below. Credentials are stored securely in your private configuration and are never transmitted elsewhere."
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
          text: "1. Open my.telegram.org and sign in with your phone number."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "2. Choose API development tools to create or view your application."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "3. Copy your numeric api_id and 32-character api_hash, paste them below, and select Save. After saving, select Pair with Telegram on the Telegram tab."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
      }

      Button {
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: text
        text: "Open my.telegram.org"
        bordered: true
        foreground: root.foreground
        fontFamily: root.fontFamily
        onClicked: root.openUrl("https://my.telegram.org")
      }

      Column {
        width: parent.width
        spacing: Style.space(8)

        Column {
          width: parent.width
          spacing: Style.space(4)

          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Telegram api_id (numeric)"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }

          TextField {
            id: tgIdField
            objectName: "telegramApiIdField"
            width: parent.width
            placeholderText: root.telegramConfigured ? ("Configured API ID: " + root.telegramApiId) : "Enter numeric api_id (e.g. 1234567)"
            placeholderTextColor: root.mutedColor
            font.pixelSize: fs(Style.font.body)
            foreground: root.foreground
            text: root.telegramIdDraft
            inputMethodHints: Qt.ImhDigitsOnly
            onTextChanged: root.telegramIdDraft = text
          }
        }

        Column {
          width: parent.width
          spacing: Style.space(4)

          Text {
            width: parent.width
            wrapMode: Text.Wrap
            text: "Telegram api_hash (32-character hexadecimal string)"
            color: root.copyColor
            font.family: root.fontFamily
            font.pixelSize: fs(Style.font.body)
          }

          TextField {
            id: tgHashField
            objectName: "telegramApiHashField"
            width: parent.width
            placeholderText: root.telegramConfigured ? "Replacement api_hash (leave blank to keep current)" : "Enter 32-character hex api_hash"
            placeholderTextColor: root.mutedColor
            font.pixelSize: fs(Style.font.body)
            foreground: root.foreground
            password: true
            text: root.telegramHashDraft
            onTextChanged: root.telegramHashDraft = text
            onAccepted: root.saveTelegramCredentials()
          }
        }

        Flow {
          width: parent.width
          spacing: Style.space(8)

          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: text
            id: saveTgBtn
            objectName: "saveTelegramBtn"
            text: root.savingTelegram ? "Saving..." : "Save Telegram credentials"
            bordered: true
            enabled: !root.savingTelegram && (root.telegramIdDraft.trim() !== "" || root.telegramHashDraft.trim() !== "")
            foreground: root.foreground
            fontFamily: root.fontFamily
            onClicked: root.saveTelegramCredentials()
          }

          Button {
            focusable: true
            Accessible.role: Accessible.Button
            Accessible.name: text
            id: deleteTgBtn
            objectName: "deleteTelegramBtn"
            visible: root.telegramConfigured
            text: "Delete credentials"
            foreground: root.urgentColor
            fontFamily: root.fontFamily
            onClicked: root.clearTelegramCredentials()
          }
        }

        Text {
          objectName: "telegramStatusText"
          width: parent.width
          visible: root.telegramStatusText !== ""
          wrapMode: Text.Wrap
          text: root.telegramStatusText
          color: root.telegramStatusText.indexOf("Error") >= 0 || root.telegramStatusText.indexOf("fail") >= 0 || root.telegramStatusText.indexOf("error") >= 0
            ? root.urgentColor : root.accentColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
      }
    }

    Rectangle {
      width: parent.width
      height: 1
      color: Color.popups.border
    }

    // Section 6: GIPHY GIF Search
    Column {
      id: gifSection
      width: parent.width
      spacing: Style.space(10)

      Text {
        width: parent.width
        wrapMode: Text.Wrap
        text: "Google Messages and Messenger GIF search (optional)"
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
          text: "3. Copy the API key, paste it below, and choose Save. OmaChat stores the key in your local configuration and sends it to GIPHY when you search. Search terms are also sent to GIPHY."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
        }
      }

      Button {
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: text
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
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
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
        focusable: true
        Accessible.role: Accessible.Button
        Accessible.name: text
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
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "Marc Ford"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/MarcFord/gmessages-omarchy-plugin")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "mautrix"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/mautrix/gmessages")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "whatsmeow"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/tulir/whatsmeow")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "gotd/td"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/gotd/td")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "Omarchy"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://omarchy.org/")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
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
        text: "OmaChat (onelegdave.omachat) is a Native Omarchy Plugin providing unified messaging across Google Messages, WhatsApp, Telegram, and Messenger. Created and maintained by OneLegDave, with AI assistance from Codex."
        color: root.copyColor
        font.family: root.fontFamily
        font.pixelSize: fs(Style.font.body)
        lineHeight: 1.25
      }

      Flow {
        width: parent.width
        spacing: Style.space(8)
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "onelegdave.dev"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.siteUrl)
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "GitHub"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.githubProfileUrl)
        }
        Button {
          visible: root.xProfileUrl.length > 0
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "X"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.xProfileUrl)
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "OmaChat"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.repoUrl)
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "OmaDroid"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/onelegdave/omadroid")
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "System QuikView"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl("https://github.com/onelegdave/system-quikview")
        }
      }

      Column {
        width: parent.width
        visible: root.coffeeUrl.length > 0
        spacing: Style.space(8)

        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: "I build OmaChat as a free, open-source project. Support is entirely optional and never required to use any feature."
          color: root.mutedColor
          font.family: root.fontFamily
          font.pixelSize: fs(Style.font.body)
          lineHeight: 1.25
        }

        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "Buy Me a Coffee"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.coffeeUrl)
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
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
          text: "Report an issue"
          bordered: true
          foreground: root.foreground
          fontFamily: root.fontFamily
          onClicked: root.openUrl(root.issuesUrl)
        }
        Button {
          focusable: true
          Accessible.role: Accessible.Button
          Accessible.name: text
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
