# Marketplace readiness

Submission is intentionally on hold until the owner completes the
[oldsmaru acceptance check](oldsmaru-checklist.md). Publishing a GitHub release
does not submit, list, approve, or verify the plugin in the marketplace.

## Repository requirements

The [current submission guide](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/SUBMISSION.md)
requires a public repository, a root manifest for one plugin, installation and
removal documentation, license and dependency information, and a unique plugin
ID. OmaChat uses `onelegdave.omachat`. The root `preview.png` and
[theme gallery](screenshots/README.md) use fictional data in actual QML components.

Suggested listing category: **Productivity**. Suggested tags: **bar**, **quickshell**,
**media**. These are suggestions, not an existing submission or approval.

## Capabilities that deserve explicit review

- The helper is built locally from this repository's vendored Go source. There
  is no downloaded executable, automatic toolchain installation, installer hook,
  or systemd unit.
- Settings can open a terminal for an allowlisted package installation only
  after an explicit user action. The terminal asks for consent before `sudo`,
  and pacman retains its own transaction prompt. This is a privilege and package
  management capability, even though it is optional.
- Google pairing reads browser cookies and may refresh them while paired.
  All services retain credentials and chat/media caches locally. These are
  sensitive capabilities, not sandboxed or isolated from other same-user code.
- Vendored generated Telegram files contain setup-related names, and an upstream
  development Makefile contains tool-install commands. These can trigger static
  capability review even though OmaChat does not execute those development targets.
- Disabling services retains credentials; unpairing and deleting stored data are
  separate, documented actions.

## Verification evidence and limits

On September 14, 2026, the marketplace's own compatibility inspector accepted
v0.3.4 (`55e099e12480ff94456ce50f6b929d732947c052`). Its V3 static baseline
returned `review-required`, with no findings and the `installer`, `package-manager`,
and `privilege` capabilities. The marketplace tooling checkout used was
`73498d58716438c466617846cf040ff289463392`. These results describe that exact
snapshot, not later changes. Rerun against the intended release commit before
any submission; future policy or code changes can alter the result.

An OSV query of the 53 vendored module versions on the same date returned
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932), which concerns the unmaintained
`golang.org/x/crypto/openpgp` packages. Those packages are absent from the vendor
tree and from `go list -mod=vendor -deps ./cmd/omachatd`. This module-level query
does not cover every potential vulnerability, the Go standard library, native
libraries, or the user's installed tools. `govulncheck` was not installed or run.

The [marketplace security policy](https://github.com/omacom/omarchy-plugin-marketplace/blob/main/SECURITY.md)
requires exact-commit checks and explicit maintainer approval. Capability review
is expected. Local tests, static checks, and this focused source review do not
guarantee security, approval, or error-free operation. Do not suppress genuine
capabilities or describe `review-required` as a clean security certification.

Before submitting, rerun the repository test suite and exact-commit marketplace
checks, inspect the public screenshots and release metadata, complete the
second-machine tests, and obtain the owner's explicit submission approval.
