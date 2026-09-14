.pragma library

// Formatting helpers for the OmaChat panel. Kept out of QML so the
// panel stays layout, and so these stay checkable by eye.

function channelLum(x) {
  var n = Number(x)
  if (!isFinite(n)) return 0
  return n <= 0.04045 ? n / 12.92 : Math.pow((n + 0.055) / 1.055, 2.4)
}

function luminance(c) {
  if (!c) return 0
  return 0.2126 * channelLum(c.r) + 0.7152 * channelLum(c.g) + 0.0722 * channelLum(c.b)
}

function contrastRatio(a, b) {
  var x = luminance(a)
  var y = luminance(b)
  var hi = Math.max(x, y)
  var lo = Math.min(x, y)
  return (hi + 0.05) / (lo + 0.05)
}

function mix(a, b, t) {
  var k = Math.max(0, Math.min(1, Number(t) || 0))
  return Qt.rgba(
    a.r + (b.r - a.r) * k,
    a.g + (b.g - a.g) * k,
    a.b + (b.b - a.b) * k,
    1)
}

function inkOn(surface, light, dark) {
  return contrastRatio(light, surface) >= contrastRatio(dark, surface) ? light : dark
}

// Pull a tinted surface toward `base` until one of the inks clears minRatio.
function readableSurface(base, tint, startT, inkA, inkB, minRatio) {
  var floor = minRatio || 4.5
  var t = startT
  var best = mix(base, tint, startT)
  var bestScore = 0
  for (var i = 0; i < 10; i++) {
    var s = mix(base, tint, t)
    var score = Math.max(contrastRatio(inkA, s), contrastRatio(inkB, s))
    if (score > bestScore) {
      best = s
      bestScore = score
    }
    if (score >= floor) return s
    t = Math.max(0.12, t - 0.07)
  }
  return best
}

function outgoingFill(bg, accent, fg) {
  return readableSurface(bg, accent, 0.48, fg, bg, 4.5)
}

function incomingFill(bg, fg) {
  return readableSurface(bg, fg, 0.14, fg, bg, 4.5)
}

function metaInk(ink, surface) {
  var faded = mix(surface, ink, 0.72)
  if (contrastRatio(faded, surface) >= 3.0) return faded
  return ink
}

function toDate(micros) {
  return new Date(Math.floor((Number(micros) || 0) / 1000))
}

function sameDay(a, b) {
  return a.getFullYear() === b.getFullYear()
    && a.getMonth() === b.getMonth()
    && a.getDate() === b.getDate()
}

function relativeTime(micros) {
  var n = Number(micros) || 0
  if (n === 0) return ""
  var d = toDate(n)
  var now = new Date()
  if (sameDay(d, now)) return Qt.formatDateTime(d, "h:mm AP")
  var yesterday = new Date(now.getTime() - 86400000)
  if (sameDay(d, yesterday)) return "Yesterday"
  if (now.getTime() - d.getTime() < 7 * 86400000) return Qt.formatDateTime(d, "ddd")
  if (d.getFullYear() === now.getFullYear()) return Qt.formatDateTime(d, "MMM d")
  return Qt.formatDateTime(d, "MMM d, yyyy")
}

function formatDuration(secs) {
  var n = Math.max(0, Math.floor(Number(secs) || 0))
  var m = Math.floor(n / 60)
  var s = n % 60
  return m + ":" + (s < 10 ? "0" + s : String(s))
}

function bubbleTime(micros) {
  var n = Number(micros) || 0
  if (n === 0) return ""
  return Qt.formatDateTime(toDate(n), "h:mm AP")
}

function dayLabel(micros) {
  var d = toDate(micros)
  var now = new Date()
  if (sameDay(d, now)) return "Today"
  var yesterday = new Date(now.getTime() - 86400000)
  if (sameDay(d, yesterday)) return "Yesterday"
  if (d.getFullYear() === now.getFullYear()) return Qt.formatDateTime(d, "dddd, MMMM d")
  return Qt.formatDateTime(d, "MMMM d, yyyy")
}

function dayKey(micros) {
  var d = toDate(micros)
  return d.getFullYear() + "-" + d.getMonth() + "-" + d.getDate()
}

