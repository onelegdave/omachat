# OmaChat v0.3.9

- Add in-app Telegram API credential configuration in Settings, replacing the manual
  terminal script as the primary setup method while preserving the script as an
  alternative option.
- Include numeric api_id and masked 32-character hexadecimal api_hash input fields,
  in-line validation, and direct links to my.telegram.org.
- Implement the `setTelegramCredentials` helper RPC to validate and atomically save
  credentials to private configuration. The API hash is never exposed in logs,
  responses, or status events.
- Safely update the running Telegram backend without requiring a shell or daemon
  restart, canceling any in-flight pairing and restoring existing sessions when
  credentials change.
- Provide a direct "Telegram settings" navigation shortcut in the Telegram pairing
  view when credentials are required.
- Update Telegram service documentation and roadmap status.

Verification: Go unit tests, race tests, QML integration tests, Go vet, and local
plugin validation passed. No live sending or pairing tests were performed with real
account credentials for this release. No dependencies are installed automatically.

Publishing is not marketplace submission or security certification. Marketplace
submission remains on hold pending the owner's testing and explicit approval.
