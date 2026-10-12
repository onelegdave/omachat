import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat
import "Chat/Keybindings.js" as Keybindings

ShellRoot {
  id: root
  property int step: 0
  property var stale: null
  property string staleNetwork: ""
  function check(value, label) { if (!value) throw new Error(label); console.log("PASS:", label) }
  function take(method) {
    for (var i=0; i<fake.requests.length; i++) if (fake.requests[i].method === method) return fake.requests.splice(i,1)[0]
    throw new Error("missing " + method)
  }
  function row(net) { return unified.conversations.find(function(c) { return c.network === net }) }
  QtObject { id: drafts; property var drafts: ({}) }
  QtObject {
    id: fake
    property bool connected: true
    property string state: "connected"
    property string refreshError: ""
    property var requests: []
    property var status: ({state:"connected",phoneOK:true})
    property var statusWA: ({state:"connected",phoneOK:true})
    property var conversations: [{id:"same",name:"Synthetic Google",timestamp:10,unread:true}]
    property var conversationsWA: [{id:"same",name:"Synthetic WhatsApp",timestamp:20,unread:true}]
    signal paired(var network)
    signal messageReceived(var msg, var network)
    signal historyUpdated(var update, var network)
    function conversationsFor(net) { return net === "whatsapp" ? conversationsWA : (net === "gmessages" ? conversations : []) }
    function stateFor(net) { return "connected" }
    function statusFor(net) { return ({state:"connected",phoneOK:true}) }
    function call(method,params,callback,net) { requests.push({method:method,params:params,callback:callback,net:net}) }
  }
  FloatingWindow {
    visible:true; implicitWidth:920; implicitHeight:580
    Item { id: host; property bool opened:true; function toggleSidebar() { unified.sidebarCollapsed = !unified.sidebarCollapsed }
      function focusConversationList() { unified.focusConversationList() } }
    Chat.UnifiedInbox {
      id: unified; anchors.fill:parent; service:fake; host:host; draftStore:drafts
      conversations: Keybindings.buildUnifiedConversations([{network:"gmessages",conversations:fake.conversations},{network:"whatsapp",conversations:fake.conversationsWA}])
    }
    Chat.InboxView { id: provider; visible:false; viewActive:false; service:fake; host:host; draftStore:drafts; network:"gmessages" }
    TestResult { id: inspect }
  }
  Timer {
    interval:100;running:true;repeat:true
    onTriggered: {
      stop()
      try {
        var detail=unified.detailView
        var composer=inspect.findChild(detail,"composer")
        switch(root.step++) {
        case 0:
          check(unified.conversations.length===2 && unified.conversations[0].network==="whatsapp", "enabled-source rows sorted with service identity")
          unified.selectConversation(row("gmessages"))
          break
        case 1:
          var first=take("messages");check(first.net==="gmessages" && first.params.conversationID==="same", "Google detail request routes to Google")
          root.stale=first.callback
          composer.text="Google unsent draft"
          unified.selectConversation(row("whatsapp"))
          break
        case 2:
          var second=take("messages");check(second.net==="whatsapp", "same-id WhatsApp detail request routes to WhatsApp")
          second.callback(true,{messages:[{id:"wa",conversationID:"same",text:"Synthetic WhatsApp message",timestamp:20}],hasMore:false})
          root.stale(true,{messages:[{id:"gm-old",conversationID:"same",text:"Retired Google message",timestamp:10}],hasMore:false})
          check(detail.messages.length===1 && detail.messages[0].id==="wa", "retired other-service reply cannot replace detail history")
          composer.text="WhatsApp unsent draft"
          unified.sidebarCollapsed=true
          unified.handleKeyboardAction("search")
          check(unified.sidebarVisible, "search reveals a collapsed unified sidebar")
          detail.sendingMedia=true;unified.selectConversation(row("gmessages"))
          check(unified.detailNetwork==="whatsapp", "upload keeps selected row and detail target consistent")
          detail.sendingMedia=false
          unified.selectConversation(row("gmessages"))
          break
        case 3:
          take("messages").callback(true,{messages:[],hasMore:false})
          check(composer.text==="Google unsent draft", "unified draft restores without another service's text")
          provider.selectConversation("same",false)
          take("messages").callback(true,{messages:[],hasMore:false})
          check(inspect.findChild(provider,"composer").text==="Google unsent draft", "provider and unified views share the same per-service draft")
          fake.conversations=[]
          break
        case 4:
          check(unified.selectedConversation===null && detail.selectedConvID==="" && detail.messages.length===0, "removing a service clears its selected detail")
          unified.selectConversation(row("whatsapp"))
          break
        case 5:
          take("messages").callback(true,{messages:[],hasMore:false})
          check(composer.text==="WhatsApp unsent draft", "other enabled service retains its own draft")
          fake.requests=[];detail.hasOlder=true;detail.loadingMessages=false;detail.loadingOlder=false
          detail.queueOlderMessages();detail.selectionGeneration++
          root.stale=function() { check(fake.requests.length===0,"deferred older-history request retires with selection") }
          break
        case 6:
          root.stale()
          console.log("OMACHAT_UNIFIED_INBOX_PASS")
          Qt.callLater(Qt.quit)
          return
        }
        restart()
      } catch(error) {
        console.error("OMACHAT_UNIFIED_INBOX_FAIL",error)
        Qt.callLater(Qt.quit)
      }
    }
  }
}
