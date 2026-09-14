import QtQuick
import QtQuick.Window
import Quickshell
import qs.Commons
import "Chat" as Chat
import "Chat/Model.js" as Model

ShellRoot {
  id: root
  property int frame: 0
  property string artifactDir: Quickshell.env("OMACHAT_UI_ARTIFACTS")
  property string longHint: "Open the service on your phone and follow the pairing instructions. If a dependency is missing, you decide whether to install it yourself. This explanation must remain readable at larger text sizes, including a long configuration path: /home/demo/.config/omarchy/plugins/onelegdave.omachat/scripts/configure-telegram.py."
  QtObject {
    id: fake
    property var status: ({state:"error",error:"Pairing needs attention",hint:root.longHint})
    property string state: "error"
    property var browserProfiles: []
    function statusFor(net) { return status }
    function stateFor(net) { return state }
    function call(method, params, callback, network) {
      if(callback) callback(true,{uiScale:1.3,giphyKeySet:false})
    }
  }
  FloatingWindow {
    id: window
    implicitWidth: 640
    implicitHeight: 480
    visible: true
    color: Color.popups.background
    Rectangle {
      id: canvas
      color: Color.popups.background
      anchors.fill: parent
      Loader {
        id: page
        anchors.fill: parent
        anchors.margins: 20
        sourceComponent: root.frame % 4 === 0 ? pairing : settings
      }
    }
  }
  Component { id: pairing; Chat.PairingView { service:fake; network:"telegram"; uiScale:1.3 } }
  Component { id: settings; Chat.SettingsView { service:fake; uiScale:1.3 } }
  function check(ok, message) { if(!ok) throw new Error(message) }
  function inspect(item) {
    if(item.checked !== undefined && item.optionValue !== undefined) {
      check(Model.contrastRatio(item.foreground,item.color) >= 4.5,"choice text contrast on its actual fill")
    }
    if (item.text !== undefined && item.wrapMode !== undefined && item.visible && String(item.text).length > 0) {
      check(item.width > 0, "text has nonpositive width: " + item.text)
      if (String(item.text).length > 90) {
        check(!item.truncated, "long explanation was truncated: " + item.text)
        check(item.contentWidth <= item.width + 2, "long explanation overflows its width: " + item.text)
      }
    }
    var children = item.children || []
    for(var i=0;i<children.length;i++) inspect(children[i])
  }
  function findText(item, text) {
    if(item.text === text) return item
    var children = item.children || []
    for(var i=0;i<children.length;i++) { var match=findText(children[i],text); if(match) return match }
    return null
  }
  function palette() {
    var light = frame >= 4
    Color.foreground = light ? "#e0e0e0" : "#202028"
    Color.muted = light ? "#dddddd" : "#111118"
    Color.accent = light ? "#f4d4de" : "#40152f"
    Color.urgent = light ? "#efcccc" : "#351010"
    Color.shellValues = {"popups.background":light ? "#faf7f1" : "#08090f", "popups.text":light ? "#eeeeee" : "#171821"}
  }
  Timer { interval:800; running:true; onTriggered: { root.palette(); verify.restart() } }
  Timer {
    id: verify
    interval:200
    onTriggered: {
      try {
        root.check(!!page.item,"page loaded")
        root.check(Model.contrastRatio(page.item.foreground, Color.popups.background) >= 4.5,"page foreground contrast")
        root.inspect(page.item)
        if(root.frame % 4 >= 2) {
          var title=root.findText(page.item,root.frame % 4 === 2 ? "Dependencies and User Choice" : "Upstream Credits and Licenses")
          root.check(!!title,"settings section exists")
          page.item.jumpTo(title.parent)
        }
        if(root.artifactDir) {
          canvas.grabToImage(function(result) {
            root.check(result.saveToFile(root.artifactDir + "/" + (root.frame >= 4 ? "light-" : "dark-") + ["pairing","settings","dependencies","credits"][root.frame % 4] + ".png"), "screenshot saved")
            root.next()
          })
        } else root.next()
      } catch(error) { console.error("OMACHAT_READABILITY_FAIL",error); Qt.quit() }
    }
  }
  function next() {
    frame++
    if(frame === 8) { console.log("OMACHAT_READABILITY_PASS dark/light large-text setup, Settings, dependency checklist, selected choices, and credits"); Qt.quit(); return }
    palette()
    verify.restart()
  }
}