function escapeHtml(s) {
  return String(s || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
}

function safeHttpUrl(raw) {
  var s = String(raw || "").trim()
  if (s.indexOf("https://") !== 0 && s.indexOf("http://") !== 0) return ""
  if (s.indexOf("\n") >= 0 || s.indexOf("\r") >= 0 || s.indexOf(" ") >= 0) return ""
  if (s.toLowerCase().indexOf("javascript:") >= 0) return ""
  return s
}

function linkify(raw) {
  var e = escapeHtml(raw)
  return e.replace(/https?:\/\/[^\s<&]+/gi, function(m) {
    var trail = ""
    while (m.length && ".,;:!?)".indexOf(m.charAt(m.length - 1)) >= 0) {
      trail = m.charAt(m.length - 1) + trail
      m = m.substring(0, m.length - 1)
    }
    if (!safeHttpUrl(m)) return m + trail
    return '<a href="' + m + '">' + m + "</a>" + trail
  })
}

function isGif(mime, name, path) {
  var m = String(mime || "").toLowerCase()
  if (m === "image/gif") return true
  var n = String(name || path || "").toLowerCase()
  var slash = n.lastIndexOf("/")
  if (slash >= 0) n = n.substring(slash + 1)
  var q = n.indexOf("?")
  if (q >= 0) n = n.substring(0, q)
  return n.length >= 4 && n.substring(n.length - 4) === ".gif"
}

function digitCount(s) {
  return String(s || "").replace(/\D/g, "").length
}

function extractCopyTargets(text) {
  var s = String(text || "")
  if (!s) return []
  var out = []
  var seen = {}
  function add(value) {
    var v = String(value || "").trim()
    if (!v || v === "<#>") return
    if (seen[v]) return
    seen[v] = true
    out.push(v)
  }

  var m
  var g = s.match(/\bG-\d{4,8}\b/gi)
  if (g) {
    for (var i = 0; i < g.length; i++) add(g[i])
  }

  // WhatsApp and similar: 872-151
  var hyphen = s.match(/\b\d{2,4}[-–.]\d{2,4}\b/g)
  if (hyphen) {
    for (var h = 0; h < hyphen.length; h++) {
      var token = hyphen[h]
      var digits = token.replace(/\D/g, "")
      if (digits.length < 5 || digits.length > 8) continue
      add(token)
    }
  }

  var digitRe = /\d{4,8}/g
  while ((m = digitRe.exec(s)) !== null) {
    var n = m[0]
    if (/^(19|20)\d{2}$/.test(n)) continue
    add(n)
  }

  // Mixed letters+digits: 4sgLq1p5sV6
  var mixed = s.match(/\b[A-Za-z0-9]{6,16}\b/g)
  if (mixed) {
    for (var a = 0; a < mixed.length; a++) {
      var t = mixed[a]
      if (!/[A-Za-z]/.test(t) || !/\d/.test(t)) continue
      if (/https?/i.test(t)) continue
      add(t)
    }
  }

  var phoneRe = /\+?\d[\d\s().-]{8,20}\d/g
  while ((m = phoneRe.exec(s)) !== null) {
    if (digitCount(m[0]) < 10) continue
    add(m[0].replace(/\s+/g, " ").trim())
  }

  return out
}

function previewText(conv) {
  if (!conv) return ""
  var text = (conv.preview || "").replace(/\s+/g, " ").trim()
  if (text === "") text = "No messages"
  return conv.previewMine ? "You: " + text : text
}

function humanSize(bytes) {
  var n = Number(bytes) || 0
  if (n <= 0) return ""
  var units = ["B", "KB", "MB", "GB"]
  var i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return (i === 0 ? n : n.toFixed(1)) + " " + units[i]
}

function avatarColor(hex, seed, fallback) {
  if (hex && hex.length > 0) return hex.charAt(0) === "#" ? hex : "#" + hex
  var s = String(seed || "")
  var h = 0
  for (var i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) & 0xffffff
  if (s.length === 0) return fallback
  return Qt.hsla((h % 360) / 360, 0.45, 0.5, 1)
}

function statusLine(status) {
  if (!status) return "Disconnected"
  switch (status.state) {
  case "connected":
    return status.phoneOK === false ? "Phone not responding" : "Connected"
  case "connecting":
    return "Connecting"
  case "pairing":
    return "Waiting for pairing"
  case "gaiaPairing":
    return "Confirm the emoji on your phone"
  case "unpaired":
    return "Not paired"
  case "disconnected":
    return "Reconnecting"
  case "error":
    return status.error ? status.error : "Error"
  }
  return String(status.state || "")
}

function groupMessages(messages) {
  var out = []
  var lastKey = ""
  for (var i = 0; i < messages.length; i++) {
    var m = messages[i]
    var k = dayKey(m.timestamp)
    if (k !== lastKey) {
      out.push({ kind: "day", key: "day-" + k, label: dayLabel(m.timestamp) })
      lastKey = k
    }
    var prev = i > 0 ? messages[i - 1] : null
    var startsRun = !prev || prev.fromMe !== m.fromMe || prev.senderID !== m.senderID
      || dayKey(prev.timestamp) !== k
    out.push({ kind: "msg", key: m.id || ("m" + i), message: m, startsRun: startsRun })
  }
  return out
}

function receiptLabel(msg) {
  if (!msg || !msg.fromMe) return ""
  if (msg.failed || msg.delivery === "failed") return "Failed"
  if (msg.pending || msg.delivery === "sending") return "Sending"
  var s = String(msg.delivery || msg.status || "").toLowerCase()
  if (s === "read") return "Read"
  if (s === "delivered") return "Delivered"
  if (s === "sent") return "Sent"
  return "Sent"
}
