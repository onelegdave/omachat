import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

// Actual panel keyboard routing, fictional data, no account access.
ShellRoot {
  id: root
  property double now: Date.now() * 1000
  QtObject {
    id: fake
    property bool connected: true
    property string currentNetwork: "gmessages"
    property var enabledServices: ["gmessages", "whatsapp", "telegram"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string servicesError: ""
    property string refreshError: ""
    property bool refreshing: false
    property string state: "connected"
    property var status: ({state:"connected", phoneOK:true})
    property var statusWA: ({state:"connected", phoneOK:true})
    property var statusTG: ({state:"connected", phoneOK:true})
    property int unread: 1
    property var conversations: [
      {id:"demo-alex",name:"Alex Rivera",initials:"AR",avatarColor:"#4c80b8",preview:"See you at the trailhead!",timestamp:root.now,unread:false},
      {id:"demo-jordan",name:"Jordan Lee",initials:"JL",avatarColor:"#aa7350",preview:"The photos look great.",timestamp:root.now-3600000000,unread:true},
      {id:"demo-sam",name:"Sam Ortega",initials:"SO",avatarColor:"#608969",preview:"Coffee tomorrow?",timestamp:root.now-7200000000,unread:false},
      {id:"demo-riley",name:"Riley Chen",initials:"RC",avatarColor:"#936b9d",preview:"Thanks for the recommendation.",timestamp:root.now-10800000000,unread:false}
    ]
    signal messageReceived(var message, var net)
    signal conversationUpdated(var conversation, var net)
    signal paired(var net)
    function statusFor(net) { return status }
    function stateFor(net) { return "connected" }
    function conversationsFor(net) { return conversations }
    function unreadFor(net) { return net === "gmessages" ? 1 : 0 }
    function loadConversations(net) {}
    function refreshConversations(net) {}
    function call(method, params, callback, network) {
      if (!callback) return
      if (method === "config") callback(true,{uiScale:1.1,giphyKeySet:false,enabledServices:enabledServices})
      else if (method === "messages") callback(true,{hasMore:false,messages:[
        {id:"demo-1",conversationID:"demo-alex",text:"Ready for a walk this weekend?",fromMe:false,timestamp:root.now-600000000,attachments:[],reactions:[]},
        {id:"demo-2",conversationID:"demo-alex",text:"Absolutely. Saturday morning works for me.",fromMe:true,timestamp:root.now-480000000,delivery:"delivered",attachments:[],reactions:[]},
        {id:"demo-3",conversationID:"demo-alex",text:"Let's take the lakeside trail and bring a picnic.",fromMe:false,timestamp:root.now-360000000,attachments:[],reactions:[]},
        {id:"demo-4",conversationID:"demo-alex",text:"Sounds good. I'll bring coffee and sandwiches.",fromMe:true,timestamp:root.now-240000000,delivery:"read",attachments:[],reactions:[]},
        {id:"demo-5",conversationID:"demo-alex",text:"See you at the trailhead!",fromMe:false,timestamp:root.now-120000000,attachments:[],reactions:[]}
      ]})
      else callback(false,"Screenshot fixture: action unavailable")
    }
  }
  Chat.Panel { id: panel; service:fake; manageIpc:false }
  TestResult { id: inspect }
  TestEvent { id: keyboard }
  property int step: 0
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  Timer {
    interval:200; running:true; repeat:true
    onTriggered: {
      try {
        if (root.step++ === 0) { panel.open(); return }
        var loader=inspect.findChild(panel,"inboxLoader")
        var list=inspect.findChild(loader.item,"convList")
        check(list !== null,"actual panel contains keyboard conversation list")
        list.currentIndex=0
        list.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
        check(list.currentIndex===1,"actual panel forwards arrow keys")
        keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
        check(loader.item.selectedConvID==="demo-jordan","actual panel forwards Enter")
        keyboard.keyClick(Qt.Key_Tab,Qt.NoModifier,0)
        check(!list.activeFocus && panel.opened,"Tab moves to a control without switching panels")
        console.log("OMACHAT_PANEL_KEYBOARD_PASS")
        stop();Qt.quit()
      } catch(e) { console.error("OMACHAT_PANEL_KEYBOARD_FAIL",e);stop();Qt.quit() }
    }
  }
}
