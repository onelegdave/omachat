import QtQuick
import Quickshell
import "Build" as Build

ShellRoot {
  id: root
  property bool requested: false
  property int readyTicks: 0
  Build.Service { id: service }
  Timer {
    interval: 100; running: true; repeat: true
    onTriggered: {
      if (root.readyTicks > 0 && !service.connected) {
        console.log("OMACHAT_BUILD_SERVICE_PASS staged first-install build, shell-owned helper, no enabled providers")
        stop(); Qt.callLater(function() { Qt.quit() }); return
      }
      if (!root.requested && service.goPresent && service.helperState === "missing") {
        root.requested = true
        service.buildHelper()
      }
      if (root.requested && !service.building && service.helperError) {
        console.error("OMACHAT_BUILD_SERVICE_FAIL",service.helperError)
        stop(); service.stopHelper(); Qt.callLater(function() { Qt.quit() }); return
      }
      if (!root.requested || !service.connected || !service.servicesConfigLoaded) return
      if (!service.helperPresent || !service.serviceSelectionRequired || service.enabledServices.length !== 0) {
        console.error("OMACHAT_BUILD_SERVICE_FAIL fresh install must require explicit service selection")
        stop(); service.stopHelper(); Qt.callLater(function() { Qt.quit() }); return
      }
      if (root.readyTicks++ === 0) service.stopHelper()
    }
  }
}
