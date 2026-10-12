import QtQuick
import QtTest
import Quickshell
import Quickshell.Io
import "Chat" as Chat

ShellRoot {
  id: root
  readonly property string link: "https://example.invalid/pair#invented-pairing-secret"
  property int ticks: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  QtObject {
    id: fake
    property string state: "pairing"
    property var status: ({state: "pairing", qrURL: root.link})
    property var browserProfiles: []
    function statusFor(net) { return status }
    function stateFor(net) { return state }
    function call(method, params, callback, network) {}
    function loadProfiles() {}
  }
  FloatingWindow {
    implicitWidth: 500; implicitHeight: 700; visible: true
    Chat.PairingView { id: pairing; anchors.fill: parent; service: fake; network: "gmessages" }
  }
  FileView { id: argv; path: Quickshell.env("OMACHAT_QR_RECORD") + ".argv"; blockLoading: true; printErrors: false }
  FileView { id: input; path: Quickshell.env("OMACHAT_QR_RECORD") + ".stdin"; blockLoading: true; printErrors: false }
  Timer {
    interval: 100; running: true; repeat: true
    onTriggered: {
      try {
        if (++root.ticks > 60) throw new Error("qrencode did not finish; stdin was not closed")
        if (pairing.qrRevision < 1) return
        stop()
        argv.reload(); input.reload()
        check(pairing.qrError === "", "QR rendering succeeds")
        check(argv.text().indexOf("invented-pairing-secret") < 0 && argv.text().split("\n")[0] === "-o", "the pairing link is not in qrencode's command line")
        check(input.text() === root.link, "qrencode reads the exact pairing link from stdin")
        console.log("OMACHAT_PAIRING_QR_PASS")
      } catch (error) { stop(); console.error("OMACHAT_PAIRING_QR_FAIL", error) }
      Qt.callLater(Qt.quit)
    }
  }
}
