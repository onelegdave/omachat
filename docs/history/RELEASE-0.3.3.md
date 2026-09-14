# OmaChat v0.3.3

- Add a read-only dependency checklist in Settings, with source-review links,
  missing/available states, optional Install buttons, and Recheck.
- Require explicit terminal consent and preserve the package manager's own
  transaction confirmation. No automatic installation or system upgrade.
- Show useful QR-generation errors and offer retry.
- Align setup documentation with optional, user-initiated installation.
- Record follow-up work for service selection and more protocol capabilities.

Installation tests use mocks, including cancellation and failure cases. No real
package installation is performed by the test suite. Account setup, device access,
and unsupported protocol features remain separate from dependency availability.
