# OmaChat v0.3.11

- Keep Telegram credential saves responsive while the backend reconnects in tracked,
  cancelable background work.
- Cancel blocked Telegram credential refreshes promptly during helper shutdown.
- Safely supersede overlapping credential updates and reject new updates after shutdown
  begins.
- Remove unreachable service-specific Telegram credential dispatcher branches.
- Add deterministic race-tested coverage for shutdown, overlapping updates, and global
  configuration responsiveness.
- Make helper-build and message visibility bindings explicitly boolean to avoid QML
  undefined-value warnings.

Verification: Go unit tests, repeated concurrency regression tests, full Go race tests,
JavaScript and Python tests, QML integration tests, Go vet, and local plugin validation
passed. No dependencies are installed automatically.

Publishing is not marketplace submission or security certification. Marketplace
submission remains on hold pending the owner's testing and explicit approval.
