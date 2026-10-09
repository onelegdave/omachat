.pragma library

function messageKey(network, message) {
  return JSON.stringify([network, String(message.conversationID || ""), String(message.id || "")])
}

function isIncoming(message, since, now) {
  if (!message || !message.id || !message.conversationID || message.fromMe || message.deleted
      || message.provisional || message.pending || message.failed) return false
  if (String(message.text || "").trim() === "" && !(message.attachments && message.attachments.length)) return false
  var timestamp = Number(message.timestamp)
  return isFinite(timestamp) && timestamp >= since && timestamp <= now + 60000000
}
