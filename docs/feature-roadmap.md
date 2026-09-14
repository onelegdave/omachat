# Functionality follow-up

This list separates useful next work from features that already work. Installing
dependencies does not add missing protocol support.

## Current implementation

- In-app dependency detection, package-source review, and optional installation
  with terminal and package-manager confirmation.
- Readable pairing failures and retry when QR rendering fails.
- [Service opt-outs](service-selection-plan.md), with retained credentials and
  a shared-helper restart to stop disabled clients completely.
- Existing text, photo, history, and supported voice features remain available
  according to the [service guides](README.md).

## Next priorities

1. **Telegram credential setup in the app:** replace the manual script step with
   a validated, masked form and helper RPC. Never expose the API hash in logs or
   status responses. Preserve other configuration and safely reconnect the client.
2. **More service capabilities:** assess WhatsApp/Telegram reactions and typing,
   then WhatsApp voice notes. The current helper explicitly rejects unsupported
   operations; add backend behavior and isolated tests before enabling controls.
3. **Device readiness:** distinguish missing tools from unavailable cameras,
   microphone permissions, file portals, and locked keyrings. Never open a camera
   or record audio just because Settings was opened.

Verify service-specific errors, reconnect behavior, network isolation, and
cancellation using synthetic fixtures before any live send/pairing tests. Live
tests require a deliberate user action and must not expose real account data.
