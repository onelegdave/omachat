import QtQuick
import Quickshell
import "Build" as Build

ShellRoot {
  id: root
  property int initialErrors: 0
  Build.Service {
    id: service
    onTransportError: root.initialErrors++
  }
  Timer {
    interval:100
    running:true
    repeat:true
    onTriggered: {
      if(service.connected && service.servicesConfigLoaded && service.state === "disabled") {
        if(service.enabledServices.length !== 0 || service.serviceSelectionRequired) {
          console.error("OMACHAT_CONNECTION_FAIL explicit empty choice did not survive restart"); Qt.quit(); return
        }
        if(root.initialErrors > 0) console.log("OMACHAT_CONNECTION_PASS recovered after delayed helper startup")
        else console.error("OMACHAT_CONNECTION_FAIL did not exercise initial connection failure")
        Qt.quit()
      }
    }
  }
  Timer {
    interval:15000
    running:true
    onTriggered: { console.error("OMACHAT_CONNECTION_FAIL helper never connected"); Qt.quit() }
  }
}
