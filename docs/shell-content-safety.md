# Remote content and the shell

OmaChat's panel runs inside the shared Omarchy shell process, together with the
bar, notifications, and every other plugin. Anything that exhausts memory or
hangs there takes the whole desktop shell down. Messages, names, photos, and
attachments come from other people, so OmaChat v0.5.6 keeps all of it away from
the shell's decoders and rich-text loader, and bounds what the shell holds.

[HANCORE-linux](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6137173525)
reported that an incoming WhatsApp `image/png` document was decoded by Qt in
the shell, where compressed PNG text chunks are expanded and kept with no
aggregate budget. The fix covers that whole class rather than one service or
format, and an independent audit of the remaining routes from remote content
into the shell produced the other changes below.

## Images

The panel decodes only previews that the helper wrote. The helper runs as a
separate process, and [internal/safeimage](../internal/safeimage/safeimage.go)
converts an image there:

- It recognizes PNG, JPEG, GIF, and still WebP by their leading bytes and
  ignores the sender's MIME type and file name. Anything else, including SVG,
  TIFF, animated WebP, and video, gets no preview.
- It reads the dimensions from the header and refuses images over 16384 pixels
  on a side, 64 megapixels, or 192 MiB of decoded pixels before decoding any
  pixel data. The estimate assumes the costliest decoding the header allows:
  16-bit PNGs as 64-bit color with transparency, interlaced PNGs with their
  passes, and progressive JPEGs with their coefficients. A JPEG with more than
  32 scans, or too many scans for its size, is refused after a walk of its
  markers, since each scan costs a pass over the whole image.
- Shrinking a large image uses a bounded working buffer.
- For a GIF it walks the block structure without decompressing anything, so the
  frame count and total frame area are known first. An animation keeps moving
  only up to 500 frames, 64 megapixels of frames, and 1024 pixels on a side;
  otherwise its first frame becomes a still preview.
- Go's decoders skip PNG text chunks without inflating them. The preview is a
  new JPEG (opaque images), PNG, or GIF drawn from the decoded pixels, at most
  1280 pixels on the long side for attachments and 256 for profile photos. No
  text, color profile, or other metadata from the source survives.
- One conversion runs at a time, so concurrent requests cannot multiply the
  memory budget.

The helper applies previews at the socket, on everything the panel receives:
media replies carry a `preview` beside the original `path`, and every
conversation's profile photo is replaced by its preview. A photo still
converting is sent blank, and the conversation is sent again with its current
state once the preview is ready. Notification icons, which Omarchy's
notification center also decodes inside the shell, use the same previews, and
so does the composer's thumbnail of a file you pick to send.

Previews are stored beside each service's media cache, so unpairing a service
deletes them with everything else it cached; unpairing also clears previews of
your own files, and a preview whose source disappears while converting is
removed. Each preview directory is capped at 64 MiB. Images already on disk,
such as one you just sent, carry their preview in the message. Originals stay where they were. Clicking a preview opens the original
in your image viewer, outside the shell. An image without a preview shows
**Open image**.

The shell keeps running the previous release's panel code after an update until
it restarts, and that code decodes originals and renders names as rich text. A
panel identifies itself on every request; one that does not receives status
only, and any other request answers "OmaChat was updated. Restart the shell to
finish updating." No messages, names, or media reach it.

## Video

The panel no longer plays any video inline. Messenger and WhatsApp GIF-style
clips were decoded by Qt Multimedia and FFmpeg in the shell; they now open in an
external player like other video. Voice notes already played through `ffplay`
in its own process.

## Text

Qt's `Text` element guesses whether a string is rich text, and rich text can
load `<img>` sources, including from the network. Every `Text` in the panel now
declares plain text, so a contact name or message preview holding markup is
shown as typed. The message body is the only styled text: it is HTML-escaped
before links are added. A source test enforces both.

## Bounded models

Remote senders decide how many messages and conversations arrive. A thread keeps
the newest 1,000 messages and each service the newest 1,000 conversations;
loading older history stops at that size. Copy chips for codes and numbers are
limited to four per message, from the first 4,096 characters. Conversation
previews are cut to 500 characters before layout.

The helper also bounds what it sends: message text to 64 KiB, names and
previews to 1 KiB, identifiers to 1 KiB, and attachments, reactions,
participants, and list lengths to fixed counts. A reaction longer than 64 bytes
is not an emoji and is dropped. The panel also limits the attachment
bookkeeping it keeps to what the open thread shows. A frame over 8 MiB is never written; an oversized reply becomes
an error and an oversized event is dropped.

## Other routes

- Copied text reaches `wl-copy` on standard input with a fixed type. It was a
  command-line argument, which other local accounts can read through `/proc`
  and which `wl-copy`'s clipboard-serving process keeps.
- Messenger disappearing and view-once messages show a placeholder; their text
  and media are neither shown nor stored, matching WhatsApp. That includes the
  conversation preview, media arriving later, and later updates to the same
  message.
- Messenger cache files get a suffix from a fixed list of image, audio, and
  video types. A sender-chosen name such as `clip.desktop` no longer decides
  what `xdg-open` launches.

## Verification

Go tests feed the converter PNGs with many large compressed text chunks,
dimension and GIF frame-area bombs, JPEG comments, GIF comment extensions, and
SVG, TIFF, BMP, and MP4 bytes, and check the outputs carry no source text. Daemon
tests check media replies, avatar republishing, symlinked and outside sources,
legacy panels, notification icons, and the outbound budgets. Node and Python
tests cover the panel's bounds, plain text, the absence of in-process media
playback, and copying through standard input.
