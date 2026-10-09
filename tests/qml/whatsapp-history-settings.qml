import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

ShellRoot {
  id: root
  QtObject {
    id: fake
    property int historyCacheMB: 128
    property int saveCount: 0
    function call(method, params, callback) {
      if (method === "config") {
        callback(true, {whatsappHistoryCacheMB: historyCacheMB, uiScale: 1, enabledServices: []})
      } else if (method === "setWhatsAppHistoryCache") {
        historyCacheMB = Number(params.sizeMB)
        saveCount++
        callback(true, {whatsappHistoryCacheMB: historyCacheMB})
      } else {
        callback(false, "unexpected synthetic settings call: " + method)
      }
    }
  }
  Chat.SettingsView { id: settings; width: 600; height: 700; service: fake }
  TestResult { id: inspect }
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label) }
  function findText(item, text) {
    if (item && item.text === text) return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findText(children[i], text)
      if (found) return found
    }
    return null
  }
  Timer {
    interval: 200; running: true; repeat: true
    onTriggered: {
      try {
        if (root.step++ === 0) {
          check(settings.whatsappHistoryCacheMB === 128, "older config defaults to 128 MB")
          var label = findText(settings, "WhatsApp history cache")
          var helper = findText(settings, "Older cached messages are removed automatically when space is needed. This does not delete messages from WhatsApp. Photos and videos use the separate media cache.")
          check(!!label && !!helper, "cache size field and approved helper text are present")
          var choice = findText(settings, "256 MB")
          check(!!choice, "cache size choices include consistent MB units")
          choice.clicked()
          check(fake.saveCount === 1 && fake.historyCacheMB === 256, "selection saves exactly one new preference")
          check(settings.whatsappHistoryCacheMB === 256, "successful save updates the selected value")
          settings.load()
          check(settings.whatsappHistoryCacheMB === 256, "saved preference reloads from synthetic config")
          check(findText(settings, "64 MB") && findText(settings, "128 MB") && findText(settings, "512 MB"), "all approved budget choices are available")
          console.log("OMACHAT_WHATSAPP_HISTORY_SETTINGS_PASS")
          Qt.quit()
        }
      } catch (error) {
        console.error("OMACHAT_WHATSAPP_HISTORY_SETTINGS_FAIL", error)
        Qt.quit()
      }
    }
  }
}
