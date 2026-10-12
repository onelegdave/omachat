import QtQuick
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  QtObject {
    id: a
    property bool connected: true
    property var enabledServices: ["gmessages"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string currentNetwork: "gmessages"
    property string state: "unpaired"
    property var status: ({state:"unpaired"})
    property var requests: []
    function loadProfiles() {}
    function loadConversations(net) {}
    function call(method, params, callback, net) { if (callback) requests.push({method:method, callback:callback}) }
  }
  QtObject {
    id: b
    property bool connected: true
    property var enabledServices: ["gmessages"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string currentNetwork: "gmessages"
    property string state: "unpaired"
    property var status: ({state:"unpaired"})
    property var requests: []
    function loadProfiles() {}
    function loadConversations(net) {}
    function call(method, params, callback, net) { if (callback) requests.push({method:method, callback:callback}) }
  }
  Chat.Panel { id: panel; service: a; settingsOpen:true }
  Chat.SettingsView { id: settings; service:a }
  property var config: ({alwaysPopout:false,popoutMode:"floating",uiScale:1})
  Timer {
    interval:100; running:true
    onTriggered: {
      try {
        var pending=a.requests[0].callback
        panel.open()
        panel.close()
        pending(true,root.config)
        root.check(!panel.opened && !panel.openPendingConfig,"close cancels a config-delayed open")
        panel.panelConfigLoaded=false
        panel.open()
        panel.close()
        a.requests[a.requests.length-1].callback(true,root.config)
        root.check(!panel.opened && !panel.openPendingConfig,"close revokes an open-specific config request")
        panel.panelConfigLoaded=false
        panel.toggle()
        panel.toggle()
        a.requests[a.requests.length-1].callback(true,root.config)
        root.check(!panel.opened && !panel.openPendingConfig,"second toggle cancels pending opening")
        panel.loadConfig()
        var retired=a.requests[a.requests.length-1].callback
        panel.service=b
        panel.service=a
        retired(true,{alwaysPopout:true,popoutMode:"tiled"})
        root.check(!panel.panelConfigLoaded && !panel.alwaysPopout,"A-B-A does not accept a retired config result")
        a.requests[a.requests.length-1].callback(true,root.config)
        panel.loadConfig()
        var beforeSave=a.requests[a.requests.length-1].callback
        panel.applyWindowPreferences(true,"tiled")
        beforeSave(true,root.config)
        root.check(panel.alwaysPopout && panel.popoutMode==="tiled","saved preference wins over an older panel config read")
        settings.load()
        var oldLoad=a.requests[a.requests.length-1].callback
        settings.saveWindowPreferences(true,"tiled")
        var save=a.requests[a.requests.length-1].callback
        save(true,{alwaysPopout:true,popoutMode:"tiled"})
        oldLoad(true,root.config)
        root.check(settings.alwaysPopout && settings.popoutMode==="tiled","saved preference wins over an older Settings read")
        settings.saveWindowPreferences(false,"floating")
        var oldSave=a.requests[a.requests.length-1].callback
        settings.service=b
        settings.service=a
        oldSave(true,root.config)
        root.check(settings.alwaysPopout && !settings.savingWindowPreferences,"retired save cannot change a new Settings service lifetime")
        settings.saveWindowPreferences(false,"floating")
        a.requests[a.requests.length-1].callback(true,{})
        root.check(settings.alwaysPopout && settings.windowStatusText!=="","malformed save is reported without inventing defaults")
        panel.alwaysPopout=false
        panel.panelConfigLoaded=false
        panel.open()
        a.requests[a.requests.length-1].callback(false,"offline")
        root.check(panel.opened && !panel.openPendingConfig,"config failure keeps helper recovery accessible in the panel")
        panel.close()
        console.log("OMACHAT_AUTO_POPOUT_PASS")
      } catch(error) { console.error("OMACHAT_AUTO_POPOUT_FAIL",String(error)) }
      Qt.quit()
    }
  }
}
