import QtQuick
import Quickshell
import "Build" as Build
ShellRoot {
 id:root
 property bool requested:false
 Build.Service {id:service}
 Timer {
  interval:100;running:true;repeat:true
  onTriggered:{
   if(!root.requested && service.goPresent){root.requested=true;service.buildHelper();return}
   if(root.requested && !service.building && service.helperError){
    if(service.helperError.indexOf("Python 3")<0){console.error("OMACHAT_BUILD_UNAVAILABLE_FAIL",service.helperError);Qt.quit();return}
    service.stopHelper()
    console.log("OMACHAT_BUILD_UNAVAILABLE_PASS missing Python clears busy state with actionable guidance")
    Qt.quit()
   }
  }
 }
 Timer {interval:10000;running:true;onTriggered:{console.error("OMACHAT_BUILD_UNAVAILABLE_FAIL stuck building",service.building,service.helperError);Qt.quit()}}
}
