# OmaChat v0.6.2

This patch release bounds incoming Messenger WebSocket messages. It includes
the HTTP response limits from v0.6.1 and all v0.6.0 features.

## Fixed

Messenger's DGW WebSocket receiver previously disabled its message-size
limit. A compromised Meta DGW service could exhaust helper memory with an
oversized or fragmented message before protocol parsing. The receiver now
accepts at most **32 MiB per decoded message**, including continuation
frames and decompressed bytes if compression is negotiated. Oversized
messages are discarded before parsing, the connection closes safely with
status 1009, and the existing reconnect backoff remains in effect.

This is a per-message limit, not a total helper memory budget. Legitimate
messages over the ceiling will be refused. See
[implementation, tests, and limits](../messenger-websocket-safety.md).

## Update

```sh
omarchy plugin update onelegdave.omachat
```

Then choose **Build helper** in OmaChat and follow any shell-restart prompt.
Rebuilding is required for the correction to take effect. Go 1.27.2 or newer,
Python 3, and a C compiler remain required. Dependencies are vendored, and no
helper executable is shipped. This patch introduces no account migration or
re-pairing step.

## Credits

Thank you to HANCORE-linux for the
[WebSocket finding](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6403016904).
Codex implemented the correction and regression checks under OneLegDave's
direction. Upstream authorship and license notices are preserved.

## Verification and limits

Verified October 26, 2026 on Linux with installed Go 1.27.3:

- Full Go suite, vendored libgm and Messenger suites, and `go vet ./...`.
- All 18 new socket cases under the race detector. The uncompressed
  complete and fragmented overflow regressions fail with the old unlimited reader.
- 77 Node tests, 63 Python tests, and all 25 native QML fixture pass markers.
- Offline helper build reporting `0.6.2` and `omarchy plugin validate .`.

These use local servers and isolated synthetic data; no live Messenger
account, real Signal client, invitation, or personal-profile upgrade is used.
Live compatibility at the ceiling and complete Marketplace review remain
outstanding. Publication requires fresh checks and approval for the final
commit in the existing Marketplace update request #10861.
