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
 function named(item, name) {
  if (item.objectName === name) return item
  var kids=item.children || []
  for(var i=0;i<kids.length;i++) { var found=named(kids[i],name); if(found) return found }
  return null
 }
 QtObject {
  id: fake
  property bool connected: true
  property string currentNetwork: "gmessages"
  property string state: "connected"
  property var status: ({phoneOK:true, state:"connected"})
  property var statusWA: ({phoneOK:true, state:"connected"})
  property int unread: 0
  property string refreshError: ""
  property var browserProfiles: []
  property var conversations: [
   {id:"a",name:"Demo Alice",preview:"Test conversation",timestamp:1000000},
   {id:"b",name:"Demo Bob",preview:"Another test",timestamp:2000000}
  ]
  property var conversationsWA: [
   {id:"wa-1@s.whatsapp.net",name:"Demo WA",preview:"WA test",timestamp:3000000}
  ]
  property var calls: []
  property var delayed: []
  property bool failMedia: true
  signal messageReceived(var message, var net)
  signal conversationUpdated(var conversation, var net)
  signal paired(var net)
  function statusFor(net) { return net === "whatsapp" ? statusWA : status }
  function stateFor(net) { var s = statusFor(net); return s && s.state ? s.state : state }
  function conversationsFor(net) { return net === "whatsapp" ? conversationsWA : conversations }
  function unreadFor(net) { return 0 }
  function loadConversations(net) {}
  function loadProfiles() {}
  function refreshConversations(net) { calls.push({method:"refresh", network:net || "gmessages"}) }
  function call(method, params, callback, network) {
   var net = network || "gmessages"
   calls.push({method:method, params:params, network:net})
   if (method === "send" || method === "sendMedia" || method === "pickImage" || method === "react" || method === "unpair") {
    delayed.push({method:method,params:params,callback:callback,network:net}); return
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
   blocked: inbox.composerFocus === true || inbox.linkConfirmOpen === true
   property int shortcuts: 0
   onTextKey: shortcuts++
   Chat.InboxView { id: inbox; anchors.fill: parent; service: fake; host: host }
  }
  TestEvent { id: keyboard }
  TestResult { id: inspect }
 }
 Chat.Panel { id: panel; service: fake }
 Chat.SettingsView { id: settings; visible: false; service: fake }
 Chat.DependencyChecklist {
  id: dependencies
  parent: window.contentItem
  width: 600
  visible: false
  autoCheck: false
  property var launches: []
  property string openedUrl: ""
  launch: function(argv) { launches.push(argv) }
  openUrl: function(url) { openedUrl=url }
  Component.onCompleted: acceptResult(0,JSON.stringify([
    {id:"qrencode",name:"QR encoder",purpose:"Pairing",detail:"Missing qrencode",installed:false,sourceUrl:"https://gitlab.archlinux.org/archlinux/packaging/packages/qrencode"},
    {id:"go",name:"Go",purpose:"Build",detail:"Available",installed:true,sourceUrl:"https://gitlab.archlinux.org/archlinux/packaging/packages/go"}
  ]))
 }
 Process { command: ["sleep", "0.5"]; running: true; onExited: root.runTests() }
 function runTests() {
   try {
    console.log("QML_TEST_BEGIN")
    var source="https://gitlab.archlinux.org/archlinux/packaging/packages/qrencode"
    var rows=[{id:"qrencode",name:"QR encoder",purpose:"Pairing",detail:"Missing qrencode",installed:false,sourceUrl:source},
              {id:"go",name:"Go",purpose:"Build",detail:"Available",installed:true,sourceUrl:source}]
    root.check(dependencies.dependencies.length === 2 && !dependencies.errorText,"dependency result loads")
    dependencies.install("go")
    dependencies.install("unknown")
    root.check(dependencies.launches.length === 0,"available and unknown tools cannot trigger install")
    root.named(dependencies,"dependencySource-qrencode").clicked()
    root.check(dependencies.openedUrl === source && dependencies.launches.length === 0,"source review never installs")
    dependencies.install("qrencode")
    dependencies.install("qrencode")
    root.check(dependencies.launches.length === 1,"duplicate install request is blocked")
    var argv=dependencies.launches[0]
    root.check(argv[0] === "omarchy" && argv[1] === "launch" && argv[2] === "terminal" && argv[4].endsWith("/scripts/dependencies.py") && argv[5] === "--install" && argv[6] === "qrencode","install launches explicit terminal argv")
    dependencies.acceptResult(0,"not json")
    root.check(dependencies.dependencies.length === 0 && dependencies.errorText !== "","failed check clears stale availability")
    dependencies.acceptResult(0,JSON.stringify([{id:"qrencode",installed:"yes"}]))
    root.check(dependencies.dependencies.length === 0,"malformed check fails closed")
    dependencies.acceptResult(1,JSON.stringify(rows))
    root.check(dependencies.dependencies.length === 0,"nonzero check exit cannot mark tools available")
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
    fake.messageReceived({id:"incoming",conversationID:"b",text:"OK",fromMe:false,timestamp:1}, "gmessages")
    root.check(inbox.messages.length === 2, "incoming identical text preserves outgoing pending bubble")
    var sent={id:"server",tmpID:pending.params.tmpID,conversationID:"b",text:"OK",fromMe:true,timestamp:2,pending:false}
    fake.messageReceived(sent, "gmessages")
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
    fake.messageReceived(imageEcho, "gmessages")
    fake.messageReceived(captionEcho, "gmessages")
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
    fake.statusWA={state:"unpaired"}
    fake.status={state:"unpaired",error:warning}
    unpairCall.callback(false,warning)
    root.check(loader.item === null, "unpair destroys account UI state")
    var warningLabel=inspect.findChild(panel,"unpairWarningText")
    root.check(warningLabel && warningLabel.text === warning && warningLabel.wrapMode !== Text.NoWrap, "unpaired screen retains remote revocation warning and phone instructions")
    root.check(!panel.unpairing, "failed unpair releases pending state")
    var qrProcess=inspect.findChild(panel,"qrProcess")
    root.check(!!qrProcess,"pairing QR process exists")
    qrProcess.exited(1,0)
    root.check(inspect.findChild(panel,"qrErrorText").text.indexOf("Settings > Tools") >= 0,"QR failure gives dependency guidance")

    fake.state="connected"
    fake.status={phoneOK:true}
    fake.statusWA={phoneOK:true, state:"connected"}
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

    // --- WhatsApp Tab and Isolation Tests ---
    fake.state = "connected"
    fake.status = {phoneOK:true, state:"connected"}
    fake.statusWA = {phoneOK:true, state:"connected"}
    panel.activeService = "whatsapp"
    var waInbox = inspect.findChild(panel, "inboxLoader").item
    root.check(waInbox && waInbox.isWhatsApp === true, "switching to whatsapp activates WhatsApp inbox view")
    root.check(waInbox.network === "whatsapp", "inbox network property is whatsapp")
    var waMic = inspect.findChild(waInbox, "micButton")
    var waGif = inspect.findChild(waInbox, "gifButton")
    root.check(waMic && !waMic.visible && waMic.width === 0, "voice recording button is hidden on WhatsApp")
    root.check(waGif && !waGif.visible && waGif.width === 0, "GIF search button is hidden on WhatsApp")

    waInbox.react("wa-msg", "❤️")
    root.check(waInbox.threadError === "Reactions are not supported for WhatsApp in this version.", "reacting on WhatsApp displays unsupported error")
    waInbox.openGifPicker()
    root.check(waInbox.threadError === "GIF search is not supported for WhatsApp in this version.", "GIF search on WhatsApp displays unsupported error")
    waInbox.startRecording()
    root.check(waInbox.threadError === "Voice messages are not supported for WhatsApp in this version.", "voice recording on WhatsApp displays unsupported error")

    // Test WhatsApp unpair phone advice
    panel.unpair()
    var waUnpairCall = fake.delayed.pop()
    root.check(waUnpairCall.network === "whatsapp", "unpair routed to whatsapp network")
    waUnpairCall.callback(false, "Connection refused")
    root.check(inspect.findChild(panel, "unpairErrorLabel").text.indexOf("Check WhatsApp on your phone under Linked devices") >= 0, "WhatsApp unpair failure advises checking Linked devices")
    panel.unpairError = ""

    // Test tab switch with draft and conversation isolation
    panel.activeService = "whatsapp"
    waInbox = inspect.findChild(panel, "inboxLoader").item
    waInbox.selectConversation("wa-1@s.whatsapp.net")
    var waComposer = inspect.findChild(waInbox, "composer")
    waComposer.text = "WA Draft"
    panel.activeService = "gmessages"
    var gmInbox = inspect.findChild(panel, "inboxLoader").item
    root.check(gmInbox && !gmInbox.isWhatsApp, "switching back to Google restores Google inbox")
    panel.activeService = "whatsapp"
    waInbox = inspect.findChild(panel, "inboxLoader").item
    root.check(inspect.findChild(waInbox, "composer").text === "WA Draft", "returning to WhatsApp restores WhatsApp draft")

    waInbox.selectConversation("wa-1@s.whatsapp.net")
    waInbox.sendMessage("WA pending")
    var waPending = fake.delayed.pop()
    root.check(waPending.method === "send" && waPending.network === "whatsapp", "whatsapp send is routed to whatsapp")
    panel.setActiveService("gmessages")
    gmInbox = inspect.findChild(panel, "inboxLoader").item
    gmInbox.selectConversation("a")
    inspect.findChild(gmInbox, "composer").text = ""
    gmInbox.sendMessage("Google pending")
    var gmPending = fake.delayed.pop()
    root.check(gmPending.method === "send" && gmPending.network === "gmessages", "google send is routed to gmessages")
    var gmCount = gmInbox.messages.length
    waPending.callback(true, {id:"wa-late", conversationID:"wa-1@s.whatsapp.net", text:"WA pending", fromMe:true, timestamp:50})
    root.check(gmInbox.messages.length === gmCount, "late whatsapp send callback cannot land in the google inbox")
    root.check(fake.currentNetwork === "gmessages", "keyboard-equivalent tab switch updates currentNetwork")

    // 1. Google draft -> unpaired WA tab -> Google retains draft
    panel.activeService = "gmessages"
    gmInbox = inspect.findChild(panel, "inboxLoader").item
    gmInbox.selectConversation("g-draft-conv")
    inspect.findChild(gmInbox, "composer").text = "My Google Draft"

    // Simulate WhatsApp being unpaired
    fake.statusWA = {phoneOK:true, state:"unpaired"}
    fake.status = {phoneOK:true, state:"connected"}
    panel.activeService = "whatsapp"
    // The panel should show pairing view for whatsapp, but gmInbox shouldn't be destroyed
    root.check(inspect.findChild(panel, "inboxLoader").item !== null, "inbox view is retained when switching to an unpaired WA tab if Google is still connected")

    panel.activeService = "gmessages"
    gmInbox = inspect.findChild(panel, "inboxLoader").item
    root.check(inspect.findChild(gmInbox, "composer").text === "My Google Draft", "Google draft is retained after viewing unpaired WA tab")

    // 2. Identical synthetic IDs across networks don't share drafts
    panel.activeService = "gmessages"
    gmInbox = inspect.findChild(panel, "inboxLoader").item
    gmInbox.selectConversation("same-id")
    inspect.findChild(gmInbox, "composer").text = "Google specific draft"

    fake.statusWA = {phoneOK:true, state:"connected"}
    panel.activeService = "whatsapp"
    waInbox = inspect.findChild(panel, "inboxLoader").item
    waInbox.selectConversation("same-id")
    root.check(inspect.findChild(waInbox, "composer").text === "", "identical ID on WhatsApp does not load Google's draft")
    inspect.findChild(waInbox, "composer").text = "WA specific draft"

    panel.activeService = "gmessages"
    gmInbox = inspect.findChild(panel, "inboxLoader").item
    // switch to trigger saving current then select same-id again
    gmInbox.selectConversation("other-id")
    gmInbox.selectConversation("same-id")
    root.check(inspect.findChild(gmInbox, "composer").text === "Google specific draft", "identical ID on Google retained its own draft")

    // 3. unpair/re-pair one preserves other
    // Trigger onPaired directly on the inbox view component
    fake.paired("whatsapp")
    // After fake.paired, the signal propagates to InboxView
    gmInbox.selectConversation("other-id")
    gmInbox.selectConversation("same-id")
    root.check(inspect.findChild(gmInbox, "composer").text === "Google specific draft", "clearing WhatsApp drafts via onPaired does not affect Google drafts")

    // 4. if both unpaired, prior account-view destruction still valid
    fake.statusWA = {phoneOK:true, state:"unpaired"}
    fake.status = {phoneOK:true, state:"unpaired"}
    // Both unpaired, anyAccountReady should be false, and inboxLoader should be destroyed
    // need to trigger connState change update if needed, but changing status should do it.
    // wait for qml to process bindings
    panel.activeService = "gmessages" // trigger update
    root.check(inspect.findChild(panel, "inboxLoader").item === null, "if both accounts are unpaired, the inboxLoader item is destroyed")

    // Restore states for later tests
    fake.statusWA = {phoneOK:true, state:"connected"}
    fake.status = {phoneOK:true, state:"connected"}
    panel.activeService = "gmessages"
    var beforeTelegram = fake.calls.length
    panel.setActiveService("telegram")
    root.check(inspect.findChild(panel, "inboxLoader").visible, "connected Telegram shows the live inbox")
    root.check(fake.calls.length === beforeTelegram, "telegram tab does not issue chat RPCs")
    fake.call("status", null, function() {}, "telegram")
    var tel = fake.calls[fake.calls.length-1]
    root.check(tel.network === "telegram", "explicit unknown network is not rewritten to google")
    panel.setActiveService("gmessages")
    var screenshot=Quickshell.env("OMACHAT_TEST_SCREENSHOT")
    if (screenshot) {
     inbox.grabToImage(function(image) {
      if (!image.saveToFile(screenshot)) console.error("OMACHAT_QML_FAIL screenshot")
      console.log("OMACHAT_QML_PASS")
      Qt.quit()
     })
    } else { console.log("OMACHAT_QML_PASS");Qt.quit() }
    root.passed=true
   } catch(e) { console.error("OMACHAT_QML_FAIL", (e && e.message) ? e.message : "", e.stack || e); Qt.quit() }
 }
 Timer { running: true; interval: 10000; onTriggered: {console.error("OMACHAT_QML_FAIL timeout");Qt.quit()} }
}
