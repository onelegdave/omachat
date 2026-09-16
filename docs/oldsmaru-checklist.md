# Second-machine acceptance record

This record separates checks actually completed on oldsmaru from broader live
feature checks completed on the primary development machine. It is not a claim
that every service action was repeated on both machines.

## Completed on oldsmaru

- Installed the public v0.4.2 beta and built its helper entirely from vendored
  source. Fresh Google browser-profile pairing completed from `gaiaPairing`
  through `connecting` to `connected`, with `phoneOK: true`.
- Updated the clean plugin checkout from the beta repository to public stable
  candidate `91e77a2b3fac9a8bb6fb415639bab1af296cf5d3` on September 16, 2026.
- Built helper version 0.4.3 with source ID
  `433a0b8dac709c6ef62e521fc34743efeda3eb632a0a3512ad2780fecd114ca1`
  and restarted the Omarchy shell on Omarchy 4.0.4-1.
- Verified the shell-owned helper was running, the owner-only socket mode was
  0600, and sanitized status RPCs reported Google Messages, WhatsApp, and
  Messenger connected with `phoneOK: true`. Telegram remained intentionally
  unpaired on this machine.
- The synthetic UI gate separately verified the fresh service chooser,
  dependency explanations and cancellation behavior, dark and light palettes,
  larger text, keyboard operation, pop-out behavior, helper reconnection, and
  upgrade/rebuild flow without accessing personal accounts.

## Live feature evidence from the primary machine

OneLegDave visibly confirmed cross-service unread badges; Google pairing;
WhatsApp GIF search/playback, reactions, contact names, and iPhone-compatible
outbound voice; Telegram reactions; Messenger incoming updates, inline GIFs,
attachments, reactions, and voice; and the animated helper-connection screen.
Calling remains deliberately unsupported.

Public reports and screenshots must continue to exclude credentials, phone
numbers, account identifiers, contacts, and message contents. Marketplace
automation and maintainer review remain separate from this acceptance record
and are not a security certification or approval claim.
