import QtQuick
import QtTest
import Quickshell
import qs.Commons
import "Chat" as Chat

// Emoji picker behavior against the real panel and a synthetic service.
ShellRoot {
  id: root
  property double now: Date.now() * 1000
  property int step: 0
  QtObject {
    id: fake
    property bool connected: true
    property string currentNetwork: "gmessages"
    property var enabledServices: ["gmessages"]
    property bool servicesConfigLoaded: true
    property bool serviceSelectionRequired: false
    property bool savingServices: false
    property bool restartingServices: false
    property string servicesError: ""
    property string refreshError: ""
    property bool refreshing: false
    property string state: "connected"
    property var status: ({state:"connected", phoneOK:true})
    property var conversations: [{id:"demo-alex",name:"Alex Rivera",initials:"AR",preview:"Hi",timestamp:root.now,unread:false}]
    property string reactedEmoji: ""
    property string reactedMessage: ""
    signal messageReceived(var message, var net)
    signal conversationUpdated(var conversation, var net)
    signal paired(var net)
    function statusFor(net) { return status }
    function stateFor(net) { return "connected" }
    function conversationsFor(net) { return conversations }
    function unreadFor(net) { return 0 }
    function loadConversations(net) {}
    function call(method, params, callback, network) {
      if (method === "config") callback(true,{uiScale:1,enabledServices:enabledServices})
      else if (method === "messages") callback(true,{hasMore:false,messages:[
        {id:"demo-1",conversationID:"demo-alex",text:"Hi",fromMe:false,timestamp:root.now,attachments:[],reactions:[]}
      ]})
      else if (method === "react") {
        reactedEmoji=params.emoji
        reactedMessage=params.messageID
        callback(true,{})
      } else callback(false,"fixture action unavailable")
    }
  }
  Chat.Panel { id: panel; service:fake; manageIpc:false }
  TestResult { id: inspect }
  TestEvent { id: keyboard }
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
    interval:100; running:true; repeat:true
    onTriggered: {
      try {
        if (root.step++ === 0) { panel.open(); return }
        stop()
        var loader=inspect.findChild(panel,"inboxLoader")
        var inbox=loader.item
        var list=inspect.findChild(inbox,"convList")
        check(list && list.activeFocus,"opening panel focuses the conversation list")
        keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
        check(inbox.selectedConvID==="demo-alex","fixture opens its synthetic conversation")
        var composer=inspect.findChild(inbox,"composer")
        composer.text="hello world"
        composer.cursorPosition=5
        composer.forceActiveFocus()
        keyboard.keyClick(Qt.Key_E,Qt.ControlModifier,0)
        var search=inspect.findChild(inbox,"emojiSearchField")
        var grid=inspect.findChild(inbox,"emojiGrid")
        check(inbox.emojiPickerOpen && search.activeFocus,"opening emoji picker loads search and focuses its field")
        check(inbox.emojiSearchDataLoaded && inbox.emojiSearchIndex.length>3000,"bundled offline dataset is loaded and indexed")
        search.text="smiel"
        check(grid.count>0 && grid.model[0].e==="😄","live typo-tolerant search ranks the intended emoji first")
        keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
        check(grid.activeFocus && grid.currentIndex===0,"Down moves from search to the highlighted grid result")
        keyboard.keyClick(Qt.Key_Up,Qt.NoModifier,0)
        check(search.activeFocus,"Up returns focus to the search field")
        search.text="no-such-emoji-query"
        var empty=inspect.findChild(inbox,"emojiNoResults")
        check(grid.count===0 && empty.visible,"unmatched query shows an explicit no-results state")
        search.text="heart"
        keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
        check(!inbox.emojiPickerOpen && composer.activeFocus,"Enter selects the best search result and restores composer focus")
        check(composer.text==="hello❤️ world","emoji inserts at the caret without losing surrounding text")
        check(composer.cursorPosition===7,"caret advances past inserted emoji")
        inbox.reactingTo="demo-1"
        var moreReaction=visualChild(inbox,"moreEmojiReactionButton")
        moreReaction.forceActiveFocus()
        keyboard.keyClick(Qt.Key_Space,Qt.NoModifier,0)
        check(inbox.emojiPickerOpen && inbox.emojiPickerForReact,"More reactions opens the searchable picker in reaction mode")
        search=inspect.findChild(inbox,"emojiSearchField")
        search.text="thumbsup"
        keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
        check(fake.reactedMessage==="demo-1" && fake.reactedEmoji.replace(/[\uFE0E\uFE0F]/g,"")==="👍","same searchable picker applies the chosen emoji reaction")
        check(!inbox.emojiPickerOpen,"reaction selection dismisses the picker")
        inbox.emojiPickerForReact=false
        keyboard.keyClick(Qt.Key_E,Qt.ControlModifier,0)
        check(inbox.emojiPickerOpen,"Ctrl+E opens the emoji picker from the conversation")
        keyboard.keyClick(Qt.Key_Escape,Qt.NoModifier,0)
        check(!inbox.emojiPickerOpen,"Escape closes the emoji picker")
        Qt.callLater(function() {
          try {
            check(composer.activeFocus,"deferred picker focus does not steal focus after Escape")
            console.log("OMACHAT_PANEL_EMOJI_SEARCH_PASS")
            Qt.quit()
          } catch(e) { console.error("OMACHAT_PANEL_EMOJI_SEARCH_FAIL",e);Qt.quit() }
        })
      } catch(e) { console.error("OMACHAT_PANEL_EMOJI_SEARCH_FAIL",e);stop();Qt.quit() }
    }
  }
}