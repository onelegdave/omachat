import QtQuick
import qs.Commons
import qs.Ui
import "Model.js" as Model

Column {
  id: root
  objectName: "serviceOptions"
  property var service: null
  property string fontFamily: Style.font.family
  property real uiScale: 1
  property var selected: []
  property bool dirty: false
  property string resultText: ""
  property bool resultIsError: false
  readonly property bool ready: service && service.servicesConfigLoaded === true
  readonly property bool saving: service && service.savingServices === true
  readonly property color ink: Model.readableInk(Color.popups.background, Color.popups.text, 7)
  readonly property color errorInk: Model.readableInk(Color.popups.background, Color.urgent)
  function fs(n) { return Math.max(12,Math.round(n*uiScale)) }
  spacing: Style.space(10)
  function reload() {
    selected = ready ? service.enabledServices.slice() : []
    dirty = false
  }
  function choose(net, enabled) {
    if (!ready || saving) return
    var next=selected.filter(function(n) { return n !== net })
    if(enabled) next.push(net)
    selected=["gmessages","whatsapp","telegram","messenger"].filter(function(n) { return next.indexOf(n)>=0 })
    dirty=true
    resultText=""
  }
  function save() {
    if (!ready || saving || typeof service.setEnabledServices !== "function") return
    resultText=""
    service.setEnabledServices(selected.slice(),function(ok,res) {
      root.resultIsError=!ok
      if(ok) { root.reload(); root.resultText="Service choices saved." }
      else root.resultText=String(res)
    })
  }
  onServiceChanged: reload()
  Component.onCompleted: reload()
  Connections {
    target: root.service
    ignoreUnknownSignals: true
    function onServicesConfigLoadedChanged() { if(!root.dirty) root.reload() }
    function onEnabledServicesChanged() { if(!root.dirty && !root.saving) root.reload() }
  }
  Text {
    width: parent.width; wrapMode: Text.Wrap
    text: root.service && root.service.serviceSelectionRequired ? "Choose your services" : "Enabled services"
    color: root.ink; font.family: root.fontFamily; font.pixelSize: root.fs(Style.font.heading); font.bold:true
  }
  Text {
    width: parent.width; wrapMode: Text.Wrap
    text: "Turn off any service you do not want. Applying changes briefly restarts the shared helper, so other enabled services reconnect too. A disabled service does not start again and its tab is hidden. Credentials are kept; Unpair is separate. You may turn off all services. No dependencies are installed."
    color: root.ink; font.family: root.fontFamily; font.pixelSize: root.fs(Style.font.body)
  }
  Text {
    width: parent.width; wrapMode: Text.Wrap
    text: "Text drafts remain while the shell is running. Leaving a service stops recording and clears pending attachments. A message already submitted may still arrive; do not resend it without checking the conversation."
    color: root.ink; font.family: root.fontFamily; font.pixelSize: root.fs(Style.font.body)
  }
  Repeater {
    model: [
      {id:"gmessages",name:"Google Messages",hint:"Browser pairing needs SQLite, libsecret, and an unlocked keyring."},
      {id:"whatsapp",name:"WhatsApp",hint:"QR pairing needs qrencode and your phone's Linked devices screen."},
      {id:"telegram",name:"Telegram",hint:"Needs your API credentials, qrencode, and your phone's Devices screen."},
      {id:"messenger",name:"Messenger",hint:"Browser pairing needs SQLite, libsecret, an unlocked keyring, and an active Facebook or Messenger login."}
    ]
    Column {
      required property var modelData
      width: root.width; spacing:Style.space(4)
      Text {
        width:parent.width; wrapMode:Text.Wrap
        text:modelData.name + " · " + modelData.hint
        color:root.ink; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
      }
      ChoiceGroup {
        objectName: "serviceToggle-" + modelData.id
        width:parent.width
        enabled:root.ready && !root.saving
        options:[{value:"on",label:"Enabled"},{value:"off",label:"Disabled"}]
        value:root.selected.indexOf(modelData.id)>=0 ? "on" : "off"
        foreground:root.ink
        fontFamily:root.fontFamily; fontSize:root.fs(Style.font.body)
        onChanged:function(v) { root.choose(modelData.id,v === "on") }
      }
    }
  }
  Flow {
    width:parent.width; spacing:Style.space(8)
    Button {
      objectName:"saveServicesButton"
      text:root.saving ? "Applying..." : "Apply service choices"
      enabled:root.ready && !root.saving && (root.dirty || root.service.serviceSelectionRequired)
      bordered:true; foreground:root.ink; fontFamily:root.fontFamily
      onClicked:root.save()
    }
    Button {
      text:"Reset choices"; enabled:root.dirty && !root.saving
      bordered:true; foreground:root.ink; fontFamily:root.fontFamily
      onClicked: { root.reload(); root.resultText="" }
    }
  }
  Text {
    width:parent.width; wrapMode:Text.Wrap
    text: root.resultText || (root.service && root.service.servicesError ? root.service.servicesError : (!root.ready ? "Connect to the updated helper to load service choices." : ""))
    visible:text !== ""
    color:root.resultIsError || (root.service && root.service.servicesError) ? root.errorInk : root.ink; font.family:root.fontFamily; font.pixelSize:root.fs(Style.font.body)
  }
}
