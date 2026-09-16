import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
 id: root
 property int step: 0
 property var sendRequest: null
 function check(value, label) { if (!value) throw new Error(label); console.log("PASS:", label) }
 function msg() { return {id:"m1",conversationID:"a",timestamp:Date.UTC(2026,8,1)*1000,fromMe:false,text:"Fixture"} }
 function take(method) {
  while(fake.requests.length) { var req=fake.requests.shift(); if(req.method===method) return req }
  throw new Error("Missing request: "+method)
 }
 function history() { take("messages").callback(true,{messages:[msg()],hasMore:false}) }
 QtObject {
  id: fake
  property string state: "connected"
  property bool connected: true
  property var status: ({phoneOK:true})
  property var statusTG: ({state:"connected"})
  property var conversations: [{id:"a",name:"Synthetic A"},{id:"b",name:"Synthetic B"}]
  property string refreshError: ""
  property var requests: []
  signal messageReceived(var message, var net)
  signal conversationUpdated(var conversation, var net)
  signal paired(var net)
  function call(method,params,callback,net) { requests.push({method:method,params:params,callback:callback,net:net}) }
  function loadConversations() {}
 }
 FloatingWindow {
  id: window
  implicitWidth:920;implicitHeight:580;visible:true
  Item {id: host;property bool opened:true}
  Chat.InboxView {id:inbox;anchors.fill:parent;service:fake;host:host}
  TestResult {id:inspect}
  TestEvent {id:keyboard}
 }
 Timer {
  running:true;repeat:true;interval:120
  onTriggered: {
   try {
    var list=inspect.findChild(inbox,"convList")
    var req
    switch(root.step++) {
    case 0:
     check(list!==null,"conversation list exists")
     window.contentItem.forceActiveFocus()
     list.currentIndex=0;list.forceActiveFocus()
     keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
     check(list.currentIndex===1,"Down moves the conversation cursor")
     keyboard.keyClick(Qt.Key_Up,Qt.NoModifier,0)
     check(list.currentIndex===0,"Up moves the conversation cursor")
     keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
     check(inbox.selectedConvID==="a","Enter opens focused conversation")
     history()
     break
    case 1:
     var composer=inspect.findChild(inbox,"composer")
     var send=inspect.findChild(inbox,"sendButton")
     composer.text="Pending fixture"
     send.forceActiveFocus()
     check(send.focusable && send.activeFocus,"Send has keyboard focus styling")
     keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)
     root.sendRequest=take("send")
     check(root.sendRequest.params.text==="Pending fixture","Space activates Send")
     inbox.selectConversation("b")
     take("messages").callback(true,{messages:[],hasMore:false})
     break
    case 2:
     root.sendRequest.callback(false,"Synthetic send failure")
     inbox.selectConversation("a");history()
     check(inbox.messages.some(function(m){return m.text==="Pending fixture" && m.failed}),"failed send survives switching conversations")
     inbox.sendMessage("Old account send");root.sendRequest=take("send")
     inbox.clearNetwork("gmessages")
     inbox.selectConversation("a");history()
     root.sendRequest.callback(true,{id:"old",tmpID:root.sendRequest.params.tmpID,conversationID:"a",text:"Old account send",fromMe:true})
     check(inbox.messages.length===1,"late success cannot restore a cleared account")
     check(!((inbox._pendingSends.gmessages || {}).a),"late success cannot restore old outbox")
     break
    case 3:
     inbox.pendingAttachment="/tmp/fictional-photo.png"
     inbox.sendAttachment("caption")
     req=take("sendMedia")
     inbox.network="telegram"
     inbox.selectConversation("a");history()
     check(!inbox.sendingMedia,"service switch releases upload UI")
     req.callback(true,{message:{id:"old-media",conversationID:"a",text:"old caption",attachments:[{key:"old-media-key"}]}})
     check(inbox.messages.length===1 && !inbox.mediaPaths["telegram:old-media-key"],"late upload cannot alter the new service")
     inbox.sendMessage("Other account send");root.sendRequest=take("send")
     inbox.network="gmessages";history()
     fake.statusTG={state:"unpaired"}
     root.sendRequest.callback(false,"late failure")
     check(!((inbox._pendingSends.telegram || {}).a),"clearing inactive service rejects its late failure")
     break
    case 4:
     inbox.requestOpenUrl("https://example.com")
     keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
     check(!inbox.linkConfirmOpen,"Escape cancels link confirmation")
     inbox.emojiPickerOpen=true
     break
    case 5:
     var emojiGrid=inspect.findChild(inbox,"emojiGrid")
     check(emojiGrid && emojiGrid.activeFocus,"emoji picker receives keyboard focus")
     keyboard.keyClick(Qt.Key_Right,Qt.NoModifier,0)
     keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
     check(!inbox.emojiPickerOpen && inspect.findChild(inbox,"composer").text.length>0,"keyboard selects an emoji")
     inbox._withMedia("tg:42:7","/tmp/fictional-telegram-photo")
     inbox._mediaNetworks=Object.assign({},inbox._mediaNetworks,{"tg:42:7":"telegram"})
     inbox.clearNetwork("telegram")
     check(!inbox.mediaPaths["tg:42:7"],"unpair clears Telegram image references")
     console.log("OMACHAT_REVIEW_PASS")
     stop();Qt.quit();break
    }
   } catch(e) {console.error("OMACHAT_REVIEW_FAIL",e.message,e.stack || e);stop();Qt.quit()}
  }
 }
 Timer {running:true;interval:5000;onTriggered:{console.error("OMACHAT_REVIEW_FAIL timeout");Qt.quit()}}
}
