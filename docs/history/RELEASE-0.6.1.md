# OmaChat v0.6.1

This patch release bounds Messenger HTTP response reads, including compressed
responses. It includes all features and corrections from
[v0.6.0](https://github.com/onelegdave/omachat/releases/tag/v0.6.0).

## Fixed

Messenger inbox loading previously buffered an entire server response before
the preview and panel limits applied. A compromised Meta/Messenger service
could exhaust helper memory with a large or highly compressed response; the
request timeout did not bound decoded bytes.

The helper now accepts at most **32 MiB per decoded HTTP response**, without
trusting `Content-Length`. This covers inbox HTML, fetched JavaScript,
GraphQL and upload replies, and the separate encrypted-device registration
and lookup path. Oversized responses are discarded, their bodies are closed,
and they are not retried even when the server returns 429 or 500. Partial
registration responses are not parsed or included in response-body trace logs.

See [implementation, tests, and limits](https://github.com/onelegdave/omachat/blob/v0.6.1/docs/messenger-response-safety.md).
This is a per-response limit, not a total helper memory budget. A legitimate
response larger than the ceiling will be refused; live-account compatibility
at the new ceiling has not been established by the offline fixtures.

## Update

```sh
omarchy plugin update onelegdave.omachat
```

Then choose **Build helper** in OmaChat. **Rebuilding is required:** the old
executable keeps its previous behavior until it is replaced and restarted.
Follow the panel's shell-restart prompt after the build if shown.
Go 1.27.2 or newer, Python 3, and a C compiler are still required; dependencies
are vendored and no helper binary is shipped.

This patch introduces no session migration or re-pairing step. Keep your
existing settings and sessions; no account reset is required by the change.

## Credits

Thank you to HANCORE-linux for the
[Messenger response finding](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6314053857).
Codex implemented the correction and regression tests under OneLegDave's
direction. Claude independently reviewed the response paths and tests and
found no blocking issues; the suggested test and documentation improvements
were incorporated. Upstream mautrix-meta authorship and license notices remain
intact.

## Marketplace status

The existing [update-verification request #10861](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861)
records the exact-commit checks and Marketplace maintainer decision. Publishing
this GitHub release does not itself promote the catalog snapshot or complete
the maintainer's source review.

## Verification and limits

Checked October 22, 2026 (PDT) on Arch Linux with Go 1.27.2, Qt 6.12.0,
Quickshell 0.3.2, and Omarchy 4.0.5. The full Go tests and race suite,
explicit vendored Messenger and Google Messages tests, `go vet`, native
manifest validation, and the stamped offline helper build pass. The helper
reports 0.6.1 and the current source fingerprint. The 63 Python tests,
77 Node tests, and all 25 native Wayland fixtures pass, including upgrade,
failed-build preservation, rebuild, and delayed helper startup.

The new response regressions fail against the original v0.6.0 readers and
pass with the correction. They cover exact-limit and oversized streams,
missing or misleading length metadata, gzip through HTTP/1.1 and HTTP/2,
non-retryable overflow, read failures, cancellation, body closure, and
registration response-log suppression.

These checks use isolated settings, synthetic data, local fixture servers,
and a separate D-Bus session. No live provider account, real Signal client,
real invitation, or installed personal profile was used. WebSocket handling
is outside this HTTP-response correction. The new ceiling and a complete
Marketplace source review still need provider/maintainer acceptance.
