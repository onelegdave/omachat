import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
 id: root
 property int step: 0
 property var anchor: null
 property var oldRequest: null
 property var otherRequest: null
 function check(value, label) { if (!value) throw new Error(label); console.log("PASS:", label) }
 function msg(n, conv) { return {id:"m"+n,conversationID:conv || "a",timestamp:Date.UTC(2026,8,1)*1000+n*3600000000,fromMe:false,text:"Message "+n+ (n%3===0 ? "\nA second line of text.\nAnother line." : "")} }
 function page(first,last) { var out=[];for(var n=first;n<=last;n++)out.push(msg(n));return out }
 function reply(messages,cursor,more) { return {messages:messages,cursorID:cursor,cursorTime:1234,hasMore:more} }
 function take() { var req=fake.requests.shift();check(!!req,"history request exists");return req }
 function sameAnchor(label) { var now=inbox.captureViewport();check(now && anchor && now.key===anchor.key && Math.abs(now.offset-anchor.offset)<1.5,label) }
 QtObject {
  id: fake
  property string state: "connected"
  property bool connected: true
  property var status: ({phoneOK:true})
  property var conversations: [{id:"a",name:"Synthetic A"},{id:"b",name:"Synthetic B"}]
  property string refreshError: ""
  property var requests: []
  signal messageReceived(var message)
  signal conversationUpdated(var conversation)
  signal paired()
  function call(method,params,callback) {
   if(method === "messages") { requests.push({params:params,callback:callback});return }
   if(method === "send") return
   if(callback)callback(true,{})
  }
  function loadConversations() {}
 }
 FloatingWindow {
  id: window
  implicitWidth:920;implicitHeight:580;visible:true
  Item {id: host;property bool opened:true}
  Chat.InboxView {id:inbox;anchors.fill:parent;service:fake;host:host}
  TestResult {id:inspect}
 }
 Timer {
  running:true;repeat:true;interval:100
  onTriggered: {
   try {
    var list=inspect.findChild(inbox,"messageList")
    var button=inspect.findChild(inbox,"loadOlderButton")
    var req
    switch(root.step++) {
    case 0:
     inbox.selectConversation("a")
     req=take();check(req.params.count===60 && !req.params.cursorID,"initial request has no cursor")
     req.callback(true,reply(page(100,159),"c100",true));break
    case 1:
     check(list.atYEnd,"initial history opens at newest message")
     list.positionViewAtIndex(15,ListView.Beginning);list.contentY+=11
     root.anchor=inbox.captureViewport();check(root.anchor!==null,"capture actual visible message")
     button.clicked();req=take()
     check(req.params.cursorID==="c100" && req.params.cursorTime===1234,"older request sends cursor pair")
     inbox.loadOlderMessages();check(fake.requests.length===0,"duplicate page request suppressed")
     req.callback(true,reply(page(40,100),"c40",true));break
    case 2:
     check(inbox.messages.length===120,"overlapping page merged without duplicate")
     sameAnchor("prepend preserves visible message and pixel offset")
     fake.messageReceived(msg(160));break
    case 3:
     sameAnchor("incoming message preserves position while reading history")
     inbox.refreshThread();take().callback(true,reply(page(100,160),"c100",true));break
    case 4:
     check(inbox.messages[0].id==="m40" && inbox.historyCursorID==="c40","refresh retains older history and oldest cursor")
     sameAnchor("refresh preserves reading position")
     inbox.loadOlderMessages();take().callback(false,"synthetic network failure")
     check(!inbox.loadingOlder && inbox.hasOlder && inbox.historyCursorID==="c40" && inbox.historyError!=="","failed older page can retry same cursor")
     inbox.loadOlderMessages();take().callback(true,reply([],"empty-next",true));break
    case 5:
     check(inbox.hasOlder && inbox.historyCursorID==="empty-next","empty page with advancing cursor can continue")
     inbox.loadOlderMessages();take().callback(true,reply([],"empty-next",true))
     check(!inbox.hasOlder && inbox.historyError!=="","repeated cursor stops further paging")
     inbox.loadOlderMessages();check(fake.requests.length===0,"stalled cursor cannot loop")
     inbox.refreshThread();take().callback(true,reply(page(100,160),"c100",true));break
    case 6:
     inbox.loadOlderMessages();root.oldRequest=take()
     inbox.refreshThread();take().callback(true,reply(page(100,160),"c100",true))
     root.oldRequest.callback(true,reply([msg(1)],"stale",true))
     check(!inbox.messages.some(function(m){return m.id==="m1"}) && inbox.historyCursorID!=="stale","refresh invalidates an in-flight older page")
     inbox.loadOlderMessages();root.oldRequest=take()
     inbox.selectConversation("b");root.otherRequest=take()
     inbox.selectConversation("a");take().callback(true,reply(page(100,159),"c100",true))
     root.oldRequest.callback(true,reply([msg(1)],"old-a",true))
     root.otherRequest.callback(true,reply([msg(2,"b")],"old-b",true))
     check(inbox.messages.length===60 && inbox.historyCursorID==="c100","A to B to A ignores old callbacks and resets pagination")
     break
    case 7:
     list.positionViewAtIndex(12,ListView.Beginning)
     root.anchor=inbox.captureViewport()
     inbox.loadOlderMessages();take().callback(true,reply(page(0,100),"",false));break
    case 8:
     sameAnchor("final page also preserves viewport")
     check(!inbox.hasOlder && inspect.findChild(inbox,"historyStatus").text==="All available history loaded","end of history is shown")
     inbox.sendMessage("Synthetic outgoing message")
     break
    case 9:
     check(list.atYEnd,"intentional send scrolls to newest message")
     inbox.selectConversation("b");take().callback(true,{messages:[],hasMore:false,historyNotice:"Encrypted history is not available yet."})
     check(inbox.historyNotice!=="" && inspect.findChild(inbox,"historyStatus").text===inbox.historyNotice,"encrypted history notice explains an empty thread")
     var shot=Quickshell.env("OMACHAT_PAGINATION_SCREENSHOT")
     if(shot) inbox.grabToImage(function(result){result.saveToFile(shot);console.log("OMACHAT_PAGINATION_PASS");Qt.quit()})
     else {console.log("OMACHAT_PAGINATION_PASS");Qt.quit()}
     stop();break
    }
   } catch(e) {console.error("OMACHAT_PAGINATION_FAIL",e.message, e.stack || e);Qt.quit()}
  }
 }
 Timer {running:true;interval:12000;onTriggered:{console.error("OMACHAT_PAGINATION_FAIL timeout");Qt.quit()}}
}
