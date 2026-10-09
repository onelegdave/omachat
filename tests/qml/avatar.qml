import QtQuick
import Quickshell
import "Chat" as Chat

ShellRoot {
  id: root
  property int step: 0

  function findImage(item) {
    if (item && item.status !== undefined && item.source !== undefined) return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findImage(children[i])
      if (found) return found
    }
    return null
  }

  function findInitials(item) {
    if (item && item.text === "WA") return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findInitials(children[i])
      if (found) return found
    }
    return null
  }

  function findCircleMask(item) {
    if (item && item.objectName === "avatarMask") return item
    var children = item && item.children ? item.children : []
    for (var i = 0; i < children.length; i++) {
      var found = findCircleMask(children[i])
      if (found) return found
    }
    return null
  }

  FloatingWindow {
    implicitWidth: 180
    implicitHeight: 120
    visible: true

    Chat.Avatar {
      id: avatar
      anchors.centerIn: parent
      width: 64
      height: 64
      initials: "WA"
      imagePath: ""
    }
  }

  Timer {
    interval: 100
    repeat: true
    running: true
    onTriggered: {
      try {
        var image = root.findImage(avatar)
        var initials = root.findInitials(avatar)
        var circleMask = root.findCircleMask(avatar)
        if (root.step === 0) {
          if (!image || !initials || !initials.visible || image.visible) return
          if (avatar.radius !== Math.min(avatar.width, avatar.height) / 2) throw new Error("initials fallback background is not circular")
          if (avatar.color.a < 1) throw new Error("initials fallback background is not opaque")
          if (Math.abs(initials.x + initials.width / 2 - avatar.width / 2) > 1 || Math.abs(initials.y + initials.height / 2 - avatar.height / 2) > 1) throw new Error("fallback initials are not centered")
          avatar.imagePath = Quickshell.env("OMACHAT_TEST_IMAGE")
        } else if (root.step === 1) {
          if (!image || image.status !== Image.Ready) return
          if (!image.visible || initials.visible) throw new Error("loaded photo did not replace initials")
          if (!image.layer.enabled || !image.layer.effect || !circleMask || !circleMask.layer.enabled || circleMask.visible) throw new Error("loaded photo has no circular mask")
          if (circleMask.width !== image.width || circleMask.height !== image.height || circleMask.radius !== Math.min(circleMask.width, circleMask.height) / 2) throw new Error("photo mask is not circular")
          if (String(image.source) !== "file://" + Quickshell.env("OMACHAT_TEST_IMAGE")) throw new Error("photo source does not follow Avatar.imagePath")
          avatar.imagePath = Quickshell.env("OMACHAT_TEST_IMAGE_SECOND")
        } else if (root.step === 2) {
          if (!image || image.status !== Image.Ready || String(image.source) !== "file://" + Quickshell.env("OMACHAT_TEST_IMAGE_SECOND")) return
          if (!image.visible || initials.visible) throw new Error("replacement photo did not remain visible")
          console.log("OMACHAT_AVATAR_PASS initials fallback, photo decode, circular mask, and changed-path reload")
          Qt.quit()
          return
        }
        root.step++
      } catch (error) {
        console.error("OMACHAT_AVATAR_FAIL", error)
        Qt.quit()
      }
    }
  }
}
