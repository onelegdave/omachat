import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat
import "Build" as Build

ShellRoot {
  id:root
  property bool requested:false
  property int beforeGeneration:0
  Build.Service { id:service }
  Chat.UpdateSettings { id:settings; service:service; width:600 }
  TestResult { id:inspect }
  Timer {
    interval:100; running:true; repeat:true
    onTriggered:{
      if (!root.requested && service.connected && service.helperBuildChecked && service.updates.expectedSourceID) {
        if (!service.updates.newer("0.3.10", "0.3.9") || service.updates.newer("0.3.9", "0.3.10") || service.updates.newer("0.4.0-rc1", "0.3.11")) { console.error("OMACHAT_UPGRADE_FAIL version comparison");Qt.quit();return }
        if (!service.helperNeedsRebuild) { console.error("OMACHAT_UPGRADE_FAIL old helper was not detected");Qt.quit();return }
        var button=inspect.findChild(settings,"rebuildUpdateHelperButton")
        var label=inspect.findChild(settings,"helperUpdateStatus")
        if(!button || !button.enabled || label.text.indexOf("Rebuild helper") < 0) { console.error("OMACHAT_UPGRADE_FAIL rebuild guidance missing");Qt.quit();return }
        root.requested=true
        root.beforeGeneration=service.connectionGeneration
        button.clicked()
      }
      if(root.requested && service.helperError) { console.error("OMACHAT_UPGRADE_FAIL",service.helperError);Qt.quit();return }
      if(root.requested && !service.building && service.connected && service.connectionGeneration > root.beforeGeneration && service.helperBuildChecked) {
        if(service.helperNeedsRebuild || !service.runningSourceID || service.runningSourceID !== service.updates.expectedSourceID) { console.error("OMACHAT_UPGRADE_FAIL running helper still outdated");Qt.quit();return }
        if(!service.servicesConfigLoaded) return
        if(service.serviceSelectionRequired || service.enabledServices.length !== 0) { console.error("OMACHAT_UPGRADE_FAIL saved service choices lost");Qt.quit();return }
        service.stopHelper()
        console.log("OMACHAT_UPGRADE_PASS old running helper detected, rebuilt, restarted, and verified with saved choices")
        Qt.quit()
      }
    }
  }
  Timer { interval:80000; running:true; onTriggered:{console.error("OMACHAT_UPGRADE_FAIL timeout",service.helperState,service.helperError);Qt.quit()} }
}
