# OmaChat v0.6.3

This release fixes Google Messages conversations that no longer loaded, adds
Telegram sign-in for accounts with two-step verification, and fixes reopening
the pop-out after the compositor closes it. It includes the Messenger limits
from v0.6.2 and v0.6.1 and all v0.6.0 features.

## Fixed

- **Google Messages conversations load again.** Google Messages started
  sending some conversations and messages with an empty top-level ID and the
  real ID only in a nested field. OmaChat then showed a single conversation
  with an empty ID, and threads could not open
  ([#14](https://github.com/onelegdave/omachat/issues/14)). The vendored libgm
  now fills an empty ID from the nested field and never changes an ID that is
  present. This is a backport of upstream mautrix-gmessages
  [b580a1a](https://github.com/mautrix/gmessages/commit/b580a1a14f5893d7afcf3538f065b783a0d51a29),
  released in v0.2611.0.
- **The pop-out reopens after a compositor-side close.** With **Always open
  popped out** on, closing the pop-out with Hyprland's close-window bind left
  it unable to open again from the bar icon or `omarchy-shell shell toggle`
  ([#10](https://github.com/onelegdave/omachat/issues/10),
  [#11](https://github.com/onelegdave/omachat/pull/11)).

## Added

- **Telegram two-step verification.** Accounts with a Telegram cloud password
  previously stopped with "cloud password authentication is not yet supported"
  ([#13](https://github.com/onelegdave/omachat/issues/13)). After the QR code
  is accepted, OmaChat now asks for the cloud password in a masked field and
  shows the account's password hint. A wrong password keeps the prompt open,
  **Cancel sign-in** ends it, and an unanswered prompt closes after 10 minutes.
  The password is used only for Telegram's SRP check and is never stored,
  logged, or shown in status messages. See the
  [Telegram guide](../services/telegram.md#two-step-verification).

## Maintenance

- The vendored OpenTelemetry modules move from v1.44.0 to v1.47.0, which fixes
  [GHSA-q7gw-24fr-5p69](https://github.com/advisories/GHSA-q7gw-24fr-5p69) in
  the `otel/trace` auto-instrumentation tracer. The helper configures no
  tracing, so this path is not reachable in normal use.
- The native QML test runner reaches Hyprland's IPC socket, and the pagination
  fixture waits for deferred viewport work, so `make test-ui` passes on a real
  desktop ([#12](https://github.com/onelegdave/omachat/pull/12)).

## Update

```sh
omarchy plugin update onelegdave.omachat
```

Then choose **Build helper** in OmaChat and follow any shell-restart prompt.
Rebuilding is required for the Google Messages and Telegram changes to take
effect. Go 1.27.2 or newer, Python 3, and a C compiler remain required.
Dependencies are vendored, and no helper executable is shipped. There is no
account migration. Google Messages users who saw a single empty conversation
should select **Refresh** after rebuilding; re-pairing is not needed.

## Credits

Thank you to [@mollymillions2035](https://github.com/mollymillions2035) for the
pop-out report, its fix, and the native fixture fixes, included with original
commit authorship. Thank you to [@BZachHuff](https://github.com/BZachHuff) for
the detailed Google Messages report and to
[@jotapesse](https://github.com/jotapesse) for the Telegram two-step
verification report. The Google Messages fix is a backport of upstream
mautrix-gmessages work; upstream authorship and license notices are preserved.
Claude implemented the backport, Telegram sign-in, and dependency update
under OneLegDave's direction.

## Verification and limits

Verified November 21, 2026 on Omarchy 4.5.0 with Hyprland 0.56.2,
Quickshell 0.3.2, Qt 6.12.0, and installed Go 1.27.3:

- Full Go suite and the vendored libgm and Messenger suites under the race
  detector (568 tests passed, two intended Signal skips), and `go vet ./...`.
- The four nested-ID tests fail without the backport.
- 77 Node tests, 63 Python tests, 279 headless QML checks, and all 26 native
  QML fixtures (512 checks), including the new `telegram-password` fixture.
- Offline helper build and `omarchy plugin validate .`.

These use isolated synthetic data and fakes. No live Google Messages or
Telegram account was used, so the nested-ID fix and the cloud password
sign-in have not yet been confirmed against the live services. Publication
requires fresh checks and approval for the final commit in the existing
Marketplace update request #10861.
