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
  readonly property var captures: [
    {name:"tokyo-night", palette:0, view:"messenger"},
    {name:"catppuccin-latte", palette:1, view:"messenger"},
    {name:"gruvbox", palette:2, view:"messenger"},
    {name:"unified-inbox", palette:0, view:"all"},
    {name:"emoji-search", palette:0, view:"emoji"}
  ]
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
    property var statusFB: ({state:"connected", phoneOK:true})
    property int unread: 1
    property var conversations: [
      {id:"demo-alex",name:"Alex Rivera",initials:"AR",avatarColor:"#4c80b8",preview:"See you at the trailhead!",timestamp:root.now,unread:false},
      {id:"demo-jordan",name:"Jordan Lee",initials:"JL",avatarColor:"#aa7350",preview:"The photos look great.",timestamp:root.now-3600000000,unread:true},
      {id:"demo-sam",name:"Sam Ortega",initials:"SO",avatarColor:"#608969",preview:"Coffee tomorrow?",timestamp:root.now-7200000000,unread:false},
      {id:"demo-riley",name:"Riley Chen",initials:"RC",avatarColor:"#936b9d",preview:"Thanks for the recommendation.",timestamp:root.now-10800000000,unread:false}
    ]
    property var conversationsWA: [
      {id:"demo-alex",name:"Alex Rivera",initials:"AR",avatarColor:"#4c80b8",preview:"See you at the trailhead!",timestamp:root.now,unread:false},
      {id:"demo-jordan",name:"Jordan Lee",initials:"JL",avatarColor:"#aa7350",preview:"The photos look great.",timestamp:root.now-3600000000,unread:true}
    ]
    property var conversationsTG: [
      {id:"demo-weekend",name:"Weekend Walks",initials:"WW",avatarColor:"#608969",preview:"Meet by the lake at nine.",timestamp:root.now-1800000000,unread:true,isGroup:true},
      {id:"demo-sam",name:"Sam Ortega",initials:"SO",avatarColor:"#936b9d",preview:"Coffee tomorrow?",timestamp:root.now-7200000000,unread:false}
    ]
    property var conversationsFB: [
      {id:"demo-riley",name:"Riley Chen",initials:"RC",avatarColor:"#936b9d",preview:"See you at the trailhead!",timestamp:root.now-2400000000,unread:false},
      {id:"demo-maya",name:"Maya Patel",initials:"MP",avatarColor:"#608969",preview:"Saved you a seat.",timestamp:root.now-10800000000,unread:true}
    ]
    signal messageReceived(var message, var net)
    signal conversationUpdated(var conversation, var net)
    signal paired(var net)
    function statusFor(net) { return net === "whatsapp" ? statusWA : net === "telegram" ? statusTG : net === "messenger" ? statusFB : status }
    function stateFor(net) { return statusFor(net).state }
    function conversationsFor(net) { return net === "whatsapp" ? conversationsWA : net === "telegram" ? conversationsTG : net === "messenger" ? conversationsFB : conversations }
    function unreadFor(net) { return net === "gmessages" ? 0 : 1 }
    function loadConversations(net) {}
    function refreshConversations(net) {}
    function call(method, params, callback, network) {
      if (!callback) return
      if (method === "config") callback(true,{uiScale:1.1,enabledServices:enabledServices,unifiedInboxEnabled:false})
      else if (method === "setChatView") callback(true,{})
      else if (method === "messages") callback(true,{hasMore:false,messages:[
        {id:"demo-1",conversationID:params.conversationID,text:"Ready for a walk this weekend?",fromMe:false,timestamp:root.now-600000000,attachments:[],reactions:[{emoji:"👍",count:2,mine:true}]},
        {id:"demo-2",conversationID:params.conversationID,text:"Absolutely. Saturday morning works for me.",fromMe:true,timestamp:root.now-480000000,delivery:"delivered",attachments:[],reactions:[]},
        {id:"demo-4",conversationID:params.conversationID,text:"Sounds good. I'll bring coffee and sandwiches.",fromMe:true,timestamp:root.now-240000000,delivery:"read",attachments:[],reactions:[]},
        {id:"demo-5",conversationID:params.conversationID,text:"See you at the trailhead!",fromMe:false,timestamp:root.now-120000000,attachments:[],reactions:[]}
      ]})
      else callback(false,"Screenshot fixture: action unavailable")
    }
  }
  Chat.Panel { id: panel; service:fake; manageIpc:false }
  TestResult { id: inspect }
  function check(ok,label) { if (!ok) throw new Error(label); console.log("PASS:",label) }
  function checkConnectionStatus() {
    fake.enabledServices=["whatsapp","telegram","messenger"]
    fake.status={state:"disconnected",phoneOK:false}
    panel.unifiedInboxEnabled=true
    panel.activeService="all"
    check(panel.linkLabel==="CONNECTED" && panel.linkUp, "All status ignores disabled disconnected Google")
    fake.statusTG={state:"unpaired",phoneOK:true}
    check(panel.linkLabel==="2/3 CONNECTED" && !panel.linkUp, "All status reacts to a disconnected enabled provider")
    fake.connected=false
    check(panel.linkLabel==="HELPER OFFLINE" && !panel.linkUp, "helper outage takes priority over cached provider states")
    fake.connected=true
    fake.enabledServices=[]
    check(panel.linkLabel==="SERVICES OFF", "All status handles no enabled services")
    fake.enabledServices=["whatsapp","telegram","messenger"]
    fake.statusTG={state:"connected",phoneOK:true}
    panel.activeService="whatsapp"
    check(panel.linkLabel==="CONNECTED" && panel.linkUp, "individual provider status remains connected")
    fake.statusWA={state:"connected",phoneOK:false}
    check(panel.linkLabel==="PHONE OFFLINE", "individual provider phone state is preserved")
    fake.statusWA={state:"connected",phoneOK:true}
    console.log("OMACHAT_CONNECTION_STATUS_PASS")
  }
  function setPalette() {
    var p=palettes[captures[frame].palette]
    Color.background=p.background; Color.foreground=p.foreground
    Color.accent=p.accent; Color.muted=p.muted; Color.urgent=p.red
    Color.shellValues={"popups.background":p.background,"popups.text":p.foreground,"popups.border":p.accent}
    Style.applyShellValues({})
  }
  Timer {
    interval:800; running:true
    onTriggered: {
      root.setPalette()
      try { root.checkConnectionStatus() } catch(error) { console.error("GALLERY_FAIL",error); Qt.quit(); return }
      panel.open()
      capture.restart()
    }
  }
  Timer {
    id:capture; interval:500
    onTriggered: {
      var view=root.captures[root.frame].view
      fake.enabledServices=view==="all" ? ["whatsapp","telegram","messenger"] : ["gmessages","whatsapp","telegram","messenger"]
      panel.unifiedInboxEnabled=view==="all"
      panel.activeService=view==="all" ? "all" : "messenger"
      var loader=inspect.findChild(panel,view==="all" ? "unifiedInboxLoader" : "inboxLoader")
      if (!loader || !loader.item) { console.error("GALLERY_FAIL inbox unavailable"); Qt.quit(); return }
      if(view==="all") {
        check(panel.linkLabel==="CONNECTED" && panel.linkUp,"unified screenshot uses enabled-service status")
        loader.item.selectConversation(panel.unifiedConversations.find(function(c) { return c.network==="whatsapp" && c.id==="demo-alex" }))
      } else loader.item.selectConversation("demo-riley")
      if(view==="emoji") {
        loader.item.openEmojiPicker(null,false)
        inspect.findChild(loader.item,"emojiSearchField").text="heart"
      }
      save.restart()
    }
  }
  Timer {
    id:save; interval:400
    onTriggered: {
      var loader=inspect.findChild(panel,root.captures[root.frame].view==="all" ? "unifiedInboxLoader" : "inboxLoader")
      if(panel.chatViewError!=="") { console.error("GALLERY_FAIL unexpected chat-view error"); Qt.quit(); return }
      loader.parent.parent.parent.grabToImage(function(result) {
        if (!result.saveToFile(root.outputDir+"/"+root.captures[root.frame].name+".png")) {
          console.error("GALLERY_FAIL save failed"); Qt.quit(); return
        }
        root.frame++
        if(root.frame >= root.captures.length) { console.log("OMACHAT_GALLERY_PASS"); Qt.quit(); return }
        root.setPalette(); capture.restart()
      })
    }
  }
}
