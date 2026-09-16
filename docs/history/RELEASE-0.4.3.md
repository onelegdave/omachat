# OmaChat v0.4.3

This release promotes the completed multi-service beta to stable.

## Highlights

- Add native personal Messenger conversations, media, GIF search, voice notes,
  reactions, unread state, and encrypted-session persistence.
- Add WhatsApp GIF search and native GIF playback, reactions, improved contact
  names, voice recording, and an iPhone-compatible outbound audio fallback.
- Add Telegram reactions and live unread updates.
- Add per-service unread badges, aggregate bar notifications, and a clearer
  animated helper-connection screen.
- Restore Google browser-profile pairing attestation required to avoid pairing
  error 3/32.
- Refresh Settings, About, credits, dependency guidance, and fictional-data
  screenshots to match the shipped product.
- License v0.4.3 and later under AGPL-3.0-or-later so the combined helper follows
  the terms of its AGPL protocol dependencies. Earlier MIT grants remain valid.

## Update

```bash
omarchy plugin update onelegdave.omachat
```

Open OmaChat after updating and select **Rebuild helper** when prompted. The
helper is built locally from vendored source. No dependency is installed
automatically. This release does not intentionally require re-pairing, although
providers may independently expire linked-device sessions.

## Known limits

- Calling is not supported.
- Telegram has no dedicated GIF-search sender.
- Older Messenger encrypted history may be unavailable even while recent and
  live messages work.
- Marketplace review, if submitted, is separate from this release and is not a
  security certification or endorsement.
