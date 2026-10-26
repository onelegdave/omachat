# Messenger WebSocket message limits

Marketplace review of v0.6.1 [found that incoming Messenger DGW WebSocket
messages had no byte limit](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6403016904).
The earlier [HTTP response correction](messenger-response-safety.md) was
confirmed, but it did not cover this separate transport. A compromised Meta
DGW service could send an oversized message and exhaust helper memory before
protocol parsing. This requires control of that service; it is not an
arbitrary-contact attack or a TLS bypass.

The vendored DGW socket now sets a **32 MiB incoming decoded message limit**
immediately after dialing, before publishing the connection or reading any
message. The existing `coder/websocket` reader applies the limit to the
complete message across continuation frames, after decompression when
compression is negotiated. It reads one extra byte to detect overflow.
Production currently leaves WebSocket compression disabled; both supported
compression modes are nevertheless covered by the regression fixtures.

An oversized message produces `ErrMessageTooBig` and a WebSocket close with
status 1009. The DGW read loop handles the error before parsing or logging
the partial message, closes the connection, stops its reader and ping/pong
loops, closes pending streams, and clears the connected state. The existing
client reconnect loop retains its exponential backoff, capped at five
minutes. Every new connection receives the same limit. No account reset or
re-pairing is introduced.

DGW messages may bundle multiple frames, whose individual payload lengths
use a 24-bit field. The 32 MiB ceiling allows large bundled frames while
bounding each complete receive buffer. It is not a total helper memory
budget: allocation growth, queued messages, parsing, and other services also
consume memory. This patch does not change the HTTP, attachment, or panel
limits and does not claim a complete source audit.

The ceiling is fixed. Revisit it only with a reproducible
legitimate message exceeding it and a measured memory budget. Do not remove
the limit to restore compatibility. Live Messenger compatibility at the new
ceiling remains unverified.

## Offline regression checks

```sh
GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -race -mod=vendor -count=1 \
  go.mau.fi/mautrix-meta/pkg/messagix/dgw
```

`make test` explicitly includes this vendored package because `go test ./...`
does not discover it. Fixtures call the production socket's `Connect` method
against a local WebSocket server. They cover small and exact-limit messages,
one-byte overflow, continuation frames, incomplete fragmented messages,
both negotiated compression modes, per-message allowance reset, peer close
status, discarded partial frames, and socket/stream cleanup. Every input is
finite, including the negative regression run with the old unlimited reader.

Keep the socket limit and these fixtures when refreshing `vendor/`. Tests
use synthetic frames and no provider accounts. They do not establish live
provider acceptance or Marketplace approval.
