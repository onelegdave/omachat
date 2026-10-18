# OmaChat v0.5.6

This release keeps remote content away from the shell's image decoders and
rich-text loader, and bounds what the panel holds. The panel runs inside the
shared Omarchy shell process, so a hostile image, name, or message could
otherwise affect the whole desktop shell. It includes everything in
[v0.5.5](RELEASE-0.5.5.md).

[Remote content and the shell](../shell-content-safety.md) describes the design
and its limits.

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
  sizes, and the size of each reply or event.
- **Messenger disappearing messages are not kept.** Disappearing and view-once
  messages show a placeholder, and their text and media are not stored, as on
  WhatsApp.
- **Messenger file names cannot choose how a file opens.** Cached media gets a
  file extension from a fixed list of image, audio, and video types.

## Changed behavior

- Inline previews are at most 1280 pixels on the long side. Animated GIFs keep
  moving up to 500 frames and 1024 pixels on a side; larger ones show their
  first frame.
- GIF-style clips preview up to 8 seconds at 480 pixels, looping.
- Loading older messages stops after 1,000 messages in a thread; older history
  remains in the service's own apps.
- `golang.org/x/image` v0.45.0 is vendored for WebP decoding and scaling. It
  needs exactly the `golang.org/x/sys` and `golang.org/x/text` versions already
  vendored, so no other dependency changes.

## Credits

Thank you to HANCORE-linux for identifying the PNG text-chunk exhaustion.
Codex audited the remaining routes from remote content into the shell and
reviewed the correction; Claude implemented and verified it. Andrew Birklid,
Alex Vlissidis, Matthias MB, ozz1ee-dev, Marc Ford, and upstream contributors
retain their existing attribution and original authorship. See
[full credits](../../CREDITS.md).

## Verification and limits

The full Go tests, Google Messages client fixtures, race checks on the changed
packages, vet, 76 Node tests, and 52 Python tests pass. Animated clip
previews are tested against a real `ffmpeg`, including playlist and concat
files that are refused before `ffmpeg` runs and a hung `ffmpeg` that is
stopped. New Go tests feed the
converter PNGs with many large compressed text chunks, dimension, GIF
frame-area, and JPEG scan-count bombs, JPEG and GIF comments, and SVG, TIFF,
BMP, and MP4 bytes. They check that previews carry no source text, that
profile photos reach the panel only as previews, that panels from earlier
releases receive status only, and that previews do not outlive an unpaired
account.

The headless QML fixtures, native plugin validation, and live conversations
were not run for this release candidate.

## Update and Marketplace

```sh
omarchy plugin update onelegdave.omachat
```

This release changes the Go helper. Rebuild it with the panel's **Build helper**
action when prompted, then restart the shell. Until the shell restarts, the
previous release's panel code keeps running, and the rebuilt helper sends it no
messages, names, or media: it shows "OmaChat was updated. Restart the shell to
finish updating." Existing paired accounts and settings
are retained; no re-pairing or automatic dependency installation is needed. No
executable is shipped in Git.
