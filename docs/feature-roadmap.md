# Functionality follow-up

This list separates useful next work from features that already work. Installing
dependencies does not add missing protocol support.

## Current implementation

- In-app dependency detection, package-source review, and optional installation
  with terminal and package-manager confirmation.
- Readable pairing failures and retry when QR rendering fails.
- Existing text, photo, history, and supported voice features remain available
  according to the [service guides](README.md).

## Next priorities

1. **Service selection:** implement the [enable/disable design](service-selection-plan.md)
   in both the helper and UI, including stopping background activity and retaining
   credentials when a service is disabled. Hiding tabs alone is not sufficient.
2. **Telegram credential setup in the app:** replace the manual script step with
   a validated, masked form and helper RPC. Never expose the API hash in logs or
   status responses. Preserve other configuration and safely reconnect the client.
3. **More service capabilities:** assess WhatsApp/Telegram reactions and typing,
   then WhatsApp voice notes. The current helper explicitly rejects unsupported
   operations; add backend behavior and isolated tests before enabling controls.
4. **Device readiness:** distinguish missing tools from unavailable cameras,
   microphone permissions, file portals, and locked keyrings. Never open a camera
   or record audio just because Settings was opened.

Verify service-specific errors, reconnect behavior, network isolation, and
cancellation using synthetic fixtures before any live send/pairing tests. Live
tests require a deliberate user action and must not expose real account data.
