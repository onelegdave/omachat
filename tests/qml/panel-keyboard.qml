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
    property bool unifiedInboxEnabled: false
    property int loadCount: 0
    property var loadedNetworks: []
    property string lastMessageNetwork: ""
    property string lastMessageConversation: ""
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
      {id:"demo-alex",name:"Alex Rivera",initials:"AR",avatarColor:"#4c80b8",preview:"See you at the trailhead!\nI’ll be there at 9.",timestamp:root.now,unread:false},
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
    function isServiceEnabled(net) { return enabledServices.indexOf(net)>=0 }
    function unreadFor(net) { return net === "gmessages" ? 1 : 0 }
    function loadConversations(net) { loadedNetworks=loadedNetworks.concat([net]); loadCount++ }
    function refreshConversations(net) {}
    function call(method, params, callback, network) {
      if (!callback) return
      if (method === "messages") { lastMessageNetwork=network; lastMessageConversation=params.conversationID }
      if (method === "config") callback(true,{uiScale:1.1,enabledServices:enabledServices,keyboardShortcuts:keyboardShortcuts, unifiedInboxEnabled:unifiedInboxEnabled})
      else if (method === "setUnifiedInboxPreference") {
        unifiedInboxEnabled=params.enabled
        callback(true,{unifiedInboxEnabled:unifiedInboxEnabled})
      }
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
  function visualTextStartingWith(item, prefix) {
    if (!item) return null
    if (item.text !== undefined && String(item.text).indexOf(prefix) === 0) return item
    var children = item.children || []
    for (var i = 0; i < children.length; i++) {
      var found = visualTextStartingWith(children[i], prefix)
      if (found) return found
    }
    return null
  }
  Timer {
    interval:200; running:true; repeat:true
    onTriggered: {
      try {
        if (root.step++ === 0) { panel.open(); return }
        // QtTest key events pump a nested event loop. Do not run this handler
        // again while an earlier keyboard assertion is still in progress.
        stop()
        var loader=inspect.findChild(panel,"inboxLoader")
        var list=inspect.findChild(loader.item,"convList")
        check(list !== null,"actual panel contains keyboard conversation list")
        check(list.activeFocus,"opening the panel focuses its retained conversation list")
        list.currentIndex=0
        list.forceActiveFocus()
        panel.activeService="whatsapp"
        keyboard.keyClick(Qt.Key_Tab,Qt.ControlModifier,0)
        check(panel.activeService==="telegram","Ctrl+Tab switches to the next enabled service")
        keyboard.keyClick(Qt.Key_Tab,Qt.ControlModifier|Qt.ShiftModifier,0)
        check(panel.activeService==="telegram","Ctrl+Shift+Tab does not switch services")
        panel.activeService="whatsapp"
        fake.enabledServices=["whatsapp","signal"]
        panel.activeService="signal"
        keyboard.keyClick(Qt.Key_Tab,Qt.ControlModifier|Qt.ShiftModifier,0)
        check(panel.activeService==="signal","Ctrl+Shift+Tab does not switch from Signal")
        keyboard.keyClick(Qt.Key_Tab,Qt.ControlModifier|Qt.ShiftModifier,0)
        check(panel.activeService==="signal","Ctrl+Shift+Tab does not switch from WhatsApp")
        fake.enabledServices=["gmessages","whatsapp","telegram"]
        panel.activeService="gmessages"
        keyboard.keyClick(Qt.Key_2,Qt.ControlModifier,0)
        check(panel.activeService==="whatsapp","Ctrl+2 selects the second enabled tab rather than a fixed provider")
        keyboard.keyClick(Qt.Key_1,Qt.ControlModifier,0)
        check(panel.activeService==="gmessages","Ctrl+1 selects the first enabled tab")
        fake.enabledServices=["whatsapp","signal"]
        panel.activeService="whatsapp"
        keyboard.keyClick(Qt.Key_1,Qt.ControlModifier,0)
        check(panel.activeService==="whatsapp","Ctrl+1 selects WhatsApp when it is the first enabled tab")
        keyboard.keyClick(Qt.Key_2,Qt.ControlModifier,0)
        check(panel.activeService==="signal","Ctrl+2 selects Signal when it is the second enabled tab")
        fake.enabledServices=["gmessages","whatsapp","telegram"]
        panel.unifiedInboxEnabled=true
        check(panel.serviceTabs[0].value==="all" && panel.serviceTabs[0].label==="All","unified inbox appears as the first labeled tab when enabled")
        panel.activeService="gmessages"
        keyboard.keyClick(Qt.Key_1,Qt.ControlModifier,0)
        check(panel.activeService==="all","Ctrl+1 selects the unified inbox when enabled")
        keyboard.keyClick(Qt.Key_2,Qt.ControlModifier,0)
        check(panel.activeService==="gmessages","provider shortcuts shift one position when unified inbox is enabled")
        var beforeUnifiedLoad=fake.loadCount
        panel.setActiveService("all")
        check(fake.loadCount===beforeUnifiedLoad+3 && fake.loadedNetworks.slice(-3).join(",")==="gmessages,whatsapp,telegram","opening unified inbox loads all enabled providers")
        check(panel.unifiedConversations.length===12 && panel.unifiedConversations.some(function(row){return row.network==="gmessages" && row.key==="gmessages:demo-alex"}),"unified list combines cached provider conversations and tags each entry")
        var unifiedLoader=inspect.findChild(panel,"unifiedInboxLoader")
        check(unifiedLoader.visible && unifiedLoader.item.conversations.length===12,"enabled unified inbox presents the aggregate conversation list")
        var unifiedList=root.visualChild(unifiedLoader.item,"unifiedConvList")
        var firstUnifiedRow=unifiedList.itemAtIndex(0)
        var alexUnifiedIndex=unifiedLoader.item.conversations.findIndex(function(row){return row.key==="gmessages:demo-alex"})
        var alexUnifiedRow=unifiedList.itemAtIndex(alexUnifiedIndex)
        var alexPreview=root.visualTextStartingWith(alexUnifiedRow,"See you at the trailhead!")
        check(alexPreview && alexPreview.text==="See you at the trailhead! I’ll be there at 9.","unified inbox flattens multiline previews so they cannot overlap the contact name")
        var selectedUnifiedNetwork=unifiedLoader.item.conversations[0].network
        keyboard.mouseClick(firstUnifiedRow,18,18,Qt.LeftButton,Qt.NoModifier,0)
        var unifiedDetails=inspect.findChild(unifiedLoader.item,"unifiedDetailInbox")
        check(panel.activeService==="all" && unifiedLoader.item.detailNetwork===selectedUnifiedNetwork,"selecting a unified row keeps All active and targets its provider")
        check(unifiedDetails && unifiedDetails.network===selectedUnifiedNetwork && unifiedDetails.selectedConvID===unifiedLoader.item.selectedConversation.id,"the selected provider thread appears in the unified detail pane")
        check(fake.lastMessageNetwork===selectedUnifiedNetwork && fake.lastMessageConversation===unifiedLoader.item.selectedConversation.id,"the detail pane loads messages from the selected provider and conversation")
        var unifiedLeft=inspect.findChild(unifiedLoader.item,"unifiedConversationSidebar")
        var unifiedRight=inspect.findChild(unifiedLoader.item,"unifiedDetailPane")
        check(unifiedRight.width>0 && unifiedLeft.width>0 && unifiedRight.x>unifiedLeft.x,"unified inbox keeps its list and thread panes side by side")
        check(root.visualChild(unifiedLoader.item,"unifiedSelectedAvatar")!==null,"unified list renders the selected contact avatar")
        check(root.visualChild(unifiedLoader.item,"unifiedServiceBadge")!==null,"unified list tags each conversation with its service")
        var sidebarToggle=inspect.findChild(panel,"sidebarToggleButton")
        check(sidebarToggle.visible && sidebarToggle.enabled,"conversation-list visibility control is available in the unified inbox")
        var expandedDetailWidth=unifiedRight.width
        keyboard.mouseClick(sidebarToggle,10,10,Qt.LeftButton,Qt.NoModifier,0)
        check(panel.sidebarCollapsed && !unifiedLeft.visible && unifiedRight.width>expandedDetailWidth,"hiding the unified conversation list expands the thread pane")
        keyboard.mouseClick(sidebarToggle,10,10,Qt.LeftButton,Qt.NoModifier,0)
        check(!panel.sidebarCollapsed && unifiedLeft.visible,"show conversations restores the unified list")
        var unifiedNextUnread=root.visualChild(unifiedLoader.item,"unifiedNextUnread")
        check(unifiedNextUnread!==null,"unified inbox provides a next-unread control")
        check(panel.isReadingConversation(selectedUnifiedNetwork,unifiedLoader.item.selectedConversation.id),"reading a unified thread suppresses its notifications")
        keyboard.mouseClick(unifiedNextUnread,12,12,Qt.LeftButton,Qt.NoModifier,0)
        check(unifiedLoader.item.selectedConversation.unread===true && panel.activeService==="all","next unread selects an unread thread without leaving All")
        panel.setActiveService("all")
        keyboard.keyClick(Qt.Key_A,Qt.ControlModifier|Qt.ShiftModifier,0)
        check(!panel.unifiedInboxEnabled,"configurable toggle shortcut turns unified inbox off")
        keyboard.keyClick(Qt.Key_1,Qt.ControlModifier,0)
        check(panel.activeService==="gmessages","Ctrl+1 returns to first provider when unified inbox is disabled")
        check(list.activeFocus,"toggling out of unified inbox restores focus to the provider list")
        panel.unifiedInboxEnabled=true
        panel.activeService="gmessages"
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
        keyboard.keyClick(Qt.Key_N,Qt.ControlModifier,0)
        var targetSearch=inspect.findChild(loader.item,"newChatSearch")
        var targetList=inspect.findChild(loader.item,"newChatTargetList")
        check(loader.item.newChatOpen && targetSearch.activeFocus,"Ctrl+N opens a contact picker with focused search")
        keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
        check(targetList.activeFocus,"Down moves from contact search into its results")
        keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)
        check(loader.item.newChatSelectionCount===1,"Space selects a contact without a mouse")
        keyboard.keyClick(Qt.Key_2,Qt.ControlModifier,0)
        check(panel.activeService==="gmessages","modal keyboard events cannot switch the background service")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!loader.item.newChatOpen && list.activeFocus && panel.opened,"Escape closes the contact picker and restores list focus")
        keyboard.keyClick(Qt.Key_Tab,Qt.NoModifier,0)
        check(!list.activeFocus && panel.opened,"Tab moves to a control without switching panels")
        panel.settingsOpen=true
        // Let Settings finish its initial-focus handoff before driving controls.
        Qt.callLater(function() {
        try {
        var settings=inspect.findChild(panel,"bodyLoader").item
        var unifiedSwitch=inspect.findChild(settings,"unifiedInboxSwitch")
        check(unifiedSwitch!==null,"Settings exposes the unified inbox toggle")
        settings.saveUnifiedInboxPreference(true)
        check(fake.unifiedInboxEnabled && panel.unifiedInboxEnabled && panel.activeService==="all","Settings toggle saves the persistent preference and opens unified inbox")
        settings.saveUnifiedInboxPreference(false)
        check(!fake.unifiedInboxEnabled && !panel.unifiedInboxEnabled && panel.activeService==="gmessages","Settings toggle disables unified inbox and restores provider tabs")
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
        Qt.quit()
        } catch(e) { console.error("OMACHAT_PANEL_KEYBOARD_FAIL",e);Qt.quit() }
        })
      } catch(e) { console.error("OMACHAT_PANEL_KEYBOARD_FAIL",e);stop();Qt.quit() }
    }
  }
}
