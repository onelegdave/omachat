# Messenger service guide

Enable Messenger in **Settings > Services** before pairing. Disabling it keeps
the local session but stops its client after the shared helper restarts.

[Overview](../../README.md) · [Dependencies](../dependencies.md)

OmaChat is a Native Omarchy Plugin. Messenger runs in the shell-owned
`omachatd` helper through the `mautrix-meta` Messenger client. The helper is a
child process of `omarchy-shell`; OmaChat does not install a systemd unit.

## Pairing

1. Sign in to [facebook.com](https://www.facebook.com/) or
   [messenger.com](https://www.messenger.com/) in Chrome, Chromium, or Brave.
2. Unlock the desktop login keyring.
3. In OmaChat, select **Messenger > Pair from browser**.

Pairing is explicit. OmaChat copies the required Facebook session cookies from
the selected browser profile into its private Messenger session file. It does
not store your Facebook password. Cookie values are never sent through QML or
written to logs.

If multiple browser profiles exist, choose the correct profile on the pairing
screen before retrying. Facebook may request a security check in the browser or
invalidate a session after a provider-side change. Complete any account check
on the official site and pair again.

## Supported behavior

- Encrypted personal and group conversations.
- Recent conversation and message synchronization.
- Recent history fetched on demand when a conversation opens.
- Live incoming messages and conversation updates.
- Outbound text messages.
- Mark-as-read support.

Calling, reactions, typing indicators, media sending, and GIF search are not
available in the initial Messenger integration. Unsupported actions fail with a
service-specific message instead of being routed to another account.

## Protocol and account warning

Meta's official Messenger Platform API is for Facebook Pages and business
messaging, not a person's Messenger inbox. OmaChat therefore uses the same
unofficial client protocol implemented by `mautrix-meta`. Meta can change or
block that protocol without notice. This may interrupt sync, require re-pairing,
or place restrictions on an account. Use Messenger support only if you accept
that risk.

Automated tests use synthetic sessions and do not establish live account
compatibility. A live pairing or send must be initiated deliberately by the
user and should never be performed with credentials in logs, screenshots, or
bug reports.

## Storage and removal

| Data | Location |
| --- | --- |
| Session cookies | `~/.local/share/omachat/messenger_session.json` |
| Encrypted-device state | `~/.local/share/omachat/messenger.db` |
| Media and avatar cache | `~/.cache/omachat/media_messenger/` |
| Browser-profile choice | `~/.local/share/omachat/config.json` |

Private files use mode `0600`; private directories use `0700`. The files are
not encrypted by OmaChat and remain accessible to the desktop user and root.

Select **Unpair this desktop** on the Messenger tab to remove Messenger's local
session, cached conversations, and media without touching Google Messages,
WhatsApp, or Telegram. Also review Facebook's account security and active
sessions page if you want to revoke any server-side session that remains.

## Development checks

Build and test offline from the vendored dependencies:

```bash
make helper
go test -mod=vendor ./...
go test -race -mod=vendor -count=1 ./...
make test-ui
omarchy plugin validate .
```

Messenger unit tests use protocol fixtures or mock clients and do not contact
Meta. Live compatibility remains a separate, user-initiated verification step.
