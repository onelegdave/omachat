import QtQuick
import QtTest
import Quickshell
import Quickshell.Io
import "Chat" as Chat
import "Build" as Build

ShellRoot {
 id: root
 property bool requested: false
 property bool refreshRequested: false
 property bool selectionRequested: false
 property bool selectionConfirmed: false
 property bool disableRequested: false
 Build.Service { id: service }
 Chat.Panel { id: panel; service: service }
 TestResult { id: inspect }
 Timer {
  interval:5000; running:true; repeat:true
  onTriggered: console.log("BUILD_PROGRESS",service.helperState,service.connected,service.state,service.servicesConfigLoaded,service.savingServices,service.restartingServices,JSON.stringify(service.enabledServices),service.servicesError)
 }
 Process {
  command: ["sleep", "0.1"]
  running: true
  onExited: {
   if (!root.requested && service.goPresent && service.helperState === "missing") {
    var button=inspect.findChild(panel,"buildHelperButton")
    if (!button || !button.enabled) { console.error("OMACHAT_BUILD_FAIL missing button");Qt.quit();return }
    root.requested=true
    button.clicked()
   }
   if (root.requested && service.helperError) {
    console.error("OMACHAT_BUILD_FAIL",service.helperError);Qt.quit();return
   }
   if (root.requested && service.connected && service.servicesConfigLoaded && !root.selectionRequested) {
    if (!service.serviceSelectionRequired || service.enabledServices.length !== 0) {
     console.error("OMACHAT_BUILD_FAIL fresh install must start with no services");Qt.quit();return
    }
    root.selectionRequested=true
    service.call("setEnabledServices",{enabledServices:["unknown"]},function(ok) {
     if(ok) { console.error("OMACHAT_BUILD_FAIL invalid service accepted");Qt.quit();return }
     service.setEnabledServices(["gmessages"],function(saved) {
      if(!saved) { console.error("OMACHAT_BUILD_FAIL service enable failed");Qt.quit();return }
      root.selectionConfirmed=true
     })
    },"gmessages")
   }
   if (root.selectionConfirmed && service.connected && service.state === "unpaired") {
    if (!root.refreshRequested) {
     root.refreshRequested=true
     service.refreshConversations()
     running=true;return
    }
    if (service.refreshing) { running=true;return }
    if (service.refreshError.indexOf("not connected to Google Messages") < 0) {
     console.error("OMACHAT_BUILD_FAIL refresh did not reach daemon",service.refreshError);Qt.quit();return
    }
    if (!root.disableRequested) {
     root.disableRequested=true
     service.setEnabledServices([],function(saved) {
      if(!saved || service.enabledServices.length !== 0 || service.serviceSelectionRequired) {
       console.error("OMACHAT_BUILD_FAIL explicit all-disabled choice failed");Qt.quit();return
      }
      service.call("refresh",null,function(ok,res) {
       if(ok || String(res).indexOf("disabled") < 0) {
        console.error("OMACHAT_BUILD_FAIL disabled service accepted refresh",res);Qt.quit();return
       }
       service.stopHelper()
       console.log("OMACHAT_BUILD_PASS compiled helper, fresh chooser, opt-in, opt-out, and disabled RPC guard")
       Qt.quit()
      },"gmessages")
     })
    }
    running=true;return
   }
   running=true
  }
 }
}
