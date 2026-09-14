import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

Column {
  id: checklist
  objectName: "dependencyChecklist"
  width: parent.width
  spacing: Style.space(16)

  // Properties to be set by the parent (SettingsView.qml)
  property string fontFamily: ""
  property color copyColor: "white"
  property color mutedColor: "gray"
  property color accentColor: "blue"
  property color urgentColor: "red"
  property color foreground: "white"
  property var fs: function(n) { return n; }
  property var openUrl: function(url) { }

  property string scriptPath: {
    var url = String(Qt.resolvedUrl("."))
    if (url.indexOf("file://") === 0)
      url = decodeURIComponent(url.substring("file://".length))
    if (url.length > 1 && url.charAt(url.length - 1) === "/")
      url = url.substring(0, url.length - 1)
    return url + "/scripts/dependencies.py"
  }

  property var dependencies: []
  property bool loading: false
  property bool autoCheck: true
  property string errorText: ""
  property string actionText: ""
  property string pendingInstall: ""
  property var launch: function(argv) { launchProc.command = argv; launchProc.running = true }

  function acceptResult(code, text) {
    checkTimeout.stop()
    loading = false
    dependencies = []
    try {
      if (code !== 0) throw new Error("check failed")
      var rows = JSON.parse(text)
      if (!Array.isArray(rows) || rows.length === 0) throw new Error("invalid result")
      for (var i = 0; i < rows.length; i++) {
        var row = rows[i]
        if (!row || typeof row.installed !== "boolean" || !/^[a-z]+$/.test(row.id)
            || typeof row.name !== "string" || typeof row.purpose !== "string"
            || typeof row.detail !== "string" || typeof row.sourceUrl !== "string"
            || !row.sourceUrl.startsWith("https://gitlab.archlinux.org/archlinux/packaging/packages/"))
          throw new Error("invalid row")
      }
      dependencies = rows
      errorText = ""
    } catch(e) {
      errorText = "Could not check tools. Python 3 and scripts/dependencies.py must be available. No tools are marked available and nothing was installed. Recheck or use the dependency guide."
    }
  }

  function install(id) {
    if (loading || errorText || pendingInstall) return
    for (var i = 0; i < dependencies.length; i++) {
      if (dependencies[i].id === id && dependencies[i].installed === false) {
        pendingInstall = id
        actionText = "Terminal requested. Review the command and confirm there only if you want to install. After closing the terminal, choose Recheck."
        launch(["omarchy", "launch", "terminal", "python3", scriptPath, "--install", id])
        launchTimeout.restart()
        return
      }
    }
  }

  function fetch() {
    if (loading) return
    dependencies = []
    errorText = ""
    actionText = ""
    pendingInstall = ""
    loading = true
    checkProc.running = true
    checkTimeout.restart()
  }

  Process {
    id: checkProc
    command: ["python3", checklist.scriptPath, "--check"]
    stdout: StdioCollector { id: checkStdout; waitForEnd: true }
    onExited: function(code) {
      checklist.acceptResult(code, checkStdout.text)
    }
  }

  Component.onCompleted: if (autoCheck) fetch()

  Timer {
    id: checkTimeout
    interval: 10000
    onTriggered: { checkProc.running = false; checklist.acceptResult(1, "") }
  }
  Process {
    id: launchProc
    onExited: function(code) {
      launchTimeout.stop()
      if (code !== 0) {
        checklist.pendingInstall = ""
        checklist.actionText = "Could not open the terminal. Review the dependency guide for manual installation."
      }
    }
  }
  Timer {
    id: launchTimeout
    interval: 5000
    onTriggered: {
      checklist.actionText = "If no terminal opened, use the dependency guide to install manually. Otherwise finish or cancel there, then choose Recheck."
    }
  }

  Text {
    width: parent.width
    wrapMode: Text.Wrap
    text: "Review source before installing if you wish. Install opens a terminal with two confirmation steps. Available checks tools, not accounts, devices, portals, or keyring access."
    color: checklist.copyColor
    font.family: checklist.fontFamily
    font.pixelSize: checklist.fs(Style.font.body)
  }
  Text {
    objectName: "dependencyStatus"
    width: parent.width
    visible: text !== ""
    wrapMode: Text.Wrap
    text: checklist.errorText || checklist.actionText
    color: checklist.errorText ? checklist.urgentColor : checklist.copyColor
    font.family: checklist.fontFamily
    font.pixelSize: checklist.fs(Style.font.body)
  }

  Flow {
    width: parent.width
    spacing: Style.space(8)
    Button {
      text: checklist.loading ? "Checking..." : "Recheck"
      bordered: true
      foreground: checklist.foreground
      fontFamily: checklist.fontFamily
      enabled: !checklist.loading
      onClicked: checklist.fetch()
    }
    Button {
      text: "Dependency guide"
      bordered: true
      foreground: checklist.foreground
      fontFamily: checklist.fontFamily
      onClicked: checklist.openUrl("https://github.com/onelegdave/omachat/blob/main/docs/dependencies.md")
    }
    Button {
      text: "Review install code"
      bordered: true
      foreground: checklist.foreground
      fontFamily: checklist.fontFamily
      onClicked: checklist.openUrl("https://github.com/onelegdave/omachat/blob/main/scripts/dependencies.py")
    }
  }

  Column {
    width: parent.width
    spacing: Style.space(12)

    Repeater {
      model: checklist.dependencies
      delegate: Column {
        width: parent.width
        spacing: Style.space(4)

        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: modelData.name + (modelData.installed ? " (Available)" : " (Missing)")
          color: modelData.installed ? checklist.accentColor : checklist.urgentColor
          font.family: checklist.fontFamily
          font.pixelSize: checklist.fs(Style.font.body)
          font.bold: true
        }

        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: modelData.purpose
          color: checklist.copyColor
          font.family: checklist.fontFamily
          font.pixelSize: checklist.fs(Style.font.body)
          lineHeight: 1.25
        }

        Text {
          width: parent.width
          wrapMode: Text.Wrap
          text: modelData.detail
          color: checklist.mutedColor
          font.family: checklist.fontFamily
          font.pixelSize: checklist.fs(Style.font.body)
          lineHeight: 1.25
        }

        Flow {
          width: parent.width
          spacing: Style.space(8)

          Button {
            text: "Review source"
            objectName: "dependencySource-" + modelData.id
            bordered: true
            foreground: checklist.foreground
            fontFamily: checklist.fontFamily
            onClicked: checklist.openUrl(modelData.sourceUrl)
          }

          Button {
            objectName: "dependencyInstall-" + modelData.id
            visible: !modelData.installed
            enabled: !checklist.loading && checklist.pendingInstall === ""
            text: "Install"
            bordered: true
            foreground: checklist.urgentColor
            fontFamily: checklist.fontFamily
            onClicked: checklist.install(modelData.id)
          }
        }
      }
    }
  }

}
