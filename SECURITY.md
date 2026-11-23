# Security

This Native Omarchy Plugin talks to Google Messages, WhatsApp, Telegram,
Messenger, and, when explicitly enabled, the external `signal-cli` process. It
reads browser cookies while checking pairing profiles, pairing, changing the
selected profile, starting a Google account session, recovering from
authentication failures, and after the browser renews its Google sign-in
cookie.

To notice that renewal, a Google account pairing checks the modification time of
the cookie file of the browser profile it was paired from every minute (of every
profile, until it knows which one that is). When a file changed, `sqlite3` reads
the Google sign-in cookie metadata read-only and in place, without decrypting
it, and only a changed `__Secure-1PSIDTS` leads to a full read. For a Google
account pairing, every full read except when pairing or after you choose a
profile decrypts the profile the session came from first, then other profiles
until one has the same Google sign-in, and takes cookies only from that one. A
profile with no Google sign-in cookie at all is recognised from its cookie
database before the keyring is asked for anything, and is not decrypted.

Only while the opt-in sign-in diagnostics are on, it also logs Google sign-in
cookie metadata in the selected browser profile without decrypting it. WhatsApp
and Telegram use their QR flows. Message content and credentials are cached
locally.

## Report a vulnerability

Use [GitHub's private vulnerability report form](https://github.com/onelegdave/omachat/security/advisories/new)
for credential or protocol bugs. Do not include credentials, personal messages,
or working exploit details in a public issue.

## What is stored

- Pairing credentials: `~/.local/share/omachat/session.json` (0600)
- WhatsApp device store: `~/.local/share/omachat/whatsapp.db` (0600 from creation)
- WhatsApp chat cache: `~/.local/share/omachat/whatsapp_store.json` (0600)
- WhatsApp paged history cache: `~/.local/share/omachat/whatsapp_history.sqlite` (0600)
- Telegram session and chat cache: `~/.local/share/omachat/telegram.session` and `telegram_store.json` (0600)
- Messenger session, encrypted-device state, and chat cache: `~/.local/share/omachat/messenger.db` and `messenger_store.json` (0600)
- Signal linked-device keys and local message cache: `~/.local/share/omachat/signal-cli/` and `signal_store.json`; OmaChat creates the enclosing directories as 0700 and its message cache as 0600, while file modes within the `signal-cli`-managed directory are controlled by `signal-cli`
- Config (browser profile, Telegram API credentials, notification and pop-out preferences, sidebar layout, last service and per-service conversation IDs, sign-in diagnostics choice): `~/.local/share/omachat/config.json` (0600)
- Google Messages sign-in diagnostics, only if switched on in Settings > Debug: `~/.local/state/omachat/auth.log` and one older `auth.log.1` (0600, folder 0700, about 2 MB in total). See [Sign-in diagnostics](#sign-in-diagnostics).
- Attachment cache: `~/.cache/omachat/media/`, `media_whatsapp/`, `media_telegram/`, `media_messenger/`, and `media_signal/`
- Telegram conversation caches from before typed peer IDs are ignored on upgrade;
  pairing credentials are retained and ambiguous attachment filenames are not reused.
- Control socket: `$XDG_RUNTIME_DIR/omachat/daemon.sock` (0600)

OmaChat's private directories are restricted to 0700. The socket is restricted
to 0600. These permissions do not isolate OmaChat from other programs running
as the same user, or from root. Stored credentials and caches are not encrypted
by OmaChat. Trusted XDG parent directories and a trusted desktop account are
part of the security boundary.

Update preferences and cached public release metadata live in
`~/.local/state/omachat/updates.json` (or under `XDG_STATE_HOME`). They are
separate from messaging credentials. Daily checks are off by default.

Desktop alerts use the local `org.freedesktop.Notifications` D-Bus service.
Notifications are enabled by default. Alert text is always generic: "OmaChat ·
*service*" and "New message". Conversation names and message text are never sent
to the notification service, because a notification service may pass alert text
to its own subprocesses; Omarchy's history writer puts it in a `bash` command
line that other local accounts can read through `/proc`. The optional preview
setting was removed in 0.5.4 for this reason; see the
[README](README.md#notification-previews-removed-in-054).
Alerts can show the conversation's cached profile photo. Icons use local image
files from that service's media cache, named by hash; unavailable photos fall
back to a generic icon. Desktop notification actions are accepted only from the
service's current D-Bus owner and route through locally held
service/conversation identifiers.

OmaChat keeps message content and pairing links out of the command lines of the
programs it starts. The pairing link reaches `qrencode` on standard input.

## What is enforced

- Incoming attachments and avatars are read or written through a bounded
  stream. Telegram downloads stop at 32 MiB while transferring. Each service
  evicts old downloaded attachments above its 256 MiB cache budget. A declared
  `Content-Length` is never trusted as the allocation size.
- Telegram profile photos use authenticated MTProto downloads, stop at 5 MiB,
  and share Telegram's 256 MiB media cache. Unpairing clears these photos too.
- Group avatar URLs must be `https` and must resolve to a public address.
  Loopback, private, link-local, and carrier-grade NAT ranges are refused.
- OmaChat's direct child-process launches use argument arrays, not interpolated
  shell commands. External tools and desktop launchers have their own behavior.
- Cookie values are not logged. Errors name which cookie is missing.
- View-once and ephemeral WhatsApp media are omitted from disk persistence and cannot be reopened or redownloaded.
- Telegram self-destructing media is omitted from disk persistence and cannot be reopened or redownloaded.
- Google Messages, WhatsApp, Telegram, Messenger, and Signal maintain separate credential stores, databases, sessions, and cache directories. Unpairing one network never deletes or exposes files belonging to another.

## Accepted

- Community plugins and the helper run as your user without a security sandbox.
  Service separation prevents accidental cross-service state reuse, not access
  by a compromised helper or another process under the same account.
- Enabled services contact their messaging providers. No claim of offline-only
  operation or anonymity is made.
- OmaChat 0.4.4 removed GIPHY search. GIPHY accepts its API key only as a URL
  query parameter, and the marketplace review does not accept credentials in
  request URLs. On first start, the helper deletes a `giphyApiKey` left in
  `config.json` by an earlier release; it stores no third-party search key.
- Manual update checks and optional daily checks request public release metadata
  from GitHub. They send no messaging credentials or message content; GitHub
  receives ordinary request metadata, including the client IP address. Updates
  require an explicit action and the native updater's terminal confirmation.
  The updater follows the repository default branch, not a pinned release tag.
- Source review and regression tests are limited checks, not a guarantee that
  this project or its dependencies contain no vulnerabilities. Keep the desktop
  toolchain and external tools current through your normal update workflow.

- `libgm` is a reverse-engineered client. Google can break or detect it.
- `mautrix-meta` uses Meta's unofficial Messenger client protocol. Meta can
  break or detect it, invalidate sessions, or restrict an account. Messenger
  pairing imports the minimum required browser cookies into a separate private
  store; cookie values are never exposed to QML or logs.
- Signal support launches the separately installed, unofficial `signal-cli`
  client as an OmaChat child process. It decrypts messages and stores linked-
  device keys locally. Keep it current, revoke OmaChat from Signal's phone-side
  Linked devices screen before deleting local keys, and do not treat the local
  text cache as an encrypted archive. signal-cli saves received attachments in
  its own data folder; OmaChat deletes that copy along with its own and the
  helper's previews when a message is deleted, expires, is view-once, or is
  trimmed from the local store. Downloads are capped at 32 MiB, and stories are
  not fetched.
- Links in messages open only after a confirm dialog, and only `http`/`https`
  URLs. Images open locally with `xdg-open` after they are already in the cache.
- Pairing copies the browser cookie database to a 0600 tempfile so it can
  be read while the browser holds a lock, then deletes it.

## Sign-in diagnostics

Settings > Debug > Sign-in diagnostics is off by default and exists to find out
why Google Messages asks to pair again. While it is off, nothing is logged, the
browser is not checked, and no diagnostics folder is created. Turning it off
stops writing immediately; **Delete log** removes both log files.

While it is on, the helper writes `~/.local/state/omachat/auth.log` itself. The
Google Messages library's own logging never reaches it. It records:

- Google sign-in failures: where they happened, the HTTP status and error text,
  and, for 401 and 403 responses only, Google's error response as received
  (up to 64 KiB, deduplicated and rate limited).
- Browser cookie refreshes that OmaChat already performs, the browser profile
  name, and whether they worked.
- The names of sign-in cookies that changed in OmaChat's session or in the
  browser, with salted 8-character fingerprints. The salt is random per log, so
  fingerprints cannot be compared across logs and values cannot be recovered.
- Connection, pairing, and phone status changes with their status messages.

Cookie and token values are never written; text fields are also scrubbed for
the live credentials before they are written. Messages, contacts, and phone
numbers are not recorded. Google's error responses are kept as received and
can include an IP address or, occasionally, an account name, so review the
log before sharing it.

While diagnostics are on and Google Messages is paired, the helper checks the
selected browser profile's cookie file at most every three minutes. When it
changed, `sqlite3` reads the live file read-only (`mode=ro&immutable=1`, no
copy) and returns, for the listed Google sign-in cookies on `.google.com` and
`messages.google.com` only, their dates, sizes, and a SHA3 hash of the
still-encrypted value. It never runs `secret-tool`, never decrypts, and never
uses what it reads. This logging check is separate from the renewal check
described at the top, which does act on what it reads. The log is restricted to your account; root and programs
running as you can still read it.

Revoke the paired device on your phone when you stop using this machine.

## Vendored upstream API-key findings

GitHub Secret Scanning reports two Google API-key patterns in vendored protocol
code: the Google Messages relay constant from `mautrix-gmessages` and a
Google-looking string in the `whatsmeow` token dictionary. These are inherited
upstream client/protocol constants, not OmaChat user credentials. Do not remove
or obfuscate them locally: the relay constant is required for Google Messages
requests, and changing token-dictionary entries can alter WhatsApp wire indexes.
Track upstream fixes and update the vendored dependencies when they are
available. User credentials remain local under `~/.local/share/omachat/` and are
never committed.

Upstream tracking: [mautrix/gmessages#94](https://github.com/mautrix/gmessages/issues/94)
and [whatsmeow#1266](https://github.com/tulir/whatsmeow/issues/1266). The exact
locations, upstream versions, and how each string is used are recorded in
[vendored API-key alerts](docs/vendored-key-alerts.md).
