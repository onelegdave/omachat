# Google Messages service guide

[Overview](../../README.md) · [Dependencies](../dependencies.md)

Google Messages runs through the shared `omachatd` helper and mautrix libgm.
Build the helper first using the README instructions. Keep the Android phone
online for message relay, including RCS and end-to-end chats.

## Pairing

1. Open [Messages for web](https://messages.google.com/web) in a supported
   Chromium-family browser and sign in. A general Google login is insufficient;
   Messages for web must have issued its own cookies.
2. Unlock the desktop keyring. Browser cookie access uses `sqlite3` and
   `secret-tool`; OmaChat does not install them automatically. Settings > Tools
   lets you review their source and choose whether to install missing tools.
3. Select Google Messages in OmaChat, check the selected browser profile,
   and choose **Pair with Google**.
4. On the phone, confirm the emoji matching the one shown in OmaChat.

The QR fallback is for phone versions that still offer a QR scanner under
Device pairing. Scan with that scanner, not the camera or Lens. If no scanner
is available, use browser pairing.

## Supported behavior and limits

Read conversations, page older messages, and send text, photos, GIF files,
captions, and reactions. Voice notes require optional ffmpeg/ffplay; GIPHY
search requires your own API key in Settings. Local GIF attachments need no
API key. Calling is unavailable.

The inbox is limited to 50 conversations. Threads begin with 60 messages and
load earlier pages on request. Captions are sent separately from attachments.
Pairing consumes a Messages-for-web device slot. The unofficial protocol can
break when Google changes its service.

## Recovery and removal

For missing cookies, open Messages for web in the selected profile again.
Check the keyring and cookie tools if browser access fails. For an invalidated
session, explicitly choose **Pair with Google** again; recovery never starts
pairing automatically. A reconnect stays Connecting until authentication succeeds.

Choose **Unpair this desktop** on the Google tab to remove local credentials
and media and request remote revocation. If revocation fails, remove the device
in **Google Messages > Device pairing** on your phone. Other services remain paired.

Credentials are stored in `~/.local/share/omachat/session.json` with mode 0600.
Downloaded attachments use `~/.cache/omachat/media/`. Shared configuration is
in `~/.local/share/omachat/config.json`. See [Security](../../SECURITY.md).
