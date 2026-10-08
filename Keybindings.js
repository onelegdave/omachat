// @ts-nocheck
.pragma library

var ACTIONS = [
  { id: "help", label: "Show keyboard help / actions", category: "Navigation", context: "navigation", defaultShortcut: "?", alternateShortcut: "Ctrl+Shift+P", keywords: "keyboard shortcuts help actions" },
  { id: "search", label: "Search conversations", category: "Navigation", context: "navigation", defaultShortcut: "/", alternateShortcut: "Ctrl+F", keywords: "find filter contacts" },
  { id: "compose", label: "Focus message composer", category: "Conversation", context: "navigation", defaultShortcut: "i", keywords: "write message reply type" },
  { id: "history", label: "Focus message history", category: "Conversation", context: "navigation", defaultShortcut: "m", keywords: "messages scroll" },
  { id: "nextUnread", label: "Open next unread conversation", category: "Navigation", context: "navigation", defaultShortcut: "u", keywords: "unread" },
  { id: "newConversation", label: "New conversation", category: "Conversation", context: "global", defaultShortcut: "Ctrl+N", keywords: "new chat group" },
  { id: "settings", label: "Open settings", category: "Application", context: "global", defaultShortcut: "Ctrl+,", keywords: "preferences configure" },
  { id: "emojiPicker", label: "Open emoji picker", category: "Conversation", context: "global", defaultShortcut: "Ctrl+E", keywords: "emoji reaction" },
  { id: "attach", label: "Attach file or photo", category: "Conversation", context: "global", defaultShortcut: "Ctrl+O", keywords: "upload image media" },
  { id: "refresh", label: "Refresh conversations", category: "Application", context: "navigation", defaultShortcut: "r", alternateShortcut: "Ctrl+R", keywords: "sync reload" },
  { id: "service.gmessages", label: "Switch to Google Messages", category: "Services", context: "navigation", defaultShortcut: "1", keywords: "google messages" },
  { id: "service.whatsapp", label: "Switch to WhatsApp", category: "Services", context: "navigation", defaultShortcut: "2", keywords: "" },
  { id: "service.telegram", label: "Switch to Telegram", category: "Services", context: "navigation", defaultShortcut: "3", keywords: "" },
  { id: "service.messenger", label: "Switch to Messenger", category: "Services", context: "navigation", defaultShortcut: "4", keywords: "facebook meta" },
  { id: "service.signal", label: "Switch to Signal", category: "Services", context: "navigation", defaultShortcut: "5", keywords: "" }
]

var RESERVED = ["Tab", "Backtab", "Left", "Right", "Up", "Down", "Home", "End", "PageUp", "PageDown", "Enter", "Return", "Escape", "Space", "Backspace", "Delete", "Shift", "Ctrl", "Alt", "Meta"]

function actions() {
  return ACTIONS.map(function(action) { return Object.assign({}, action) })
}

function actionsForQuery(query, overrides, enabledServices) {
  var needle = String(query || "").trim().toLowerCase()
  var validated = validateOverrides(overrides || {})
  return ACTIONS.filter(function(action) {
    if (action.id.indexOf("service.") === 0 && Array.isArray(enabledServices)
        && enabledServices.indexOf(action.id.substring(8)) < 0) return false
    var haystack = (action.label + " " + action.category + " " + action.keywords).toLowerCase()
    return !needle || haystack.indexOf(needle) >= 0
  }).map(function(action) {
    var copy = Object.assign({}, action)
    copy.shortcut = effectiveShortcuts(action, validated.ok ? validated.overrides : {}).join(" / ")
    return copy
  })
}

function canonicalKey(key) {
  var raw = String(key || "").trim()
  if (!raw) return ""
  var low = raw.toLowerCase()
  var names = { control: "Ctrl", ctrl: "Ctrl", alt: "Alt", option: "Alt", meta: "Meta", super: "Meta", cmd: "Meta", command: "Meta", shift: "Shift", esc: "Escape", return: "Enter", kp_enter: "Enter", backtab: "Backtab", pageup: "PageUp", pagedown: "PageDown", spacebar: "Space", plus: "Plus", minus: "Minus", comma: ",", slash: "/", question: "?" }
  if (names[low]) return names[low]
  if (raw.length === 1) return raw
  return raw.charAt(0).toUpperCase() + raw.substring(1)
}

function normalizeShortcut(value) {
  var raw = String(value || "").trim()
  if (!raw || raw.length > 40) return ""
  var parts = raw.split("+").map(function(part) { return part.trim() }).filter(function(part) { return part !== "" })
  if (!parts.length) return ""
  var key = canonicalKey(parts[parts.length - 1])
  var modifiers = {}
  for (var i = 0; i < parts.length - 1; i++) {
    var modifier = canonicalKey(parts[i])
    if ((modifier !== "Ctrl" && modifier !== "Alt" && modifier !== "Meta" && modifier !== "Shift") || modifiers[modifier]) return ""
    modifiers[modifier] = true
  }
  if (key.length !== 1 || key.charCodeAt(0) < 33 || key.charCodeAt(0) > 126 || key === "+") return ""
  if (key.length === 1) {
    if (modifiers.Shift && /^[A-Za-z]$/.test(key)) key = key.toLowerCase()
    else if (modifiers.Shift) delete modifiers.Shift
    else key = key.toLowerCase()
  }
  if (RESERVED.indexOf(key) >= 0) return ""
  var prefix = []
  ;["Ctrl", "Alt", "Meta"].forEach(function(modifier) { if (modifiers[modifier]) prefix.push(modifier) })
  if (modifiers.Shift) prefix.push("Shift")
  return prefix.concat([key]).join("+")
}

