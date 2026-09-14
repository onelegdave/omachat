# OmaChat v0.3.6

- Clarify the helper build: required tools, potentially quiet compilation,
  several-minute first builds, automatic startup, and visible failure reporting.
- Explain that Telegram API-hash input stays blank while typing or pasting,
  distinguish it from a login password, and provide the complete setup command,
  shell restart, and phone QR-pairing steps in the app and setup script.
- Preserve message rows and decoded photo attachments across duplicate snapshots,
  receipt updates, and new messages. Keep loading text until decoding completes
  and retain still-image thumbnails while replacements load.
- Add regression coverage for Telegram setup guidance and photo-rendering stability.

No dependencies are installed automatically. All three services were reported
working by the owner on oldsmaru before this release. The new rendering changes
have automated QML coverage but still need the owner's fresh-install and live
phone-photo checks on that machine.

Publishing this release is not marketplace submission or approval. Submission
remains on hold pending the owner's testing and explicit approval.

Verification: the full uncached Go race suite, Go vet, JavaScript and Python
tests, real-QML UI/pagination/readability/photo/build/reconnection checks, and
local plugin validation passed. These checks do not guarantee security or
marketplace approval.
