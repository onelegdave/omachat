# Optional services: proposed design

Status: planning only. The current app offers all three service tabs.

## User experience

Add a Services section in Settings with independent Google Messages, WhatsApp,
and Telegram switches. Explain that disabling a service pauses its connection
and hides its tab; it does not unlink the account or delete credentials.
Keep **Unpair this desktop** as a separate, explicit account-removal action.

Existing installations retain all services enabled when upgrading. On a fresh
installation, show a service chooser before pairing. Allow no services selected:
show a calm empty state with **Choose services**, and keep Settings accessible.
No choice installs dependencies or creates an account. Show requirements beside
each choice and let the user decide whether to install missing tools themselves.

## Implementation

1. Add a persisted `enabledServices` list to the private daemon config. Distinguish
   a missing field (migration: all enabled) from an explicit empty list (none).
   Validate known service IDs and expose only this non-secret preference to QML.
2. Add a daemon settings RPC that starts/stops network clients without clearing
   their session files. Disabled services must not reconnect, sync, poll, download
   media, or accept sends. Cancel in-flight work and wait for it to finish before
   reporting the service disabled. Preserve the shell-owned shared helper.
3. Filter tabs, unread counts, refresh requests, and keyboard navigation from the
   enabled list. Move selection to an enabled service when the active one is
   disabled. Keep saved drafts isolated; explain before discarding unsent content.
4. Provide an explicit result if a send was already submitted when disable was
   requested. Never retry that send automatically on re-enable. Re-enable should
   reconnect with retained credentials, or offer pairing if credentials expired.
5. Add tests for migration, all-disabled startup, disable during sync/send,
   no background traffic while disabled, failed transitions, and service isolation.

## Decisions for the next implementation

Recommended: keep credentials and drafts when disabling, make the first-run
choices unselected, and keep the shared vendored helper build. Choosing fewer
services would not reduce build requirements in this phase. Optional per-service
builds are a separate packaging change, not part of a simple Settings switch.

No service-toggle controls are presented as functional until both the UI and
backend behavior above are implemented and verified.
