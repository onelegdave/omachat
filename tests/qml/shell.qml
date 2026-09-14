import QtQuick
import QtQuick.Window
import QtTest
import Quickshell
import Quickshell.Io
import qs.Ui
import "Chat" as Chat

ShellRoot {
 id: root
 property bool passed: false
 property string testImage: Quickshell.env("OMACHAT_TEST_IMAGE")
 function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
 QtObject {
  id: fake
  property bool connected: true
  property string state: "connected"
  property var status: ({phoneOK:true})
  property int unread: 0
  property string refreshError: ""
  property var browserProfiles: []
  property var conversations: [
   {id:"a",name:"Demo Alice",preview:"Test conversation",timestamp:1000000},
   {id:"b",name:"Demo Bob",preview:"Another test",timestamp:2000000}
  ]
  property var calls: []
  property var delayed: []
  property bool failMedia: true
  signal messageReceived(var message)
  signal conversationUpdated(var conversation)
  signal paired()
  function loadConversations() {}
  function loadProfiles() {}
  function refreshConversations() { calls.push({method:"refresh"}) }
  function call(method, params, callback) {
   calls.push({method:method, params:params})
   if (method === "send" || method === "sendMedia" || method === "pickImage" || method === "react" || method === "unpair") {
    delayed.push({method:method,params:params,callback:callback}); return
   }
   if (!callback) return
   if (method === "messages") callback(true, {messages:[]})
   else if (method === "media") callback(!failMedia, failMedia ? "temporary failure" : {path:root.testImage})
   else if (method === "config") callback(true,{uiScale:1,giphyKeySet:false})
   else callback(true, {})
  }
 }
 FloatingWindow {
  id: window
  implicitWidth: 920; implicitHeight: 580; visible: true
  Item { id: host; property bool opened: true }
  PanelKeyCatcher {
   id: catcher
   anchors.fill: parent
   property int shortcuts: 0
   onTextKey: shortcuts++
   Chat.InboxView { id: inbox; anchors.fill: parent; service: fake; host: host }
  }
  TestEvent { id: keyboard }
  TestResult { id: inspect }
 }
 Chat.Panel { id: panel; service: fake }
 Chat.SettingsView { id: settings; visible: false; service: fake }
 Process { command: ["sleep", "0.5"]; running: true; onExited: root.runTests() }
 function runTests() {
   try {
    console.log("QML_TEST_BEGIN")
    var composer = inspect.findChild(inbox,"composer")
    var caption = inspect.findChild(inbox,"attachCaption")
    var search = inspect.findChild(inbox,"searchField")
    root.check(composer && caption && search,"real inbox editors load")
    inbox.selectConversation("a")
    composer.text="Alice draft"
    caption.text="Alice caption"
    inbox.selectConversation("b")
    root.check(composer.text === "" && caption.text === "", "switching recipient clears unrelated draft and caption")
    composer.text="Bob draft"
    inbox.selectConversation("a")
    root.check(composer.text === "Alice draft", "returning to a recipient restores their draft")

    inbox.attachFromDisk()
    inbox.selectConversation("b")
    fake.delayed.pop().callback(true,{path:"/tmp/alice.png"})
    root.check(inbox.pendingAttachment === "", "late file picker result cannot move to another recipient")

    inbox.react("message-in-b", "thumbs-up")
    var reaction = fake.delayed.pop()
    inbox.selectConversation("a")
    reaction.callback(false, "Reaction failed")
    root.check(inbox.threadError === "", "late reaction failure cannot leak into another conversation")
    inbox.selectConversation("b")

    inbox.sendMessage("OK")
    var pending = fake.delayed.pop()
    inbox.refreshThread()
    root.check(inbox.messages.length === 1 && inbox.messages[0].tmpID === pending.params.tmpID, "refresh preserves an in-flight send")
    root.check(!fake.calls.some(function(c){return c.method === "markRead" && c.params.messageID === pending.params.tmpID}), "read receipts never use a local provisional message ID")
    fake.messageReceived({id:"incoming",conversationID:"b",text:"OK",fromMe:false,timestamp:1})
    root.check(inbox.messages.length === 2, "incoming identical text preserves outgoing pending bubble")
    var sent={id:"server",tmpID:pending.params.tmpID,conversationID:"b",text:"OK",fromMe:true,timestamp:2,pending:false}
    fake.messageReceived(sent)
    pending.callback(true,{id:sent.tmpID,tmpID:sent.tmpID,conversationID:"b",text:"OK",fromMe:true,timestamp:3,pending:true,provisional:true})
    root.check(inbox.messages.length === 2 && inbox.messages[1].id === "server", "late acknowledgement preserves real message identity")

    inbox.requestMedia("image")
    fake.failMedia=false
    inbox.requestMedia("image")
    root.check(inbox.mediaPaths.image === root.testImage, "failed media retries successfully")

    inbox.selectConversation("a")
    composer.text=""
    inbox.pendingAttachment=root.testImage
    caption.text="Separate caption"
    inbox.sendAttachment(caption.text)
    var attachmentSend=fake.delayed.pop()
    var delayedCount=fake.delayed.length
    inbox.sendAttachment(caption.text)
    root.check(fake.delayed.length === delayedCount, "an in-flight attachment cannot be submitted twice")
    var imageEcho={id:"image-server",tmpID:attachmentSend.params.tmpID,conversationID:"a",fromMe:true,deleted:false,timestamp:10,attachments:[{key:"image-key"}]}
    var captionEcho={id:"caption-server",tmpID:"caption-tx",conversationID:"a",fromMe:true,timestamp:11,text:"Separate caption"}
    fake.messageReceived(imageEcho)
    fake.messageReceived(captionEcho)
    attachmentSend.callback(true,{
     message:{id:attachmentSend.params.tmpID,tmpID:attachmentSend.params.tmpID,conversationID:"a",fromMe:true,deleted:false,timestamp:12,provisional:true,pending:true,attachments:[{key:"image-key"}]},
     captionMessage:{id:"caption-tx",tmpID:"caption-tx",conversationID:"a",fromMe:true,timestamp:13,text:"Separate caption",provisional:true,pending:true}
    })
    root.check(inbox.pendingAttachment === "" && inbox.messages.length === 2, "split attachment acknowledgement clears the staged file and keeps two messages")
    root.check(inbox.messages[0].id === "image-server" && inbox.messages[1].id === "caption-server", "split send acknowledgements preserve earlier phone echoes")

    inbox.pendingAttachment=root.testImage
    caption.text="Keep this caption"
    inbox.sendAttachment(caption.text)
    var partial=fake.delayed.pop()
    partial.callback(true,{message:{id:partial.params.tmpID,tmpID:partial.params.tmpID,conversationID:"a",fromMe:true,deleted:false,timestamp:20,pending:true,provisional:true,attachments:[{key:"partial-image"}]},captionError:"Phone timeout"})
    root.check(inbox.pendingAttachment === "" && composer.text === "Keep this caption", "caption failure retains only text for retry, never the accepted image")
    root.check(inbox.threadError.indexOf("Attachment submitted") >= 0 && inbox.threadError.indexOf("Phone timeout") >= 0, "partial send failure clearly reports that the image was submitted")

    inbox.pendingAttachment=root.testImage
    caption.text="Unsent caption"
    inbox.sendAttachment(caption.text)
    fake.delayed.pop().callback(false,"Image rejected")
    root.check(inbox.pendingAttachment === root.testImage && caption.text === "Unsent caption", "image failure preserves the attachment and caption for retry")
    inbox.pendingAttachment=""
    inbox.threadError=""
    composer.text=""

    window.contentItem.forceActiveFocus()
    search.forceActiveFocus()
    search.text=""
    var text="r123 jkl hx"
    for (var i=0;i<text.length;i++) keyboard.keyClickChar(text[i], Qt.NoModifier, 0)
    root.check(search.text === text && catcher.shortcuts === 0, "real search editor receives shortcut characters")
    inbox.pendingAttachment=root.testImage
    caption.forceActiveFocus()
    caption.text=""
    for (var j=0;j<text.length;j++) keyboard.keyClickChar(text[j], Qt.NoModifier, 0)
    root.check(caption.text === text && catcher.shortcuts === 0, "real attachment caption receives shortcut characters")
    inbox.pendingAttachment=""
    settings.parent=catcher
    settings.width=920; settings.height=580; settings.visible=true
    var key=inspect.findChild(settings,"keyField")
    key.forceActiveFocus()
    key.text=""
    for (var k=0;k<text.length;k++) keyboard.keyClickChar(text[k], Qt.NoModifier, 0)
    root.check(key.text === text && catcher.shortcuts === 0, "real Settings key editor receives shortcut characters")
    settings.visible=false


    var loader=inspect.findChild(panel,"inboxLoader")
    var original=loader.item
    original.selectConversation("a")
    inspect.findChild(original,"composer").text="Keep across settings"
    panel.settingsOpen=true
    root.check(loader.item === original && !loader.visible, "Settings hides and retains the inbox")
    panel.settingsOpen=false
    root.check(loader.item === original && inspect.findChild(original,"composer").text === "Keep across settings", "Settings preserves selection and draft")
    panel.refresh()
    root.check(fake.calls.some(function(c){return c.method === "refresh"}), "panel refresh requests a network refresh")
    fake.refreshError="Refresh failed"
    original.threadError=""
    root.check(inspect.findChild(original,"threadErrorLabel").text === "Refresh failed", "thread success does not erase inbox refresh failure")
    fake.refreshError=""
    root.check(inspect.findChild(original,"threadErrorLabel").text === "", "next refresh clears the previous failure")
    var unpairButton=inspect.findChild(panel,"unpairButton")
    panel.settingsOpen=true
    unpairButton.clicked()
    root.check(panel.unpairing && !panel.settingsOpen, "unpair waits for its result and leaves Settings")
    var unpairCall=fake.delayed.pop()
    var beforeDuplicate=fake.calls.length
    panel.unpair()
    root.check(fake.calls.length === beforeDuplicate, "pending unpair cannot be submitted twice")
    var warning="Local credentials and cached files were cleared. Google device revocation could not be confirmed. Remove OmaChat in Google Messages on your phone under Device pairing."
    fake.state="unpaired"
    fake.status={state:"unpaired",error:warning}
    unpairCall.callback(false,warning)
    root.check(loader.item === null, "unpair destroys account UI state")
    var warningLabel=inspect.findChild(panel,"unpairWarningText")
    root.check(warningLabel && warningLabel.text === warning && warningLabel.wrapMode === Text.WordWrap, "unpaired screen retains remote revocation warning and phone instructions")
    root.check(!panel.unpairing, "failed unpair releases pending state")

    fake.state="connected"
    fake.status={phoneOK:true}
    panel.unpair()
    unpairCall=fake.delayed.pop()
    unpairCall.callback(false,"Disconnected from omachatd")
    root.check(inspect.findChild(panel,"unpairErrorLabel").text.indexOf("Disconnected from omachatd") >= 0, "unpair transport failure is visible instead of silently dropped")

    panel.unpair()
    unpairCall=fake.delayed.pop()
    fake.state="gaiaPairing"
    fake.status={state:"gaiaPairing",emoji:"synthetic"}
    unpairCall.callback(false,warning)
    root.check(panel.unpairError === "" && !panel.unpairing, "late unpair reply cannot overwrite a newer pairing")

    fake.state="connected"
    fake.status={phoneOK:true}
    panel.unpair()
    unpairCall=fake.delayed.pop()
    fake.state="unpaired"
    fake.status={state:"unpaired"}
    unpairCall.callback(true,null)
    root.check(panel.unpairError === "" && !panel.unpairing, "successful unpair leaves no stale warning")
    var screenshot=Quickshell.env("OMACHAT_TEST_SCREENSHOT")
    if (screenshot) {
     inbox.grabToImage(function(image) {
      if (!image.saveToFile(screenshot)) console.error("OMACHAT_QML_FAIL screenshot")
      console.log("OMACHAT_QML_PASS")
      Qt.quit()
     })
    } else { console.log("OMACHAT_QML_PASS");Qt.quit() }
    root.passed=true
   } catch(e) { console.error("OMACHAT_QML_FAIL",e.stack || e); Qt.quit() }
 }
 Timer { running: true; interval: 10000; onTriggered: {console.error("OMACHAT_QML_FAIL timeout");Qt.quit()} }
}
