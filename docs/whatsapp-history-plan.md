# WhatsApp history storage and paging

PR #9 supplies paged SQLite history separate from the WhatsApp credential/device
database. The cache defaults to 128 MB; Settings allows 64, 128, 256, or 512 MB.
Old local history is evicted without deleting phone messages. Attachments and
profile photos use the separate media cache.

Conversation metadata remains indexed beyond the 50 hot message windows. Memory
and returned pages are bounded; timestamp plus message ID orders pages. SQLite
page ceilings, journal/compaction allowances, private index writes, and physical
artifact accounting enforce the disk budget. Large legacy caches are streamed
within bounds, and migration preserves the legacy payload until persistence and
the atomic replacement index have succeeded. Failures remain recoverable.

At the cached-history boundary a connected phone may supply an older page. One
pending request, timeout, cooldown, unavailable state, and PN/LID identity aliases
control the request lifecycle. Phone availability is not guaranteed. Deferred QML
paging and replies retire when the selected conversation/service changes.
View-once and ephemeral payloads remain excluded from reusable media storage.

Local unpair and phone-initiated logout detach and close the SQLite handle before
local cleanup, clear anchors and pending requests, and require explicit pairing.
The next pairing opens a fresh cache. A retired read cannot repopulate the new
account's history or request state. Cleanup failure keeps pairing blocked.

Synthetic tests cover persistence beyond hot memory, alias merging, byte/disk
limits, migration failure, timeouts, account retirement, and Settings receipts.
Actual phone-history completeness and installed-profile behavior remain pending
manual acceptance. Local verification does not use private account files or
send real messages.

The alternate local installer from PR #9 is excluded. Installation continues
through `omarchy plugin add`; build staging preserves the reviewed offline,
hidden sibling implementation and never runs an installer hook or automatic
package installation.
