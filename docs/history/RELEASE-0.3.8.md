# OmaChat v0.3.8

- Stop Telegram setup without replacing unreadable, malformed, or non-object
  configuration. Validate credentials before saving and report failures clearly.
- Coordinate the setup script and helper with a shared file lock. Reload saved
  settings and update only the requested fields, preserving credentials,
  preferences, unknown fields, and service opt-outs.
- Preserve the fresh-install service chooser and avoid restoring stale values
  removed by another writer. Keep atomic, private configuration writes.
- Add regression coverage for Python-to-Go updates, lock contention, concurrent
  writers, invalid files, permission errors, and failed-write memory state.
- Update the Telegram guide with recovery and lock-contention instructions.

Verification: Go race tests, vendored pairing tests, Go vet, Python and JavaScript
tests, the real-QML suite, and local plugin validation passed using synthetic test
data. No live sending or pairing tests were performed for these configuration
changes. No dependencies are installed automatically.

Publishing is not marketplace submission or security certification. Marketplace
submission remains on hold pending the owner's testing and explicit approval.
