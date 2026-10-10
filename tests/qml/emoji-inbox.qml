import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0
  function check(ok,label) { if (!ok) throw new Error(label); console.log("PASS:",label) }
  QtObject {
    id: fake
    property string state:"connected"
    property bool connected:true
    property string refreshError:""
    property var status: ({state:"connected",phoneOK:true})
    property var conversations: [{id:"emoji-fixture",name:"Synthetic emoji chat"}]
    function call(method,params,callback,net) {
      if (method==="messages") callback(true,{messages:[],hasMore:false})
    }
  }
  FloatingWindow {
    implicitWidth:920;implicitHeight:580;visible:true
    Item { id: host; property bool opened:true }
    Chat.InboxView { id: inbox; anchors.fill:parent;service:fake;host:host }
    TestResult {id: inspect}
    TestEvent {id: keyboard}
  }
  Timer {
    running:true;repeat:true;interval:150
    onTriggered: {
      stop()
      try {
        var composer=inspect.findChild(inbox,"composer")
        var search=inspect.findChild(inbox,"emojiSearchField")
        var grid=inspect.findChild(inbox,"emojiGrid")
        switch(root.step++) {
        case 0:
          check(inbox.emojiSearchDataLoaded && inbox.emojiSearchIndex.length>3000,"bundled offline metadata loads in the actual inbox")
          inbox.selectConversation("emoji-fixture",false)
          composer.text="hello world";composer.cursorPosition=5
          inbox.openEmojiPicker(composer,false)
          break
        case 1:
          check(search.activeFocus,"picker focuses search")
          search.text="smiel"
          check(grid.count>0 && grid.model[0].e==="😄","typo search ranks the expected emoji")
          search.text="heart"
          keyboard.keyClick(Qt.Key_Return,Qt.NoModifier,0)
          check(!inbox.emojiPickerOpen && composer.text==="hello❤️ world","Enter inserts at the caret without losing text")
          inbox.openEmojiPicker(composer,false)
          check(search.text==="","default clears search on reopen")
          search.text="heart";inbox.closeEmojiPicker();inbox.keepPreviousEmojiSearchText=true
          inbox.openEmojiPicker(composer,false)
          break
        case 2:
          check(search.activeFocus && search.text==="heart" && grid.count>0,"opt-in restores the previous query and focus")
          keyboard.keyClick(Qt.Key_Down,Qt.NoModifier,0)
          check(grid.activeFocus,"Down moves into results")
          keyboard.keyClick(Qt.Key_Up,Qt.NoModifier,0)
          check(search.activeFocus,"Up from first row returns to search")
          search.text="definitelynomatchemojiquery"
          check(grid.count===0 && inspect.findChild(inbox,"emojiNoResults").visible,"no-results state is explicit")
          inbox.closeEmojiPicker()
          console.log("OMACHAT_EMOJI_INBOX_PASS")
          Qt.callLater(Qt.quit);return
        }
        restart()
      } catch(error) { console.error("OMACHAT_EMOJI_INBOX_FAIL",error);Qt.callLater(Qt.quit) }
    }
  }
}
