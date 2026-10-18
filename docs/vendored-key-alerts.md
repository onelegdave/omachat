# Vendored Google API-key alerts

GitHub Secret Scanning reported two Google API-key patterns in the public
beta repository. Both are in vendored upstream code. This record classifies
them from the source; it does not reproduce either value. Checked October 17,
2026, against the vendor tree of this release.

| Location | Upstream module | What the string is |
| --- | --- | --- |
| `vendor/go.mau.fi/whatsmeow/binary/token/token.go:13` | `go.mau.fi/whatsmeow` `v0.0.0-20260909164725-b25a56d63729` | Entry 62 of `DoubleByteTokens[2]`, WhatsApp's binary XML token dictionary |
| `vendor/go.mau.fi/mautrix-gmessages/pkg/libgm/util/constants.go:3` | `go.mau.fi/mautrix-gmessages` `v0.2608.0` | `GoogleAPIKey`, the Google Messages for Web client key |

## Evidence

- Both vendored files are byte-identical to the upstream sources at the pinned
  versions (SHA-256 compared against the upstream repositories at commit
  `b25a56d63729` and tag `v0.2608.0`). OmaChat did not add or change either
  string.
- Each value occurs exactly once in the repository, in its vendored file. No
  OmaChat source, test, fixture, document, or configuration contains it.
- **whatsmeow:** the dictionary maps two-byte codes on the wire to strings and
  back (`token.GetDoubleToken`, `token.IndexOfDoubleByteToken`). A string's
  position is its code. The encoder substitutes this code only when WhatsApp's
  own protocol carries the identical string. OmaChat never sends it as a
  credential. Removing, reordering, or masking the entry would shift or break
  the codes WhatsApp's servers use and corrupt decoding.
- **mautrix-gmessages:** `util.BuildRelayHeaders` sends `GoogleAPIKey` as the
  `x-goog-api-key` header on Google Messages relay requests, alongside the
  `messages.google.com` origin and referrer, as the Google Messages for Web
  page does. It identifies that web client to Google's API, not a person or
  an account. A user is authorized by their own pairing session, kept locally
  under `~/.local/share/omachat/`. Removing it would stop Google Messages.

## Classification

Neither string is an OmaChat secret or a personal credential, and OmaChat
cannot rotate or revoke either. Both are public client or protocol constants
that every copy of the upstream libraries carries. They are kept unchanged and
unobfuscated. Upstream tracking:
[mautrix/gmessages#94](https://github.com/mautrix/gmessages/issues/94) and
[whatsmeow#1266](https://github.com/tulir/whatsmeow/issues/1266). If upstream
changes either constant, the change arrives with a normal vendored dependency
update.

How the alerts are closed in GitHub is the repository owner's decision. This
record is the evidence for it.
