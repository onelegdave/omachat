import QtQuick
import Quickshell
import Quickshell.Io
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0

  function check(ok, label) {
    if (!ok) throw new Error(label)
    console.log("PASS:", label)
  }

  function fail(message) {
    console.error("OMACHAT_POPOUT_FAIL", message)
    Qt.quit()
  }

  Chat.Panel {
    id: panel
    settingsOpen: true
  }

  Timer {
    interval: 50
    repeat: true
    running: true
    onTriggered: {
      try {
        if (root.step === 0) {
          root.check(!panel.popoutOpen && !panel.contentInPopout,
            "anchored panel remains the default")
          panel.openPopout()
          root.step = 1
        } else if (root.step === 1 && panel.appliedPopoutMode === "floating") {
          root.check(panel.popoutOpen && panel.contentInPopout,
            "one chat surface moves into the pop-out")
          root.check(panel.settingsOpen,
            "view state survives the move into the pop-out")
          clientCheck.expectedFloating = true
          clientCheck.command = ["hyprctl", "-j", "clients"]
          clientCheck.running = true
          root.step = 2
        } else if (root.step === 3 && panel.appliedPopoutMode === "tiled") {
          clientCheck.expectedFloating = false
          clientCheck.command = ["hyprctl", "-j", "clients"]
          clientCheck.running = true
          root.step = 4
        } else if (root.step === 5) {
          root.check(!panel.popoutOpen && !panel.contentInPopout && panel.opened,
            "Return to panel restores the anchored surface")
          root.check(panel.settingsOpen,
            "view state survives the return to the panel")
          panel.close()
          console.log("OMACHAT_POPOUT_PASS hidden default, shared surface, floating, tiled, and return")
          Qt.quit()
        }
      } catch (error) {
        root.fail(String(error))
      }
    }
  }

  Process {
    id: clientCheck
    property bool expectedFloating: false
    stdout: StdioCollector { id: clientsStdout; waitForEnd: true }
    stderr: StdioCollector { id: clientsStderr; waitForEnd: true }
    onExited: function(code) {
      try {
        root.check(code === 0, "Hyprland client query succeeds")
        var clients = JSON.parse(String(clientsStdout.text || "[]"))
        var match = null
        for (var i = 0; i < clients.length; i++) {
          if (String(clients[i].address) === panel.popoutAddress) {
            match = clients[i]
            break
          }
        }
        root.check(match !== null, "pop-out has an exact Hyprland address")
        root.check(match.floating === expectedFloating,
          expectedFloating ? "Floating applies to the pop-out" : "Tiled applies to the pop-out")
        if (expectedFloating) {
          root.check(match.size[0] === panel.preferredPopoutWidth
              && match.size[1] === panel.preferredPopoutHeight,
            "Floating restores the intended pop-out size")
        }
        if (expectedFloating) {
          panel.setPopoutMode("tiled")
          root.step = 3
        } else {
          panel.returnToPanel()
          root.step = 5
        }
      } catch (error) {
        root.fail(String(error) + " " + String(clientsStderr.text || ""))
      }
    }
  }

  Timer {
    interval: 10000
    running: true
    onTriggered: root.fail("timed out at step " + root.step + ": " + panel.popoutModeError)
  }
}
