# WhatsApp profile pictures

PR #9 adds background photo lookups for ordinary direct user/LID conversations
and standard groups. Canonical JIDs and phone/LID aliases identify contacts;
display names never identify an account. Unavailable pictures retain initials.

The worker serializes lookups outside messaging, coalesces queued work, and
handles picture changes/removals. Account generation, photo epoch, and cancelled
contexts reject retired completions. Retirement clears per-account photo state.
Content-derived filenames allow QML to reload a changed image.

Downloads and redirects accept HTTPS WhatsApp domains only, with a 20-second
timeout, 4 MiB body cap, and decoded JPEG/PNG/GIF limit of 2048 by 2048 pixels.
Transport errors exclude the request URL, including any provider-signed query
parameters. Provider-issued download URLs are used only for retrieval; they are
not stored as conversation avatar paths or logged. Files use private atomic
writes and the separate 256 MiB media cache. Unpair removes local photos.

Synthetic Go tests cover limits, redirects, canonical identities, changes,
removal, and retired completions. The Avatar fixture checks fallback, local image
decode, mask configuration, and changed-path reload. Offscreen software rendering
does not prove the GPU mask's appearance. Live direct/group photo availability,
refresh, notification icons, and native panel/pop-out rendering need acceptance
before release. No personal account or device was used for these checks.
