# Signal setup and current scope

OmaChat's Signal integration is an unofficial linked-device client built on the
separately installed [signal-cli](https://github.com/AsamK/signal-cli). Signal,
signal-cli, and OmaChat are independent projects. Signal support is opt-in and
does not start a process until Signal is enabled in Settings > Services.

## Setup

1. Install a current `signal-cli` release by following its upstream README. On
   Arch/Omarchy, the community AUR package can be installed with
   `yay -S --needed signal-cli`; review its PKGBUILD and transaction before confirming.
2. Verify `signal-cli --version` succeeds in a terminal.
3. In OmaChat, open Settings > Services, enable Signal, and apply the change.
   If `signal-cli` is missing, OmaChat asks before opening a terminal installer.
   Canceling that prompt leaves Signal disabled and installs nothing.
4. Open the Signal tab and select **Link Signal**.
5. On the primary phone, open Signal > Settings > Linked devices > Link a new
   device and scan OmaChat's QR code.

OmaChat launches `signal-cli --data-dir ~/.local/share/omachat/signal-cli
--output=json jsonRpc --receive-mode=manual --ignore-stories` as a child of the
shared helper. It subscribes to messages after selecting the linked account.
Stories are not shown, so signal-cli does not fetch or save them. Disabling Signal stops
the child and hides the tab without deleting keys or local messages.

## Current features

Implemented: optional service lifecycle, QR provisioning, direct and group
creation and text send/receive, contact discovery, a locally accumulated conversation store,
incoming and outgoing attachments with captions, voice notes, and emoji
reactions including add, replace, remove, and phone-synced updates. Attachments
download lazily into `~/.cache/omachat/media_signal/` and are subject to the
shared 256 MiB cache budget. A received attachment over 32 MiB is not
downloaded into OmaChat; open it on the phone. Photos appear as previews the
helper re-encodes first, like every other service.

The local inbox keeps up to 500 messages per conversation. Threads open with
the newest 60; **Load older messages** pages through the retained local history.
This is a bounded cache, not a complete archive or phone-history import.
This integration currently requires a numbered Signal account and uses one
account at a time. Other accounts' receive events are ignored. An invalid,
unreadable, or unsupported inbox cache blocks startup and is retained for recovery.

Use the plus button beside conversation search to start a chat with a Signal
contact or create a group. Signal discovery does not fill the inbox with empty
contact or group rows until you explicitly choose one there.

OmaChat honors remote deletions and removes the content of expired disappearing
messages. Their timer counts from when the message was sent, so OmaChat removes
it no later than the phone does. Removal covers OmaChat's message store, its
media cache, the helper's previews, and signal-cli's own saved copy of each
attachment. View-once attachments are not listed or cached by OmaChat, and
signal-cli's saved copy is deleted as the message arrives. Messages trimmed past
the 500-message limit lose their media the same way.

Not implemented yet: phone-history import, upstream read receipts, typing
indicators, calls, safety-number UI, complete group administration, static
sticker presentation, or in-app device revocation.

Use the phone's Linked devices screen to revoke OmaChat. Only after revocation,
remove `~/.local/share/omachat/signal-cli/` if you also want to delete the local
device keys. OmaChat's local inbox is
`~/.local/share/omachat/signal_store.json`.

## Security and maintenance

`signal-cli` decrypts messages locally and stores Signal device keys on disk.
OmaChat's cache is also local and is not an encrypted archive. Use full-disk
encryption and normal user-account protections where local confidentiality
matters. Keep `signal-cli` current as Signal's service changes, and do not use
this integration for bulk or automated messaging.

## Starting conversations

The inbox **+** picker lists contacts from the connected Signal account. A
direct selection opens a local chat without sending a message. The chat is
keyed by the contact's Signal account ID, as received messages are, so their
replies arrive in it. A group request
uses signal-cli to create a named group and may invite the selected contacts.
Explicitly opened chats are saved locally and survive helper restarts. Reopening
an active chat preserves its messages and unread state until it is acknowledged.
If group creation reports an uncertain outcome, refresh and inspect Signal
before retrying. Closing the picker cannot undo a request already submitted.
Live creation and invitations remain unverified by the synthetic tests.
