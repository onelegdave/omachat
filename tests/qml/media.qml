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
  property int mediaCalls: 0
  property int callsAfterCorruptLoad: 0
  property var photo: ({id:"photo",conversationID:"a",timestamp:1000000,fromMe:false,attachments:[{key:"photo-key",isImage:true,mimeType:"image/svg+xml"}]})
  property var corruptPhoto: ({id:"corrupt",conversationID:"a",timestamp:3000000,fromMe:false,attachments:[{key:"corrupt-key",isImage:true,mimeType:"image/png"}]})
  property var videoGif: ({id:"video-gif",conversationID:"a",timestamp:4000000,fromMe:false,attachments:[{key:"video-gif-key",isGif:true,isVideo:true,mimeType:"video/x-f4v",name:"clip.f4v"}]})
  property var unpreviewable: ({id:"no-preview",conversationID:"a",timestamp:5000000,fromMe:false,attachments:[{key:"no-preview-key",isImage:true,mimeType:"image/png",name:"document.png"}]})
  property int callsBeforeVideo: 0
  property var lastMediaParams: null
  function check(ok, label) { if (!ok) throw new Error(label); console.log("PASS:", label) }
  function thumb(item) {
    if (item && item.ready !== undefined && item.maxEdge !== undefined) return item
    var children = item ? item.children : []
    for (var i=0;i<children.length;i++) { var match=thumb(children[i]); if(match) return match }
    return null
  }
  function named(item, name) {
    if (item && item.objectName === name) return item
    var children = item ? item.children : []
    for (var i=0;i<children.length;i++) { var match=named(children[i],name); if(match) return match }
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
      if (method === "media") { root.mediaCalls++; root.pending=callback; root.lastMediaParams=params; return }
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
          root.pending(true,{path:"/nonexistent/original.png",preview:Quickshell.env("OMACHAT_TEST_IMAGE")})
        }
        if(root.step===2) {
          root.check(root.originalThumb.ready,"downloaded photo decodes")
          root.check(root.originalThumb.path===Quickshell.env("OMACHAT_TEST_IMAGE"),"the thumbnail decodes the helper preview, not the original")
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
          inbox.mergeMessage(root.corruptPhoto)
        }
        if(root.step===6) {
          root.check(!!root.pending,"corrupt photo requests media once")
          root.pending(true,{path:"/dev/null",preview:"/dev/null"})
          root.callsAfterCorruptLoad=root.mediaCalls
        }
        if(root.step===7) {
          root.check(inbox.mediaRequests["corrupt-key"]==="failed","decode failure becomes a stable failed state")
          root.check(root.mediaCalls===root.callsAfterCorruptLoad,"decode failure does not redownload in a loop")
          root.callsBeforeVideo=root.mediaCalls
          inbox.mergeMessage(root.videoGif)
        }
        if(root.step===8) {
          root.check(root.mediaCalls===root.callsBeforeVideo+1 && root.lastMediaParams.animation===true,"video-backed GIF asks the helper for an animated preview")
          root.pending(true,{path:"/tmp/clip.mp4",preview:Quickshell.env("OMACHAT_TEST_IMAGE")})
        }
        if(root.step===9) {
          var videoRow=list.itemAtIndex(list.count-1)
          var gifThumb=root.thumb(videoRow)
          root.check(!root.named(videoRow,"loopingVideoThumb"),"video-backed GIF has no inline player in the shell")
          root.check(!!gifThumb && gifThumb.path===Quickshell.env("OMACHAT_TEST_IMAGE"),"video-backed GIF shows the helper's animation")
          root.check(!root.named(videoRow,"openVideoExternally").visible,"animated clip hides Open video")
          root.callsBeforeVideo=root.mediaCalls
          inbox.mergeMessage(root.unpreviewable)
        }
        if(root.step===10) {
          root.check(root.mediaCalls===root.callsBeforeVideo+1,"image without a preview is requested once")
          root.pending(true,{path:"/tmp/original-document.png"})
        }
        if(root.step===11) {
          var row=list.itemAtIndex(list.count-1)
          var chip=root.named(row,"openImageExternally")
          root.check(inbox.mediaRequests["no-preview-key"]==="external","image without a preview is offered externally")
          root.check(!!chip && chip.visible,"image without a preview shows Open image")
          var noPreviewThumb=root.thumb(row)
          root.check(!noPreviewThumb || noPreviewThumb.path==="","the original is never decoded in the shell")
          console.log("OMACHAT_MEDIA_PASS"); Qt.quit()
        }
        root.step++
      } catch(error) { console.error("OMACHAT_MEDIA_FAIL",error); Qt.quit() }
    }
  }
}
