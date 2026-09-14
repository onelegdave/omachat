import QtQuick
import QtTest
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0
  property var originalRow: null
  property var originalThumb: null
  property var pending: null
  property var photo: ({id:"photo",conversationID:"a",timestamp:1000000,fromMe:false,attachments:[{key:"photo-key",isImage:true,mimeType:"image/svg+xml"}]})
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function thumb(item) {
    if (item && item.ready !== undefined && item.maxEdge !== undefined) return item
    var children = item ? item.children : []
    for (var i=0;i<children.length;i++) { var match=thumb(children[i]); if(match) return match }
    return null
  }
  QtObject {
    id: fake
    property bool connected: true
    property string state: "connected"
    property var status: ({phoneOK:true})
    property string refreshError: ""
    property var conversations: [{id:"a",name:"Synthetic photo test"}]
    signal messageReceived(var message)
    signal conversationUpdated(var conversation)
    signal paired()
    function call(method, params, callback) {
      if (method === "media") { root.pending=callback; return }
      if (callback) callback(true, method === "messages" ? {messages:[]} : {})
    }
  }
  FloatingWindow {
    implicitWidth:920; implicitHeight:580; visible:true
    Chat.InboxView { id:inbox; anchors.fill:parent; service:fake }
    TestResult { id:inspect }
  }
  Timer {
    interval:250; repeat:true; running:true
    onTriggered: {
      try {
        var list=inspect.findChild(inbox,"messageList")
        if(root.step===0) { inbox.selectConversation("a"); inbox.mergeMessage(root.photo) }
        if(root.step===1) {
          list.forceLayout()
          root.originalRow=list.itemAtIndex(1)
          root.originalThumb=root.thumb(root.originalRow)
          root.check(!!root.originalThumb && !root.originalThumb.ready,"photo starts with a loading placeholder")
          root.check(root.originalThumb.parent.mediaLoading,"loading label remains until image decode")
          root.check(root.originalThumb.parent.height>=96,"loading photo reserves thumbnail space")
          root.pending(true,{path:Quickshell.env("OMACHAT_TEST_IMAGE")})
        }
        if(root.step===2) {
          root.check(root.originalThumb.ready,"downloaded photo decodes")
          root.check(!root.originalThumb.parent.mediaLoading,"ready photo replaces loading label")
          inbox.mergeMessage(JSON.parse(JSON.stringify(root.photo)))
        }
        if(root.step===3) {
          root.check(list.itemAtIndex(1)===root.originalRow,"duplicate snapshot retains photo delegate")
          root.check(root.thumb(root.originalRow)===root.originalThumb,"duplicate snapshot retains decoded image")
          inbox.mergeMessage(Object.assign({},root.photo,{read:true}))
        }
        if(root.step===4) {
          root.check(list.itemAtIndex(1)===root.originalRow,"receipt update retains photo delegate")
          root.check(root.thumb(root.originalRow)===root.originalThumb && root.originalThumb.ready,"receipt update retains decoded attachment")
          inbox.mergeMessage({id:"next",conversationID:"a",timestamp:2000000,text:"Another message"})
        }
        if(root.step===5) {
          root.check(list.itemAtIndex(1)===root.originalRow && root.thumb(root.originalRow)===root.originalThumb,"new message does not rebuild existing photo")
          console.log("OMACHAT_MEDIA_PASS"); Qt.quit()
        }
        root.step++
      } catch(error) { console.error("OMACHAT_MEDIA_FAIL",error); Qt.quit() }
    }
  }
}
