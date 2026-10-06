.pragma library

// Pure and offline. No Qt/Node APIs; callers own the original records.
function normalize(value) {
    var text = String(value || "").toLowerCase().replace(/^[\s:]+|[\s:]+$/g, "")
    if (/^[+-]1$/.test(text)) return text
    return text.replace(/[\uFE0E\uFE0F]/g, "").replace(/[_\-:\s]+/g, " ").trim()
}

function buildIndex(rows) {
    var index = rows.map(function(row) {
        var exact = [normalize(row.e), normalize(row.label)]
        var fields = exact.slice()
        ;(row.aliases || []).forEach(function(alias) {
            exact.push(normalize(alias))
            fields.push(normalize(alias))
        })
        ;(row.keywords || []).forEach(function(word) { fields.push(normalize(word)) })
        if (row.k) fields.push(normalize(row.k))
        var tokens = []
        fields.forEach(function(field) {
            field.split(" ").forEach(function(token) {
                if (token && tokens.indexOf(token) === -1) tokens.push(token)
            })
        })
        return { row: row, exact: exact, fields: fields, tokens: tokens, haystack: fields.join("\n") }
    })
    return index
}

function literalScore(entry, text) {
    // Most rows do not match. One native string scan avoids two JS field loops.
    if (entry.haystack.indexOf(text) === -1) return -1
    if (entry.exact.indexOf(text) !== -1) return 0
    for (var i = 0; i < entry.fields.length; i++) {
        if (entry.fields[i].indexOf(text) === 0) return 1
    }
    if (entry.tokens.indexOf(text) !== -1) return 2
    for (var j = 0; j < entry.fields.length; j++) {
        if (entry.fields[j].indexOf(text) !== -1) return 3
    }
    return -1
}

// A bounded Damerau distance of one: insertion, deletion, substitution or
// adjacent transposition. Linear time and no per-candidate matrix allocation.
function oneEdit(a, b) {
    if (Math.abs(a.length - b.length) > 1) return false
    var i = 0
    while (i < a.length && i < b.length && a.charAt(i) === b.charAt(i)) i++
    if (i === a.length && i === b.length) return true
    if (a.length === b.length) {
        if (a.slice(i + 1) === b.slice(i + 1)) return true
        return a.charAt(i) === b.charAt(i + 1) && a.charAt(i + 1) === b.charAt(i)
            && a.slice(i + 2) === b.slice(i + 2)
    }
    return a.length > b.length ? a.slice(i + 1) === b.slice(i) : a.slice(i) === b.slice(i + 1)
}

function wordScore(entry, word, cache) {
    var literal = literalScore(entry, word)
    if (literal >= 0) return literal
    // Short words, glyphs and signed aliases never receive fuzzy expansion.
    if (word.length < 4 || word.length > 64 || !/^[a-z0-9]+$/.test(word)) return -1
    var i, token, key
    for (i = 0; i < entry.exact.length; i++) {
        token = entry.exact[i]
        key = word + ":" + token
        if (cache[key] === undefined) cache[key] = oneEdit(word, token)
        if (cache[key]) return 4
    }
    for (i = 0; i < entry.tokens.length; i++) {
        token = entry.tokens[i]
        key = word + ":" + token
        if (cache[key] === undefined) cache[key] = /^[a-z0-9]+$/.test(token) && oneEdit(word, token)
        if (cache[key]) return 5
    }
    return -1
}

function search(index, query) {
    var text = normalize(query)
    if (!text) return index.map(function(entry) { return entry.row })
    var words = text.split(" ")
    var matches = []
    var cache = Object.create(null)
    index.forEach(function(entry, order) {
        var score = literalScore(entry, text)
        if (score < 0) {
            score = words.length > 1 ? 2 : 0
            for (var i = 0; i < words.length; i++) {
                var part = wordScore(entry, words[i], cache)
                if (part < 0) { score = -1; break }
                score = Math.max(score, part)
            }
        }
        if (score >= 0) matches.push({ row: entry.row, score: score, order: order })
    })
    matches.sort(function(a, b) { return a.score - b.score || a.order - b.order })
    return matches.map(function(match) { return match.row })
}