function isReserved(shortcut) {
  return RESERVED.indexOf(shortcut.split("+").pop()) >= 0
}

function shortcutContext(action, shortcut) {
  if (action.context === "global") return "global"
  return shortcut.indexOf("Ctrl+") === 0 || shortcut.indexOf("Alt+") === 0 || shortcut.indexOf("Meta+") === 0 ? "global" : action.context
}

function validateOverrides(overrides) {
  var supplied = overrides && typeof overrides === "object" && !Array.isArray(overrides) ? overrides : {}
  var known = {}
  ACTIONS.forEach(function(action) { known[action.id] = action })
  var normalized = {}
  var seen = {}
  for (var id in supplied) {
    if (!Object.prototype.hasOwnProperty.call(supplied, id)) continue
    if (!known[id]) return { ok: false, error: "Unknown keyboard action: " + id }
    var requested = Array.isArray(supplied[id]) ? supplied[id] : [supplied[id]]
    var values = []
    for (var i = 0; i < requested.length; i++) {
      var shortcut = normalizeShortcut(requested[i])
      if (!shortcut) {
        if (String(requested[i] || "").trim() === "") continue
        return { ok: false, error: "Unsupported shortcut for " + known[id].label + "." }
      }
      if (isReserved(shortcut)) return { ok: false, error: shortcut + " is reserved for standard keyboard navigation." }
      var context = shortcutContext(known[id], shortcut)
      var key = shortcut + "@" + context
      if (seen[key]) return { ok: false, error: "Shortcut " + shortcut + " is assigned to both " + known[seen[key]].label + " and " + known[id].label + "." }
      seen[key] = id
      values.push(shortcut)
    }
    normalized[id] = values
  }
  for (var a = 0; a < ACTIONS.length; a++) {
    var first = ACTIONS[a]
    var firstShortcuts = effectiveShortcuts(first, normalized)
    for (var b = a + 1; b < ACTIONS.length; b++) {
      var second = ACTIONS[b]
      var secondShortcuts = effectiveShortcuts(second, normalized)
      for (var x = 0; x < firstShortcuts.length; x++) {
        for (var y = 0; y < secondShortcuts.length; y++) {
          var firstContext = shortcutContext(first, firstShortcuts[x])
          var secondContext = shortcutContext(second, secondShortcuts[y])
          if (firstShortcuts[x] === secondShortcuts[y]
              && (firstContext === secondContext || firstContext === "global" || secondContext === "global"))
            return { ok: false, error: "Shortcut " + firstShortcuts[x] + " is assigned to both " + first.label + " and " + second.label + "." }
        }
      }
    }
  }
  return { ok: true, overrides: normalized }
}

function effectiveShortcuts(action, overrides) {
  if (overrides && Object.prototype.hasOwnProperty.call(overrides, action.id)) {
    var custom = Array.isArray(overrides[action.id]) ? overrides[action.id] : [overrides[action.id]]
    return custom.map(normalizeShortcut).filter(function(shortcut) { return !!shortcut })
  }
  return [action.defaultShortcut, action.alternateShortcut].map(normalizeShortcut).filter(function(shortcut) { return !!shortcut })
}

function eventShortcut(event) {
  if (!event || event.isComposing || event.composing) return ""
  var key = String(event.text || "")
  var numericKey = Number(event.key)
  if (!key && numericKey >= 32 && numericKey <= 126) key = String.fromCharCode(numericKey)
  if (event.ctrl || event.alt || event.meta) {
    var eventKey = numericKey >= 65 && numericKey <= 90
      ? String.fromCharCode(numericKey)
      : (numericKey >= 48 && numericKey <= 57 ? String.fromCharCode(numericKey) : (key || String(event.key || "")))
    if (eventKey.length === 1 && /[a-z0-9]/i.test(eventKey)) key = eventKey
  }
  if (key.length !== 1) key = canonicalKey(event.key || "")
  if (!key) return ""
  var parts = []
  if (event.ctrl) parts.push("Ctrl")
  if (event.alt) parts.push("Alt")
  if (event.meta) parts.push("Meta")
  if (event.shift) parts.push("Shift")
  parts.push(key)
  return normalizeShortcut(parts.join("+"))
}

function match(shortcut, event, context) {
  var expected = normalizeShortcut(shortcut)
  var actual = eventShortcut(event)
  if (!expected || expected !== actual) return false
  var modified = expected.indexOf("Ctrl+") === 0 || expected.indexOf("Alt+") === 0 || expected.indexOf("Meta+") === 0
  if (context === "editing" && !modified) return false
  return true
}

function resolve(event, context, state) {
  var options = state || {}
  var validation = validateOverrides(options.overrides || {})
  if (!validation.ok) return { id: "", error: validation.error }
  for (var i = 0; i < ACTIONS.length; i++) {
    var action = ACTIONS[i]
    if (action.id.indexOf("service.") === 0 && options.enabledServices && options.enabledServices.indexOf(action.id.substring(8)) < 0) continue
    var shortcuts = effectiveShortcuts(action, validation.overrides)
    for (var j = 0; j < shortcuts.length; j++) {
      if (action.context === "global" || context === "navigation" || match(shortcuts[j], event, "editing")) {
        if (match(shortcuts[j], event, context)) return { id: action.id, action: action }
      }
    }
  }
  return { id: "" }
}

function displayShortcut(shortcut) {
  return normalizeShortcut(shortcut).replace("Meta", "Super")
}
