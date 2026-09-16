import QtQuick
import qs.Commons
import qs.Ui

Column {
  id:root
  property var service:null
  readonly property var updates: service && service.updates ? service.updates : null
  property string fontFamily: Style.font.family
  property real uiScale:1
  property color foreground:Color.popups.text
  property color mutedColor:foreground
  spacing:Style.space(8)
  function fs(n) { return Math.max(12,Math.round(n*uiScale)) }
  Text {
    width:parent.width; text:"Updates"; color:root.foreground
    font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.heading); font.bold:true
  }
  Text {
    objectName:"updateVersionLabel"
    width:parent.width; wrapMode:Text.Wrap; textFormat:Text.PlainText
    text:"Installed: " + (root.updates && root.updates.installedVersion ? root.updates.installedVersion : "Checking...")
      + (root.updates && root.updates.latestVersion ? "  •  Latest release: " + root.updates.latestVersion : "")
      + (root.updates && root.updates.checkedAt ? "\nLast checked: " + new Date(root.updates.checkedAt * 1000).toLocaleString() : "\nNo successful release check yet.")
    color:root.foreground; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
  }
  Text {
    width:parent.width; wrapMode:Text.Wrap; textFormat:Text.PlainText
    text:root.updates && root.updates.available ? "A newer release is available. Read the release notes, then update in the terminal." : "Check for releases here or turn on daily checks. Updates always require your confirmation."
    color:root.foreground; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
  }
  Flow {
    width:parent.width; spacing:Style.space(8)
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      objectName:"checkUpdatesButton"; text:root.updates && root.updates.busy ? "Checking..." : "Check for updates"
      enabled:!!root.updates && !root.updates.busy
      focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:{root.updates.inspect();root.updates.run("check")}
    }
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      text:"Release notes"; focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:Qt.openUrlExternally(root.updates ? root.updates.releaseUrl : "https://github.com/onelegdave/omachat/releases")
    }
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      objectName:"installUpdateButton"; text:"Update"; enabled:!!root.updates && !(root.service && root.service.building)
      focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:root.updates.launchUpdate()
    }
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      objectName:"automaticUpdatesButton"
      text:root.updates && root.updates.automatic ? "Daily checks: On" : "Daily checks: Off"
      enabled:!!root.updates && !root.updates.busy
      focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:root.updates.run(root.updates.automatic ? "auto-off" : "auto-on")
    }
  }
  Text {
    width:parent.width; wrapMode:Text.Wrap
    text:"Daily checks are optional and off by default. Checks request public release metadata from GitHub, which sees your IP address. No messages or account credentials are sent. The updater follows the default branch; review its changes in the terminal."
    color:root.mutedColor; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.bodySmall)
  }
  Text {
    objectName:"helperUpdateStatus"
    width:parent.width; wrapMode:Text.Wrap; textFormat:Text.PlainText
    text: !root.service ? "Helper status unavailable."
      : root.service.building ? "Building helper. This may take several minutes with no output. The helper restarts automatically on success."
      : root.service.helperNeedsRebuild ? "Update downloaded. Rebuild helper to finish. The running helper does not match the installed source."
      : !root.service.connected ? "Waiting for the helper to connect. Build it if it is missing."
      : root.service.helperBuildChecked && root.updates && root.updates.expectedSourceID ? "The running helper matches the installed source."
      : "Checking the running helper..."
    color:root.foreground; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
  }
  Flow {
    width:parent.width; spacing:Style.space(8)
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      objectName:"rebuildUpdateHelperButton"; text:root.service && root.service.building ? "Building..." : "Rebuild helper"
      enabled:!!root.service && root.service.goPresent === true && !root.service.building
      focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:root.service.buildHelper()
    }
    Button {
      Accessible.role:Accessible.Button
      Accessible.name:text
      fontSize:root.fs(Style.font.body)
      text:"Recheck helper"; enabled:!!root.service && typeof root.service.checkGo === "function" && !root.service.building
      focusable:true; bordered:true; foreground:root.foreground; fontFamily:root.fontFamily
      onClicked:{root.service.checkGo();if(root.updates)root.updates.inspect();if(root.service.connected)root.service.checkRunningBuild()}
    }
  }
  Text {
    width:parent.width; wrapMode:Text.Wrap; textFormat:Text.PlainText
    visible:text !== ""
    text:[root.updates ? root.updates.error : "",root.updates ? root.updates.actionText : "",root.service && root.service.helperError ? root.service.helperError : ""].filter(function(t){return !!t}).join("\n")
    color:root.foreground; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
  }
}
