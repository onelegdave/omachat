# Messenger HTTP response limits

Marketplace review of v0.6.0 [found an unbounded response
read](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6314053857)
in the vendored Messenger inbox client. The 60-second request timeout and
later preview/frame limits did not bound the bytes buffered by `io.ReadAll`.
A compressed response could expand beyond the size reported by the server.

The local mautrix-meta patch now uses a shared reader with a **32 MiB decoded
response limit**. It reads at most one extra byte to distinguish an exact-limit
response from an oversized one. It checks the stream, regardless of missing,
false, or compressed `Content-Length` metadata. The production transport
transparently expands gzip before this reader, on HTTP/1.1 and HTTP/2.

The inbox HTML, fetched JavaScript, GraphQL, and upload API replies all use
this reader through `MakeRequest`. Encrypted-device registration and lookup
use it as well; that separate path previously contained another unbounded
read. Both callers close the response on success and failure. Failed reads
return no partial payload for parsing or response-body logging. Oversized
responses have a permanent error so the HTTP and upload retry loops do not
download them again, including responses with status 429 or 500.

The ceiling allows larger bootstrap HTML/JavaScript than the panel's small
message frames, while bounding each response buffer. It is not a 32 MiB cap
on total helper memory: allocation growth, parsing, other state, and concurrent
requests also consume memory. Attachment downloads retain their separate
[file limits](media-download-safety.md). WebSocket message handling is outside
this HTTP-response correction. v0.6.2 adds a separate
[WebSocket message limit](messenger-websocket-safety.md).

`ponytail:` 32 MiB is a fixed ceiling, not a user setting. Revisit it only with
a reproducible legitimate provider response that exceeds it and a measured
memory budget; do not remove the bound to restore compatibility. Live account
compatibility at this ceiling has not been established by the offline tests.

## Offline regression checks

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=vendor \
  go.mau.fi/mautrix-meta/pkg/messagix \
  go.mau.fi/mautrix-meta/pkg/messagix/httpclient
```

`make test` explicitly includes both vendored packages, which `go test ./...`
does not discover. The fixtures cover normal and exact-limit bodies, absent
and understated length metadata, bounded reads of oversized streams, 429/500
responses without retries, partial read failures, closure, cancellation,
encrypted-device registration and failure-log suppression, and gzip expansion through the production
HTTP/1.1 and HTTP/2 transports. They use local servers and synthetic data.
The oversized cases fail against the original v0.6.0 readers; fixture streams
are finite so this negative check cannot exhaust the machine.

Preserve the shared reader, both call sites, permanent-error classification,
and tests when refreshing `vendor/`. This correction does not establish a
complete source audit, live-provider acceptance, or Marketplace approval.
