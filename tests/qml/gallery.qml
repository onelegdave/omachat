import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

// Real plugin components, fictional data, no protocol helper or account access.
ShellRoot {
  id: root
  property var palettes: JSON.parse(Quickshell.env("OMACHAT_GALLERY_PALETTES"))
  property int frame: 0
  property string outputDir: Quickshell.env("OMACHAT_UI_ARTIFACTS")
  // Fixed synthetic clock keeps committed screenshots reproducible.
  property double now: 1767225600000000
  QtObject {
    id: fake
    property bool connected: true
    property string currentNetwork: "messenger"
    property var enabledServices: ["gmessages", "whatsapp", "telegram", "messenger"]
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
    function unreadFor(net) { return net === "messenger" ? 2 : (net === "whatsapp" ? 1 : 0) }
    function loadConversations(net) {}
    function refreshConversations(net) {}
    function call(method, params, callback, network) {
      if (!callback) return
      if (method === "config") callback(true,{uiScale:1.1,giphyKeySet:false,enabledServices:enabledServices})
      else if (method === "messages") callback(true,{hasMore:false,messages:[
        {id:"demo-1",conversationID:"demo-alex",text:"Ready for a walk this weekend?",fromMe:false,timestamp:root.now-600000000,attachments:[],reactions:[{emoji:"👍",count:2,mine:true}]},
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
  function setPalette() {
    var p=palettes[frame]
    Color.background=p.background; Color.foreground=p.foreground
    Color.accent=p.accent; Color.muted=p.muted; Color.urgent=p.red
    Color.shellValues={"popups.background":p.background,"popups.text":p.foreground,"popups.border":p.accent}
    Style.applyShellValues({})
  }
  Timer {
    interval:800; running:true
    onTriggered: {
      root.setPalette()
      panel.open()
      capture.restart()
    }
  }
  Timer {
    id:capture; interval:500
    onTriggered: {
      var loader=inspect.findChild(panel,"inboxLoader")
      if (!loader || !loader.item) { console.error("GALLERY_FAIL inbox unavailable"); Qt.quit(); return }
      panel.activeService="messenger"
      loader.item.selectConversation("demo-alex")
      save.restart()
    }
  }
  Timer {
    id:save; interval:400
    onTriggered: {
      var loader=inspect.findChild(panel,"inboxLoader")
      loader.parent.parent.parent.grabToImage(function(result) {
        if (!result.saveToFile(root.outputDir+"/"+root.palettes[root.frame].slug+".png")) {
          console.error("GALLERY_FAIL save failed"); Qt.quit(); return
        }
        root.frame++
        if(root.frame >= root.palettes.length) { console.log("OMACHAT_GALLERY_PASS"); Qt.quit(); return }
        root.setPalette(); capture.restart()
      })
    }
  }
}
