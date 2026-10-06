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
    property var keyboardShortcuts: ({})
    property int shortcutSaveCount: 0
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
      if (method === "config") callback(true,{uiScale:1.1,enabledServices:enabledServices,keyboardShortcuts:keyboardShortcuts})
      else if (method === "setKeyboardShortcuts") {
        for (var id in params.shortcuts) {
          if (typeof params.shortcuts[id] !== "string") { callback(false,"shortcut RPC requires string values"); return }
        }
        keyboardShortcuts=Object.assign({},params.shortcuts)
        shortcutSaveCount++
        callback(true,{keyboardShortcuts:keyboardShortcuts})
      }
      else if (method === "conversationTargets") callback(true,[{id:"contact-a",name:"Alex"},{id:"contact-b",name:"Jordan"}])
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
  function visualChild(item, name) {
    if (!item) return null
    if (item.objectName === name) return item
    var children = item.children || []
    for (var i = 0; i < children.length; i++) {
      var found = visualChild(children[i], name)
      if (found) return found
    }
    return null
  }
  Timer {
    interval:200; running:true; repeat:true
    onTriggered: {
      stop()
      try {
        if (root.step++ === 0) { panel.open(); restart(); return }
        stop()
        var loader=inspect.findChild(panel,"inboxLoader")
        var list=inspect.findChild(loader.item,"convList")
        check(list !== null,"actual panel contains keyboard conversation list")
        check(list.activeFocus,"opening the panel focuses its retained conversation list")
        list.currentIndex=0
        list.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
        check(list.currentIndex===1,"actual panel forwards arrow keys")
        keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
        check(loader.item.selectedConvID==="demo-jordan","actual panel forwards Enter")
        var composer=inspect.findChild(loader.item,"composer")
        check(composer.activeFocus,"Enter on a conversation focuses its writable composer")
        list.forceActiveFocus()
        check(!loader.item.isKeyboardEditing(),"conversation list focus leaves text editing")
        keyboard.keyClick(Qt.Key_K,Qt.NoModifier,0)
        check(list.currentIndex===0,"Vim k moves the conversation cursor outside editors")
        keyboard.keyClick(Qt.Key_J,Qt.NoModifier,0)
        check(list.currentIndex===1,"Vim j moves the conversation cursor outside editors")
        panel.shortcutOverrides=({search:"j"})
        keyboard.keyClick(Qt.Key_J,Qt.NoModifier,0)
        var searchField=inspect.findChild(loader.item,"searchField")
        check(searchField.activeFocus,"custom bindings take priority over optional Vim navigation")
        panel.shortcutOverrides=({})
        list.forceActiveFocus()
        keyboard.keyClick(Qt.Key_P,Qt.ControlModifier|Qt.ShiftModifier,0)
        var actionMenu=inspect.findChild(panel,"keyboardActionMenu")
        var actionSearch=inspect.findChild(actionMenu,"keyboardActionSearch")
        var actionList=inspect.findChild(actionMenu,"keyboardActionList")
        check(actionMenu.visible,"Ctrl+Shift+P opens the searchable keyboard action menu")
        check(actionSearch.activeFocus,"opening the action menu moves focus into its search field")
        actionSearch.text="refresh"
        check(actionList.count===1 && actionList.model[0].id==="refresh","action menu filters actions as the user types")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!actionMenu.visible && panel.opened,"Escape dismisses the action menu without closing the panel")
        list.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Question,Qt.ShiftModifier,0)
        check(actionMenu.visible,"? outside editors opens keyboard help")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        composer.text="draft "
        composer.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Question,Qt.ShiftModifier,0)
        check(composer.text==="draft ?" && !actionMenu.visible,"? inside the composer remains literal text")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(list.activeFocus && panel.opened && composer.text==="draft ?","Escape leaves composition without discarding the draft")
        keyboard.keyClick(Qt.Key_Tab,Qt.NoModifier,0)
        check(!list.activeFocus && panel.opened,"Tab moves to a control without switching panels")
        panel.settingsOpen=true
        // Let Settings finish its initial-focus handoff before driving controls.
        Qt.callLater(function() {
        try {
        var settings=inspect.findChild(panel,"bodyLoader").item
        var preferences=inspect.findChild(settings,"keyboardShortcutsSection")
        preferences.replaceShortcut("search","Ctrl+Alt+f")
        preferences.replaceShortcut("compose","Ctrl+Alt+i")
        check(fake.shortcutSaveCount===2 && fake.keyboardShortcuts.search==="Ctrl+Alt+f" && fake.keyboardShortcuts.compose==="Ctrl+Alt+i","successive shortcut edits send valid string maps to the helper")
        check(panel.shortcutOverrides.search[0]==="Ctrl+Alt+f","saving preferences updates the live shortcut router")
        preferences.replaceShortcut("attach","Ctrl+Alt+i")
        check(fake.shortcutSaveCount===2 && preferences.errorText!=="","conflicts are explained before a save is sent")
        preferences.resetAll()
        check(Object.keys(fake.keyboardShortcuts).length===0 && Object.keys(panel.shortcutOverrides).length===0,"reset all restores live defaults")
        var recorder=visualChild(settings,"recordShortcut_help")
        recorder.forceActiveFocus()

        keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)

        keyboard.keyClick(Qt.Key_P,Qt.ControlModifier|Qt.ShiftModifier,0)


        check(fake.keyboardShortcuts.help==="Ctrl+Shift+p" && !actionMenu.visible,"recording captures a chord without executing its action")
        recorder.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!settings.isCapturingShortcut() && panel.settingsOpen,"Escape cancels shortcut recording before leaving Settings")
        var credentialEditor=inspect.findChild(settings,"telegramApiIdField")
        credentialEditor.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(panel.settingsOpen && !credentialEditor.activeFocus,"Escape leaves a Settings text editor before returning to chats")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!panel.settingsOpen && panel.opened,"Escape leaves Settings before closing the panel")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!panel.opened,"Escape closes the panel after leaving Settings")
        console.log("OMACHAT_PANEL_KEYBOARD_PASS")
        Qt.callLater(function() { Qt.quit() })
        } catch(e) { console.error("OMACHAT_PANEL_KEYBOARD_FAIL",e);Qt.callLater(function() { Qt.quit() }) }
        })
      } catch(e) { console.error("OMACHAT_PANEL_KEYBOARD_FAIL",e);stop();Qt.callLater(function() { Qt.quit() }) }
    }
  }
}
