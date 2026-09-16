import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

ShellRoot {
 id:root
 property string artifacts:Quickshell.env("OMACHAT_UI_ARTIFACTS")
 property int frame:0
 QtObject {
  id:manager
  property string installedVersion:"0.3.11"
  property string latestVersion:"0.4.0"
  property string expectedSourceID:"source"
  property bool available:true
  property bool automatic:false
  property bool busy:false
  property double checkedAt:1789521107
  property string error:""
  property string actionText:""
  property string lastAction:""
  function run(action) {lastAction=action;if(action==="auto-on")automatic=true}
  function inspect() {}
  function launchUpdate() {lastAction="update"}
 }
 QtObject {
  id:fake
  property var updates:manager
  property bool building:false
  property bool connected:true
  property bool helperNeedsRebuild:true
  property bool helperBuildChecked:true
  property bool goPresent:true
  property string helperError:""
  function buildHelper() {building=true}
  function checkGo() {}
  function checkRunningBuild() {}
 }
 FloatingWindow {
  id:window
  implicitWidth:660; implicitHeight:820; visible:true; color:Color.popups.background
  Rectangle {
   id:canvas;anchors.fill:parent;color:root.frame===0?"#161820":"#faf7f1"
   Chat.UpdateSettings {id:settings;anchors.fill:parent;anchors.margins:24;service:fake;uiScale:1.15;foreground:root.frame===0?"#eeeeee":"#202020";mutedColor:foreground}
  }
 }
 TestResult {id:inspect}
 function check(ok,message){if(!ok)throw new Error(message)}
 Timer {
  interval:500; running:true
  onTriggered:{
   try {
    var checkButton=inspect.findChild(settings,"checkUpdatesButton")
    root.check(checkButton.enabled && checkButton.focusable,"check action accessible")
    checkButton.clicked();root.check(manager.lastAction==="check","manual check routed")
    inspect.findChild(settings,"automaticUpdatesButton").clicked();root.check(manager.automatic,"daily opt-in routed")
    inspect.findChild(settings,"installUpdateButton").clicked();root.check(manager.lastAction==="update","terminal action routed")
    root.check(inspect.findChild(settings,"helperUpdateStatus").text.indexOf("Rebuild helper")>=0,"mismatch guidance")
    inspect.findChild(settings,"rebuildUpdateHelperButton").clicked();root.check(fake.building,"rebuild routed")
    root.check(!inspect.findChild(settings,"installUpdateButton").enabled,"update blocked during build")
    fake.building=false
    capture.restart()
   }catch(e){console.error("OMACHAT_UPDATES_FAIL",e);Qt.quit()}
  }
 }
 Timer {
  id:capture;interval:200
  onTriggered:{
   if(root.artifacts) canvas.grabToImage(function(result){result.saveToFile(root.artifacts+"/updates-"+root.frame+".png");next()})
   else next()
  }
 }
 function next(){
  if(frame===0){frame=1;Color.shellValues={"popups.background":"#faf7f1","popups.text":"#202020"};fake.helperNeedsRebuild=false;capture.restart()}
  else {console.log("OMACHAT_UPDATES_PASS update controls, opt-in, rebuild guidance, and build exclusion");Qt.quit()}
 }
}
