import QtQuick
import QtQuick.Controls as Controls
import qs.Commons
import qs.Ui as Ui

// Single-line text field styled like Omarchy's Ui.TextField.
//
// Qt's native Wayland paste buffers the provider before any length limit, so
// every user paste entry point goes through BoundedPaste instead. Editing uses
// a plain-text TextEdit rather than TextInput: through Qt 6.12.0 an editable
// TextInput reads the whole clipboard on every clipboard change (QTBUG-149610),
// while TextEdit only checks the offered formats.
FocusScope {
  id: root
  property color foreground: Color.foreground
  property color accent: Color.accent
  property color selectionTint: Style.selectionFillFor(foreground, accent)
  property bool hasCursor: false
  property real horizontalPadding: Style.spacing.controlPaddingX
  property real verticalPadding: Style.spacing.inputPaddingY
  property string placeholderText: ""
  property color placeholderTextColor: Qt.darker(foreground, 1.6)
  property int maximumLength: 32767
  // Message fields keep pasted line breaks; others flatten them to spaces.
  property bool allowLineBreaks: false
  property bool pasteAllowed: true
  property string pasteContext: ""
  readonly property bool pasteBusy: paste.busy
  property alias pasteError: paste.error
  property alias text: editor.text
  property alias font: editor.font
  property alias color: editor.color
  property alias selectionColor: editor.selectionColor
  property alias selectedTextColor: editor.selectedTextColor
  property alias readOnly: editor.readOnly
  property alias selectByMouse: editor.selectByMouse
  property alias inputMethodHints: editor.inputMethodHints
  property alias cursorPosition: editor.cursorPosition
  readonly property alias selectionStart: editor.selectionStart
  readonly property alias selectionEnd: editor.selectionEnd
  readonly property alias selectedText: editor.selectedText
  readonly property alias length: editor.length
  readonly property alias preeditText: editor.preeditText
  readonly property alias canUndo: editor.canUndo
  readonly property alias canRedo: editor.canRedo
  readonly property alias hovered: hover.hovered
  property Item background: Ui.BorderSurface {
    color: Style.controlFill(root._focused, root._hot, root.foreground, root.accent)
    borderSpec: root._borderSpec
    radius: Style.cornerRadius
  }
  signal accepted()
  signal editingFinished()

  readonly property bool _focused: activeFocus
  readonly property bool _hot: hovered || hasCursor
  readonly property var _borderSpec: Border.controlSpec(_focused ? "focus" : (_hot ? "hover-cursor" : "normal"), root.foreground, root.accent)
  readonly property real leftPadding: horizontalPadding + Border.left(_borderSpec)
  readonly property real rightPadding: horizontalPadding + Border.right(_borderSpec)
  readonly property real topPadding: verticalPadding + Border.top(_borderSpec)
  readonly property real bottomPadding: verticalPadding + Border.bottom(_borderSpec)
  readonly property real _lineHeight: editor.cursorRectangle.height
  property real _scrollX: 0
  property real _scrollY: 0
  property bool _sanitizing: false

  implicitWidth: Math.max(background ? background.implicitWidth : 0,
                          Math.max(editor.contentWidth, placeholder.implicitWidth) + leftPadding + rightPadding)
  implicitHeight: Math.max(background ? background.implicitHeight : 0,
                           Math.max(_lineHeight, placeholder.implicitHeight) + topPadding + bottomPadding)

  function select(start, end) { editor.select(start, end) }
  function selectAll() { editor.selectAll() }
  function deselect() { editor.deselect() }
  function insert(position, value) { editor.insert(position, value) }
  function remove(start, end) { editor.remove(start, end) }
  function undo() { editor.undo() }
  function redo() { editor.redo() }
  function cut() { editor.cut() }
  function copy() { editor.copy() }
  function positionAt(x, y) { return editor.positionAt(x - viewport.x - editor.x, y - viewport.y - editor.y) }
  function cancelPaste() { paste.cancel() }
  function requestPaste(primary) {
    if (paste.busy || !paste.allowed) return
    editor.forceActiveFocus()
    paste.request(primary)
  }

  // Typed, IME and assigned text follow the same rules as pasted text.
  function sanitize() {
    if (_sanitizing) return
    _sanitizing = true
    var pattern = allowLineBreaks ? /[\t\r\x00\u2028\u2029]/g : /[\t\r\n\x00\u2028\u2029]/g
    var value = editor.text, found = [], match
    while ((match = pattern.exec(value)) !== null) found.push(match)
    for (var i = found.length - 1; i >= 0; i--) {
      var c = found[i][0], at = found[i].index
      editor.remove(at, at + 1)
      if (c !== "\u0000") editor.insert(at, allowLineBreaks && c !== "\t" ? "\n" : " ")
    }
    var excess = editor.length - maximumLength
    if (excess > 0) {
      // Drop what was just typed, as TextInput refuses it, rather than the end.
      var cut = Math.max(0, Math.min(editor.cursorPosition, editor.length) - excess)
      editor.remove(cut, cut + excess)
    }
    _sanitizing = false
  }

  function ensureCursorVisible() {
    var r = editor.cursorRectangle
    var x = _scrollX, y = _scrollY
    if (r.x + r.width > x + viewport.width) x = r.x + r.width - viewport.width
    if (r.x < x) x = r.x
    _scrollX = Math.max(0, Math.min(x, editor.contentWidth + r.width - viewport.width))
    if (r.y + r.height > y + _lineHeight) y = r.y + r.height - _lineHeight
    if (r.y < y) y = r.y
    _scrollY = Math.max(0, y)
  }

  onBackgroundChanged: placeBackground()
  Component.onCompleted: placeBackground()
  function placeBackground() {
    if (!background) return
    background.parent = root
    background.z = -1
    background.anchors.fill = root
  }
  onMaximumLengthChanged: sanitize()
  onAllowLineBreaksChanged: sanitize()

  HoverHandler { id: hover }

  // Clicks on the padding still focus the field and place the cursor.
  MouseArea {
    anchors.fill: parent
    cursorShape: Qt.IBeamCursor
    onPressed: function(mouse) {
      editor.forceActiveFocus()
      editor.cursorPosition = root.positionAt(mouse.x, mouse.y)
    }
  }

  Item {
    id: viewport
    x: root.leftPadding
    y: root.topPadding
    width: Math.max(0, root.width - root.leftPadding - root.rightPadding)
    height: Math.max(0, root.height - root.topPadding - root.bottomPadding)
    clip: true
    onWidthChanged: root.ensureCursorVisible()

    Text {
      id: placeholder
      anchors.fill: parent
      text: root.placeholderText
      font: editor.font
      color: root.placeholderTextColor
      verticalAlignment: Text.AlignVCenter
      elide: Text.ElideRight
      visible: !editor.length && !editor.preeditText
      Accessible.ignored: true
    }

    TextEdit {
      id: editor
      objectName: "safeTextEditor"
      focus: true
      activeFocusOnTab: true
      x: -root._scrollX
      y: Math.round((viewport.height - root._lineHeight) / 2) - root._scrollY
      width: Math.max(viewport.width, implicitWidth)
      height: Math.max(viewport.height, implicitHeight)
      textFormat: TextEdit.PlainText
      wrapMode: TextEdit.NoWrap
      selectByMouse: true
      color: root.foreground
      selectionColor: root.selectionTint
      selectedTextColor: root.foreground
      font.family: Style.font.family
      font.pixelSize: Style.font.body
      Accessible.name: root.Accessible.name || root.placeholderText
      onTextChanged: root.sanitize()
      onCursorRectangleChanged: root.ensureCursorVisible()
      onContentWidthChanged: root.ensureCursorVisible()
      onEditingFinished: root.editingFinished()

      // Instance key handlers on the field run first, as they did on the old
      // TextField; pasteKeys runs before both so none can reopen native paste.
      Keys.priority: Keys.BeforeItem
      Keys.forwardTo: [pasteKeys, root]
      Keys.onShortcutOverride: function(event) {
        if (event.matches(StandardKey.Paste)) event.accepted = true
      }
      Keys.onPressed: function(event) {
        var mods = event.modifiers & (Qt.ControlModifier | Qt.AltModifier | Qt.MetaModifier)
        if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
          // TextInput's behavior: Return accepts; it never inserts a line break.
          root.accepted()
          root.editingFinished()
          if (!mods) event.accepted = true
        } else if ((event.key === Qt.Key_Tab || event.key === Qt.Key_Backtab) && !mods) {
          var next = editor.nextItemInFocusChain(event.key === Qt.Key_Tab && !(event.modifiers & Qt.ShiftModifier))
          if (next && next !== editor) next.forceActiveFocus(Qt.TabFocusReason)
          event.accepted = true
        }
      }
    }
  }

  Item {
    id: pasteKeys
    Keys.onPressed: function(event) {
      if (event.matches(StandardKey.Paste)) {
        event.accepted = true
        root.requestPaste(false)
      }
    }
  }

  BoundedPaste {
    id: paste
    allowed: root.enabled && root.visible && !root.readOnly && root.pasteAllowed
    focused: editor.activeFocus
    context: root.pasteContext
    maximumLength: root.maximumLength
    allowLineBreaks: root.allowLineBreaks
    currentText: editor.text
    cursorPosition: editor.cursorPosition
    selectionStart: editor.selectionStart
    selectionEnd: editor.selectionEnd
    onReady: function(start, end, value) {
      // Replacing through the selection is one undo step.
      editor.select(start, end)
      if (start === end) editor.insert(start, value)
      else editor.cursorSelection.text = value
      editor.cursorPosition = start + value.length
    }
  }

  // Consume both middle-button press and release; Qt pastes PRIMARY on release.
  MouseArea {
    anchors.fill: parent
    acceptedButtons: Qt.MiddleButton
    onPressed: function(mouse) {
      editor.forceActiveFocus()
      editor.cursorPosition = root.positionAt(mouse.x, mouse.y)
    }
    onClicked: root.requestPaste(true)
  }

  // Do not use Qt's paste action or bind to editor.canPaste. No native payload
  // read is needed to offer paste; the bounded reader handles empty/nontext data.
  Controls.ContextMenu.menu: Controls.Menu {
    popupType: Controls.Popup.Item
    Controls.MenuItem { text: "Undo"; enabled: root.canUndo; onTriggered: root.undo() }
    Controls.MenuItem { text: "Redo"; enabled: root.canRedo; onTriggered: root.redo() }
    Controls.MenuSeparator {}
    Controls.MenuItem { text: "Cut"; enabled: !root.readOnly && root.selectedText.length > 0; onTriggered: root.cut() }
    Controls.MenuItem { text: "Copy"; enabled: root.selectedText.length > 0; onTriggered: root.copy() }
    Controls.MenuItem {
      objectName: "boundedPasteAction"
      text: "Paste"
      enabled: root.enabled && !root.readOnly && root.pasteAllowed && !root.pasteBusy
      onTriggered: root.requestPaste(false)
    }
    Controls.MenuItem { text: "Delete"; enabled: !root.readOnly && root.selectedText.length > 0; onTriggered: root.remove(root.selectionStart, root.selectionEnd) }
    Controls.MenuSeparator {}
    Controls.MenuItem { text: "Select all"; enabled: root.length > 0; onTriggered: root.selectAll() }
  }

  Controls.ToolTip.text: pasteError
  Controls.ToolTip.visible: pasteError !== "" && (activeFocus || hovered)
  Controls.ToolTip.timeout: 6000
}
