import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

ShellRoot {
  id: root
  QtObject {
    id: fake
    property bool connected: true
    property var configResult: ({uiScale: 1, enabledServices: [], debugLogPath: "~/.local/state/omachat/auth.log"})
    property var requests: []
    property bool failNextDelete: false
    function call(method, params, callback) {
      requests.push({method: method, params: params})
      if (method === "config") {
        callback(true, configResult)
      } else if (method === "setDebugLogging") {
        configResult = Object.assign({}, configResult, {debugLogging: params.enabled === true})
        callback(true, configResult)
      } else if (method === "deleteDebugLog") {
        if (failNextDelete) { failNextDelete = false; callback(false, "Could not delete the log: permission denied") }
        else callback(true, {deleted: true})
      } else {
        callback(false, "unexpected synthetic settings call: " + method)
      }
    }
    function count(method) {
      var n = 0
      for (var i = 0; i < requests.length; i++) if (requests[i].method === method) n++
      return n
    }
    function last(method) {
      for (var i = requests.length - 1; i >= 0; i--) if (requests[i].method === method) return requests[i]
      return null
    }
  }
  Chat.SettingsView { id: settings; width: 600; height: 700; service: fake }
  TestResult { id: inspect }
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function findText(item, text) {
    if (item && item.text === text) return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findText(children[i], text)
      if (found) return found
    }
    return null
  }
  function findTextContaining(item, text) {
    if (item && typeof item.text === "string" && item.text.indexOf(text) >= 0) return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findTextContaining(children[i], text)
      if (found) return found
    }
    return null
  }
  Timer {
    interval: 200; running: true; repeat: true
    onTriggered: {
      try {
        var section = inspect.findChild(settings, "debugSection")
        var toggle = inspect.findChild(settings, "debugLoggingToggle")
        switch (root.step++) {
        case 0:
          check(!!section && !!toggle, "Debug section and toggle exist")
          var telegram = inspect.findChild(settings, "telegramSection")
          var credits = inspect.findChild(settings, "creditsSection")
          check(telegram.y < section.y && section.y < credits.y, "Debug section sits after Telegram API and before Credits")
          var nav = ["Telegram API", "Debug", "Credits"].map(function(label) { return findText(settings, label) })
          check(nav[0] && nav[1] && nav[2], "Debug appears in the section list")
          var navPos = nav.map(function(b) { return b.mapToItem(settings.contentItem, 0, 0) })
          check(navPos[0].y < navPos[1].y || (navPos[0].y === navPos[1].y && navPos[0].x < navPos[1].x), "Debug follows Telegram API in the section list")
          check(navPos[1].y < navPos[2].y || (navPos[1].y === navPos[2].y && navPos[1].x < navPos[2].x), "Credits follows Debug in the section list")
          check(settings.debugLogging === false && toggle.checked === false, "toggle is off when config has no debugLogging")
          check(fake.count("setDebugLogging") === 0, "loading settings never changes the setting")
          check(!!findText(settings, "Sign-in diagnostics"), "subheading present")
          check(!!findTextContaining(settings, "Turn this on if Google Messages keeps asking you to pair again."), "explanation opening present")
          check(!!findTextContaining(settings, "It covers Google Messages only, and it's off unless you turn it on."), "explains scope and default")
          check(!!findTextContaining(settings, "Google's full error response"), "explains error responses are recorded")
          check(!!findTextContaining(settings, "the browser profile name"), "explains profile names are recorded")
          check(!!findTextContaining(settings, "A fingerprint is a one-way scramble"), "explains fingerprints")
          check(!!findTextContaining(settings, "Some file locations, which include your user name."), "explains file paths")
          check(!!findTextContaining(settings, "It doesn't unlock your keyring, doesn't decrypt anything, and doesn't use what it reads."), "explains the browser watcher is keyless and read-only")
          check(!!findText(settings, "Your messages, contacts, phone numbers, passwords, or the cookies themselves."), "explains what is never recorded")
          check(!!findTextContaining(settings, "programs running as you, and the system administrator, can too"), "access wording is accurate")
          check(!!findTextContaining(settings, "about 2 MB"), "states the size limit")
          check(!!findTextContaining(settings, "can include your IP address"), "warns before sharing")
          check(!!findTextContaining(settings, "Your existing log stays until you choose Delete log."), "explains turning off keeps the log")
          check(findText(settings, "Log file: ~/.local/state/omachat/auth.log") !== null, "shows the log location")
          toggle.toggled()
          break
        case 1:
          var on = fake.last("setDebugLogging")
          check(on && on.params.enabled === true, "toggling sends setDebugLogging enabled=true")
          check(settings.debugLogging === true && toggle.checked === true, "toggle reflects the saved value")
          toggle.toggled()
          break
        case 2:
          var off = fake.last("setDebugLogging")
          check(off && off.params.enabled === false, "toggling again sends setDebugLogging enabled=false")
          check(settings.debugLogging === false, "toggle off reflects the saved value")
          var del = inspect.findChild(settings, "deleteDebugLogBtn")
          check(del && del.visible && del.text === "Delete log", "Delete log button is shown")
          del.clicked()
          check(fake.count("deleteDebugLog") === 0, "Delete log asks before deleting")
          check(inspect.findChild(settings, "deleteDebugLogPrompt").visible, "confirmation prompt is shown")
          inspect.findChild(settings, "cancelDeleteDebugLogBtn").clicked()
          check(fake.count("deleteDebugLog") === 0 && !settings.deleteDebugLogConfirmOpen, "Cancel does not delete")
          del.clicked()
          inspect.findChild(settings, "confirmDeleteDebugLogBtn").clicked()
          check(fake.count("deleteDebugLog") === 1, "confirming sends deleteDebugLog once")
          check(settings.debugStatusText === "Log deleted." && !settings.debugStatusIsError, "reports the log was deleted")
          fake.failNextDelete = true
          del.clicked()
          inspect.findChild(settings, "confirmDeleteDebugLogBtn").clicked()
          check(settings.debugStatusIsError && settings.debugStatusText.indexOf("permission denied") >= 0, "reports a delete failure")
          console.log("OMACHAT_DEBUG_SETTINGS_PASS")
          Qt.quit()
          return
        }
      } catch (error) {
        console.error("OMACHAT_DEBUG_SETTINGS_FAIL", error)
        Qt.quit()
      }
    }
  }
}
