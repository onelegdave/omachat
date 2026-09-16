import QtQuick
import Quickshell
import Quickshell.Io

Item {
  id: root
  required property string pluginDir
  property string installedVersion: ""
  property string expectedSourceID: ""
  property string latestVersion: ""
  property string dismissedVersion: ""
  property bool automatic: false
  property double checkedAt: 0
  property string checkError: ""
  property string sourceError: ""
  readonly property string error: [checkError,sourceError].filter(function(t){return !!t}).join("\n")
  property string actionText: ""
  property bool busy: stateProc.running
  property bool inspecting: inspectProc.running
  readonly property bool available: newer(latestVersion, installedVersion)
  readonly property bool noticeVisible: available && latestVersion !== dismissedVersion
  readonly property string releaseUrl: "https://github.com/onelegdave/omachat/releases" + (latestVersion ? "/tag/v" + latestVersion : "")
  readonly property string script: pluginDir + "/scripts/updates.py"

  function newer(a,b) {
    if (!/^\d+\.\d+\.\d+$/.test(a) || !/^\d+\.\d+\.\d+$/.test(b)) return false
    var aa=a.split('.'), bb=b.split('.')
    for(var i=0;i<3;i++) { if(Number(aa[i]) !== Number(bb[i])) return Number(aa[i]) > Number(bb[i]) }
    return false
  }
  function run(action) {
    if(busy) return
    checkError=""
    stateProc.command=["python3",script,action]
    stateProc.running=true
    stateTimeout.restart()
  }
  function inspect() { if (!inspectProc.running) { expectedSourceID=""; sourceError=""; inspectProc.running=true; inspectTimeout.restart() } }
  function launchUpdate() {
    if(launchProc.running) return
    actionText="Terminal requested. Review the changes and confirm there. After it finishes, return here and rebuild if requested."
    launchProc.running=true
  }
  Component.onCompleted: { inspect(); run("daily") }
  Timer { interval:3600000; running:true; repeat:true; onTriggered: root.run("daily") }
  Process {
    id: stateProc
    stdout: StdioCollector { id: stateOutput; waitForEnd:true }
    onExited: function(code) {
      stateTimeout.stop()
      try {
        var data=JSON.parse(stateOutput.text)
        if(code !== 0) throw new Error(data.error || "Could not read update preferences")
        root.installedVersion=data.installed || ""
        root.latestVersion=data.latest || ""
        root.dismissedVersion=data.dismissed || ""
        root.automatic=data.automatic === true
        root.checkedAt=data.checkedAt || 0
        root.checkError=data.error || ""
      } catch(e) { root.checkError="Update check unavailable. Verify Python 3 is installed and try again. " + String(e) }
    }
  }
  Timer { id:stateTimeout; interval:20000; onTriggered: { stateProc.running=false; root.checkError="Update check timed out. Try again later." } }
  Process {
    id: inspectProc
    command:["python3",root.script,"inspect"]
    stdout: StdioCollector { id:inspectOutput; waitForEnd:true }
    onExited:function(code) {
      inspectTimeout.stop()
      try {
        var data=JSON.parse(inspectOutput.text)
        if(code !== 0 || !/^[a-f0-9]{64}$/.test(data.sourceID || "")) throw new Error("Could not identify installed helper source")
        root.expectedSourceID=data.sourceID
        root.installedVersion=data.installed
      } catch(e) { root.sourceError=String(e) }
    }
  }
  Timer { id:inspectTimeout; interval:20000; onTriggered: { inspectProc.running=false; root.sourceError="Checking the installed source timed out. Try again." } }
  Process {
    id:launchProc
    command:["omarchy","launch","terminal","python3",root.script,"update"]
    onExited:function(code) { if(code !== 0) root.actionText="Could not open the terminal. Run: omarchy plugin update onelegdave.omachat" }
  }
}
