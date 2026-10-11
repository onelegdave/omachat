import QtQuick
import Quickshell
import Quickshell.Io
import "Chat" as Chat

// PATH holds a fake python3 that hangs past the paste watchdog.
ShellRoot {
  id: root
  property int step: 0
  property int ticks: 0
  property real started: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function probeRecord(script) {
    probe.code = -1
    probe.command = ["/bin/sh", "-c", script, "probe", Quickshell.env("OMACHAT_CLIPBOARD_HANG_RECORD")]
    probe.running = true
  }
  FileView { id: mode; path: Quickshell.env("OMACHAT_CLIPBOARD_HANG_MODE"); atomicWrites: true }
  FloatingWindow {
    implicitWidth: 500; implicitHeight: 100; visible: true
    Chat.SafeTextField { id: field; width: 450; text: "keep" }
  }
  Process { id: probe; property int code: -1; onExited: function(code) { probe.code = code } }
  Timer {
    running: true; repeat: true; interval: 100
    onTriggered: {
      stop()
      try {
        if (++root.ticks > 170) throw new Error("fixture timed out")
        if (field.pasteBusy || probe.running) { restart(); return }
        switch (root.step++) {
        case 0:
          mode.setText("graceful")
          field.forceActiveFocus(); field.selectAll(); field.requestPaste(false)
          root.started = Date.now()
          break
        case 1:
          check(Date.now() - root.started >= 3000 && field.text === "keep" && field.pasteError.indexOf("timed out") >= 0,
                "watchdog rejects a hung reader without editing the field")
          probeRecord('grep -q "^cleaned " "$1"')
          break
        case 2:
          check(probe.code === 0, "watchdog lets the reader finish SIGTERM cleanup before any SIGKILL")
          mode.setText("ignore")
          field.forceActiveFocus(); field.requestPaste(false)
          root.started = Date.now()
          break
        case 3:
          check(Date.now() - root.started >= 3900, "reader ignoring SIGTERM gets a grace period")
          probeRecord('pid=$(sed -n "s/^start //p" "$1" | tail -n 1); [ -n "$pid" ] && ! kill -0 "$pid" 2>/dev/null')
          break
        case 4:
          check(probe.code === 0, "watchdog kills a reader that ignores SIGTERM")
          console.log("OMACHAT_CLIPBOARD_WATCHDOG_PASS")
          Qt.quit(); return
        }
        restart()
      } catch (error) { console.error("OMACHAT_CLIPBOARD_WATCHDOG_FAIL", error); Qt.quit() }
    }
  }
}
