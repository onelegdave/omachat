# OmaChat v0.6.0

This release adds an optional Signal service and starting new direct and
group chats. It also keeps remote content away from the shell's image
decoders and rich-text loader and bounds what the panel holds. That work was
prepared as v0.5.6, which was never published; it is part of this release.
It includes everything in [v0.5.5](RELEASE-0.5.5.md).

[Remote content and the shell](../shell-content-safety.md) describes the
content safety design and its limits.

## New

- **Signal (experimental, off by default).** Link OmaChat as a Signal device
  through the separately installed
  [signal-cli](https://github.com/AsamK/signal-cli). It supports direct and
  group text, attachments with captions, voice notes, reactions, remote
  deletion, and disappearing messages, with a local inbox of up to 500 messages
  per chat. If `signal-cli` is missing, OmaChat asks before opening a terminal
  that runs the fixed `yay -S --needed signal-cli` command, and yay asks again.
  The terminal then asks whether to restart the shell. See
  [Signal setup and scope](../services/signal.md).
- **Start a conversation.** The **+** beside conversation search lists the
  connected service's contacts. Pick one to open a direct chat, or choose
  **Group**, pick at least two contacts, and name it. Each service uses its
  own protocol's real group creation; OmaChat never fakes one. New encrypted
  Messenger chats still start in Messenger.

## Fixed

- **Images are re-encoded before the panel shows them.** Previously the panel
  decoded downloaded originals itself. An incoming PNG with compressed text
  chunks below the download cap could exhaust the shell's memory when its
  conversation was viewed. The helper now decodes PNG, JPEG, GIF, and still
  WebP images in its own process, within size and frame limits checked before
  decoding, and writes a new preview with no metadata. This covers attachments
  on every service, profile photos, and notification icons. Clicking a preview
  opens the original in your image viewer. Images the helper cannot preview,
  such as animated WebP stickers, show **Open image** instead.
- **Names and previews are plain text.** A contact name or message preview
  holding markup could make the shell fetch an image from the network. Every
  label now shows text exactly as sent; the HTML-escaped message body keeps its
  links.
- **GIF-style clips are converted outside the shell.** Messenger and WhatsApp
  send GIFs as short videos, which the panel used to decode inside the shell.
  The helper now turns them into an animated preview with optional `ffmpeg`,
  in its own process and within time, size, and memory limits; clicking one
  opens the original video. Without `ffmpeg` they show **Open video**. Other
  video already opened in your video player.
- **Copied text no longer appears in process arguments.** Copying a message or
  code sends it to `wl-copy` on standard input.
- **Bounded panel state.** A thread keeps its newest 1,000 messages, each
  service its newest 1,000 conversations, and a message shows at most four copy
  chips. The helper limits message text, names, reactions, identifiers, list
  sizes, the new-conversation contact list, and the size of each reply or event.
- **Messenger disappearing messages are not kept.** Disappearing and view-once
  messages show a placeholder, and their text and media are not stored, as on
  WhatsApp.
- **Messenger file names cannot choose how a file opens.** Cached media gets a
  file extension from a fixed list of image, audio, and video types.
- **WhatsApp status updates are no longer chats.** Status and story broadcasts
  were read as an ordinary conversation from live messages and phone history.
  Both routes now skip them, and a status conversation saved by an earlier
  release is removed with its messages and media on upgrade.
- **Reading older messages stays put.** Jumping up in a thread right after the
  panel reopened could snap the view back to the newest message, and later
  refreshes kept forcing it there.
- **The in-panel helper build is safe on any plugin filesystem.** The build
  already staged outside the watched plugin folder
  ([#1](https://github.com/onelegdave/omachat/issues/1)). It now syncs the
  helper to disk before the final rename, stages on the filesystem of the
  helper's own folder (which may differ from the plugin's), and removes partial
  helpers that releases before 0.5.0 left in `bin`. If neither the hidden
  folder beside the plugin nor the cache folder shares that filesystem, the
  build stops with an explanation before writing anything in the plugin,
  instead of copying the helper in where a shell reload could interrupt it. A
  cache folder inside the plugin, including through a symlink, is never used
  or created.
- **Typing after a restored draft lands at its end.** When the panel filled in
  a draft, a returned attachment caption, or an emoji search, the cursor
  stayed at the start, so the next keystroke went before the text. Assigned
  text now leaves the cursor at the end, as it did before 0.5.2.
- **Opening the panel without a chat selected starts at the conversation
  list,** as documented, and moves to search only when the list is empty.

## Changed behavior

- Inline previews are at most 1280 pixels on the long side. Animated GIFs keep
  moving up to 500 frames and 1024 pixels on a side; larger ones show their
  first frame.
- GIF-style clips preview up to 8 seconds at 480 pixels, looping.
- Loading older messages stops after 1,000 messages in a thread; older history
  remains in the service's own apps.
- Signal attachments over 32 MiB are not downloaded into OmaChat, and Signal
  stories are not fetched. When a Signal message is deleted, expires, is
  view-once, or leaves the 500-message store, OmaChat also deletes the helper's
  previews and signal-cli's saved copy of its media.
- Google Messages group creation first asks the phone for the recipients'
  conversation and creates a named RCS group only when the phone requires one.
- `golang.org/x/image` v0.45.0 is vendored for WebP decoding and scaling. It
  needs exactly the `golang.org/x/sys` and `golang.org/x/text` versions already
  vendored, so no other dependency changes.

## Credits

Thank you to Andrew Birklid for Signal support and conversation creation, to
Matthias MB for the WhatsApp status filter and formatting fixes, to
HANCORE-linux for identifying the PNG text-chunk exhaustion, and to
@ozz1ee-dev for the build-loop report. Their commits keep their original
authorship. Claude implemented the integration, its fixes, and its tests, and
Codex audited the routes from remote content into the shell and reviewed the
work. Alex Vlissidis, Marc Ford, and upstream contributors retain their
existing attribution. See [full credits](../../CREDITS.md).

## Verification and limits

Checked October 17, 2026 on Arch Linux with Go 1.27.0, Quickshell 0.3.1, Qt
6.11.2, and Omarchy 4.0.4, from an isolated copy of the release with invented
data. No real account, message, pairing, or package installation was used.

- `gofmt`, `go vet`, and an offline `-mod=vendor` build are clean.
- The full Go suite passes (549 top-level tests, 625 including subtests), as do
  the Google Messages client fixtures, and the whole suite passes again under
  `-race`. Two Signal tests skip by design: one needs an installed
  `signal-cli`, and one only runs as a child process of another test.
- 61 Python tests and the emoji data checksum check pass, including a real
  two-filesystem helper build watched with inotify. The 76 Node model tests
  pass on Windows with Node 24.22; one of them calls the Python checksum
  generator, which needed a temporary `python3` alias there.
- The headless QML subset passes: 19 isolated fixtures and 199 checks, including
  new-conversation picker lifetimes, pagination, media previews, safe fields,
  and an isolated helper build. `omarchy plugin validate` passes.
- The full native Wayland QML suite passes on Hyprland: 24 fixtures and 432
  checks, including keyboard routing, pop-out placement, unpair, a real
  isolated helper build, a missing build launcher, an upgrade from an older
  helper, and delayed helper startup. Each fixture used a mock service, its own
  data and runtime folders, and a private D-Bus session. Only `hyprctl` reached
  the running compositor, to place the test's own pop-out window.

New tests cover the WhatsApp status filter on both routes and in a stored
cache, Signal media removal on deletion, expiry, view-once, and trimming
(including signal-cli's copy and the helper's previews), the Signal download
cap and RPC line bound, safe Signal suffixes, a UUID-keyed reply landing in a
started chat, Google Messages RCS group requests, bounded contact lists,
helper builds watched the way the Omarchy shell watches plugins (across two
real filesystems, and with layouts that are refused before any plugin write,
including a cache folder reached through a symlink), and the cursor after
assigned text.

Earlier work in this release is also tested: Go tests feed the image converter
PNGs with many large compressed text chunks, dimension, GIF frame-area, and
JPEG scan-count bombs, JPEG and GIF comments, and SVG, TIFF, BMP, and MP4
bytes, and clip previews run against a real `ffmpeg`, including refused
playlist and concat files and a hung `ffmpeg` that is stopped.

Not run: an update of a real installed copy, and any live service. Signal was tested against a simulated `signal-cli`, and
conversation creation against synthetic contacts; neither establishes live
compatibility.

## Update and Marketplace

```sh
omarchy plugin update onelegdave.omachat
```

This release changes the Go helper. Rebuild it with the panel's **Build helper**
action when prompted, then restart the shell. Until the shell restarts, the
previous release's panel code keeps running, and the rebuilt helper sends it no
messages, names, or media: it shows "OmaChat was updated. Restart the shell to
finish updating." Existing paired accounts and settings are retained; no
re-pairing or automatic dependency installation is needed. Signal stays off
until you enable it. No executable is shipped in Git.
