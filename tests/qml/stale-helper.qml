import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat
import "Chat/Keybindings.js" as Keys

// A 0.6.0 panel next to a helper from an earlier release, which sends original
// downloaded avatars and turns them into notification icons. Only the socket
// is replaced: replies and events go through the real Service, UnifiedInbox,
// Avatar, DesktopNotifications and UpdateSettings. All data is invented.
ShellRoot {
  id: root
  property int step: 0
  property int received: 0
  property int messageSerial: 0
  property int waits: 0
  property var frames: []
  property var heldConversations: null
  readonly property string original: Quickshell.env("OMACHAT_TEST_IMAGE")
  readonly property string preview: Quickshell.env("OMACHAT_TEST_IMAGE_SECOND")
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function sent(method) { return frames.filter(function(frame) { return frame.method === method }) }
  function last(method) { var list = sent(method); return list.length ? list[list.length - 1] : null }
  function reply(frame, ok, result) {
    service._handleLine(JSON.stringify(ok ? {id:frame.id, ok:true, result:result} : {id:frame.id, ok:false, error:result}))
  }
  function event(name, data, notify) { service._handleLine(JSON.stringify({event:name, network:"telegram", data:data, notify:notify === true})) }
  function conversation(path) { return {id:"synthetic-chat", name:"Synthetic chat", initials:"SC", avatarPath:path, timestamp:10} }
  function message() { return {id:"synthetic-" + (++messageSerial), conversationID:"synthetic-chat", text:"Invented message", timestamp:Date.now() * 1000} }
  function images(item, path, found) {
    found = found || []
    if (item && item.source !== undefined && String(item.source) === "file://" + path) found.push(item)
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) images(children[i], path, found)
    return found
  }
  function connect() {
    service.connected = true
    service.connectionGeneration++
    event("status", {state:"connected", unread:0, phoneOK:true, qrURL:"", error:""})
    service.applyServiceConfig({enabledServices:["telegram"], notificationsEnabled:true, notificationPreviews:false})
    service.checkRunningBuild()
    return last("buildInfo")
  }
  function probeClosed(label) {
    var counts = ["conversations", "media", "notifyMessage", "testNotification", "startPairing"].map(function(method) { return sent(method).length })
    var receivedBefore = received
    event("conversation", conversation(original))
    event("message", message(), true)
    event("paired", {})
    service.loadConversations("telegram")
    var refusal = ""
    service.call("conversations", {count:50}, function(ok, res) { refusal = ok ? "accepted" : String(res) }, "telegram")
    service.call("media", {key:"synthetic"}, null, "telegram")
    service.call("startPairing", null, null, "telegram")
    service.notifications.test()
    var after = ["conversations", "media", "notifyMessage", "testNotification", "startPairing"].map(function(method) { return sent(method).length })
    check(JSON.stringify(after) === JSON.stringify(counts), label + ": no content request or alert reaches the helper")
    check(refusal !== "" && refusal !== "accepted", label + ": content requests fail with a reason")
    check(received === receivedBefore, label + ": live messages from the helper are not accepted")
    check(!service.conversationsTG.some(function(item) { return item.avatarPath === original }), label + ": the original avatar never enters the conversation model")
    var settings = sent("setUiScale").length
    service.call("setUiScale", {scale:1}, null, "telegram")
    check(sent("setUiScale").length === settings + 1, label + ": settings still reach the helper")
  }

  Chat.Service {
    id: service
    function rebuildSocket() {}
    function checkGo() {}
    function _write(frame) { root.frames = root.frames.concat([frame]); return true }
  }
  Connections { target: service; function onMessageReceived() { root.received++ } }
  FloatingWindow {
    id: window
    implicitWidth: 800; implicitHeight: 500; visible: true
    Column {
      anchors.fill: parent
      Chat.UnifiedInbox {
        id: inbox; width: parent.width; height: 380; service: service
        conversations: Keys.buildUnifiedConversations([{network:"telegram", conversations:service.conversationsTG}])
      }
      Chat.UpdateSettings { id: updates; service: service; width: parent.width }
    }
  }
  TestResult { id: inspect }
  Timer {
    interval: 150; repeat: true; running: true
    onTriggered: {
      try {
        check(root.images(window.contentItem, root.original).length === 0, "step " + root.step + ": no original avatar is loaded into the shell")
        if (root.step === 0) {
          if (service.updates.inspecting) return
          service.updates.expectedSourceID = "" // installed source could not be identified
          var first = root.connect()
          check(first && !service.contentAllowed && !service.helperBuildChecked, "remote content waits for the helper's build reply")
          root.probeClosed("before the helper answers")
          root.reply(first, true, {sourceID:"v055-no-preview-capability"})
          check(service.helperBuildChecked && !service.contentAllowed && service.helperNeedsRebuild, "a helper without the preview capability needs a rebuild even when the installed source is unknown")
          root.probeClosed("helper without the capability")
          var malformed = [{sourceID:"x", contentProtocol:false}, {contentProtocol:"safe-content-0"}, {contentProtocol:{}}, {contentProtocol:["safe-content-1"]}, {contentProtocol:"SAFE-CONTENT-1"}, null, "safe-content-1"]
          for (var i = 0; i < malformed.length; i++) {
            service.checkRunningBuild()
            root.reply(root.last("buildInfo"), true, malformed[i])
            check(!service.contentAllowed, "malformed capability " + JSON.stringify(malformed[i]) + " is refused")
          }
          service.checkRunningBuild()
          root.reply(root.last("buildInfo"), false, "unknown method")
          check(!service.contentAllowed && service.helperNeedsRebuild, "a failed build reply keeps content closed")
          root.probeClosed("after malformed and failed replies")
          var label = inspect.findChild(updates, "helperUpdateStatus")
          check(label && label.text.indexOf("earlier release") >= 0, "Settings explains that the helper must be rebuilt")
        } else if (root.step === 1) {
          service.checkRunningBuild()
          var earlier = root.last("buildInfo")
          service.connected = false
          check(!service.contentAllowed, "disconnecting closes content")
          var current = root.connect()
          root.reply(earlier, true, {sourceID:"current", contentProtocol:"safe-content-1"})
          check(!service.contentAllowed, "a capability reply from an earlier connection does not open this one")
          root.probeClosed("after a stale capability reply")
          root.reply(current, true, {sourceID:"current", contentProtocol:"safe-content-1"})
          check(service.contentAllowed && !service.helperNeedsRebuild, "a current helper that confirms previews opens content")
          var load = root.last("conversations")
          check(load && load.network === "telegram", "confirmation loads conversations for enabled services")
          root.reply(load, true, [root.conversation(root.preview)])
          check(service.conversationsTG.length === 1 && service.conversationsTG[0].avatarPath === root.preview, "helper previews fill the conversation model")
          var alerts = root.sent("notifyMessage").length
          root.event("message", root.message(), true)
          check(root.received === 1 && root.sent("notifyMessage").length === alerts + 1, "live messages and alerts resume with a confirmed helper")
          service.loadConversations("telegram")
          root.heldConversations = root.last("conversations")
        } else if (root.step === 2) {
          var shown = root.images(window.contentItem, root.preview)
          if (!shown.some(function(image) { return image.status === Image.Ready }) && ++root.waits < 30) return
          check(shown.some(function(image) { return image.status === Image.Ready }), "the real Avatar shows the helper preview")
          service.connected = false
          check(!service.contentAllowed && !service.helperNeedsRebuild, "an ordinary disconnect closes content without a rebuild notice")
          var reconnect = root.connect()
          check(!service.contentAllowed && !service.helperNeedsRebuild, "a reconnected helper must confirm again, with no rebuild notice while it is asked")
          var waiting = ""
          service.call("messages", {conversationID:"synthetic-chat"}, function(ok, res) { waiting = ok ? "accepted" : String(res) }, "telegram")
          check(waiting.indexOf("Checking the running helper") === 0, "a request in that moment explains the wait")
          root.reply(root.heldConversations, true, [root.conversation(root.original)])
          check(service.conversationsTG.length === 1 && service.conversationsTG[0].avatarPath === root.preview, "a content reply from an earlier connection is ignored")
          root.probeClosed("after reconnecting")
          root.reply(reconnect, true, {sourceID:"v055-no-preview-capability"})
          check(!service.contentAllowed && service.helperNeedsRebuild, "an earlier release taking over after a restart stays closed")
          root.probeClosed("earlier release after a restart")
        } else if (root.step === 3) {
          console.log("OMACHAT_STALE_HELPER_PASS")
          Qt.quit()
          return
        }
        root.step++
      } catch (error) { console.error("OMACHAT_STALE_HELPER_FAIL", error); Qt.quit() }
    }
  }
  Timer { interval: 15000; running: true; onTriggered: { console.error("OMACHAT_STALE_HELPER_FAIL timeout at step", root.step); Qt.quit() } }
}
