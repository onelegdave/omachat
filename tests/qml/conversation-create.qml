import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
 id: root
 function check(ok,label) { if (!ok) throw new Error(label); console.log("PASS:",label) }
 QtObject {
  id: fake
  property bool connected: true
  property string state: "connected"
  property string refreshError: ""
  property var pending: []
  property var calls: []
  property var conversations: [{id:"old",name:"Existing chat",timestamp:1,unread:false}]
  signal messageReceived(var message,string network)
  function stateFor(net) { return state }
  function conversationsFor(net) { return conversations }
  function loadConversations(net) { calls.push({method:"reload",network:net}) }
  function call(method,params,callback,network) {
   calls.push({method:method,params:params,network:network})
   if (method === "messages") { callback(true,{messages:[]});return }
   if (method === "conversationTargets" || method === "createConversation") pending.push({method:method,callback:callback})
  }
 }
 FloatingWindow {
  implicitWidth:920;implicitHeight:580;visible:true
  Item { id:host;property bool opened:true }
  Chat.InboxView { id:inbox;anchors.fill:parent;host:host;service:fake;network:"telegram" }
  TestEvent {id:keyboard}
  TestResult {id:inspect}
 }
 property int step:0
 Timer {
  interval:100;repeat:true;running:true
  onTriggered: {
   try {
    if (root.step === 0) {
     inbox.openNewChat();var old=fake.pending.shift().callback
     inbox.newChatOpen=false
     inbox.openNewChat()
     root.check(fake.pending.length===1,"closing a loading picker permits a fresh request")
     old(true,[{id:"wrong",name:"Old response"}])
     root.check(inbox.newChatLoading && inbox.newChatTargets.length===0,"old picker response cannot replace fresh contacts")
     fake.pending.shift().callback(true,[{id:"alice",name:"Alice"},{id:"bob",name:"Bob"}])
     inbox.newChatOpen=false
     inbox.openNewChat();old=fake.pending.shift().callback
     inbox.network="whatsapp";inbox.network="telegram";inbox.openNewChat()
     old(false,"Old service error")
     root.check(inbox.newChatLoading && inbox.newChatError==="","service round trip cannot revive an old callback")
     fake.pending.shift().callback(true,[{id:"alice",name:"Alice"},{id:"bob",name:"Bob"}])
     inbox.toggleNewChatTarget("alice");inbox.createNewChat()
     var count=fake.calls.filter(function(c){return c.method==="createConversation"}).length
     inbox.createNewChat()
     root.check(fake.calls.filter(function(c){return c.method==="createConversation"}).length===count,"busy picker cannot create duplicate remote chats")
     old=fake.pending.shift().callback
     inbox.newChatOpen=false;inbox.selectConversation("old")
     old(true,{id:"late-created",name:"Late result"})
     root.check(inbox.selectedConvID==="old" && !inbox.newChatOpen,"closed picker cannot steal the conversation selection")
     inbox.openNewChat();old=fake.pending.shift().callback
     inbox.service=null;inbox.service=fake;inbox.openNewChat()
     old(false,"Replaced service failed")
     root.check(inbox.newChatLoading && inbox.newChatError==="","service replacement retires old responses")
     fake.pending.shift().callback(true,[{id:"alice",name:"Alice"},{id:"bob",name:"Bob"}])
     inbox.newChatGroup=true
     inbox.toggleNewChatTarget("alice");inbox.toggleNewChatTarget("bob")
     inspect.findChild(inbox,"newChatName").text=" Friends "
     inbox.createNewChat();var created=fake.pending.shift()
     var sent=fake.calls.filter(function(c){return c.method==="createConversation"}).slice(-1)[0]
     root.check(sent.params.name==="Friends" && sent.params.targetIDs.length===2 && sent.network==="telegram","group requests use the chosen service and recipients")
     created.callback(false,"Synthetic group failure")
     root.check(!inbox.newChatLoading && inbox.newChatOpen && inbox.newChatError==="Synthetic group failure","failed group requests retain the selection for review")
     inbox.newChatOpen=false
     inbox.openNewChat();fake.pending.shift().callback(false,"Temporary contact failure")
     root.check(inbox.newChatError==="Temporary contact failure" && !inbox.newChatLoading,"current failures leave a usable picker")
     inbox.newChatOpen=false;inbox.openNewChat()
     fake.pending.shift().callback(true,[{id:"alice",name:"Alice"},{id:"bob",name:"Bob"}])
     root.step++
    } else if (root.step === 1) {
     var list=inspect.findChild(inbox,"newChatTargetList")
     list.forceLayout();var target=list.itemAtIndex(0)
     root.check(!!target,"contact row is realized")
     target.forceActiveFocus()
     keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)
     root.check(inbox.newChatSelectionCount===1,"keyboard selects a contact")
     keyboard.keyClick(Qt.Key_Tab,Qt.NoModifier,0)
     root.check(inspect.findChild(inbox,"newChatCancelButton").activeFocus,"Tab from contacts stays within the picker")
     keyboard.keyClick(Qt.Key_Tab,Qt.NoModifier,0)
     root.check(inspect.findChild(inbox,"createConversationButton").activeFocus,"Tab reaches the create action")
     keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
     root.check(!inbox.newChatOpen,"Escape closes the picker")
     inbox.openNewChat();var old=fake.pending.shift().callback
     host.opened=false;old(true,[{id:"hidden",name:"Hidden response"}])
     root.check(!inbox.newChatOpen && !inbox.newChatLoading,"closing the panel retires contact requests")
     console.log("OMACHAT_CONVERSATION_CREATE_PASS");Qt.quit()
    }
   } catch(e) {console.log("OMACHAT_CONVERSATION_CREATE_FAIL",e);Qt.quit()}
  }
 }
}
