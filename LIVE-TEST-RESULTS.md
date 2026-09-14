# OmaChat live test results

Date: 2026-09-14

The user authorized live pairing, revocation, and message tests to their own
number. Test messages and account details are not included as fixtures in this
repository. The installed plugin now contains the reviewed fixes, and its
binary was checked against the local build.

## Passed

| Test | Evidence |
| --- | --- |
| Updated helper starts in the shell | A new child helper process started; shell IPC answered and the plugin connected. |
| Live inbox refresh | The network refresh RPC succeeded and returned an inbox of 50 conversations. |
| Actual panel rendering | The installed panel was opened and visually inspected. |
| Local unpair cleanup | Status became unpaired; the session file disappeared; the media directory was recreated with mode 0700. |
| No credential resurrection on restart | Restarting the shell/helper left the session file absent and status unpaired. |
| Remote revocation | The user confirmed the OmaChat device disappeared from the phone's Device pairing list before re-pairing started. |
| Browser-cookie re-pairing | The existing Brave profile supplied cookies; the phone confirmed the saxophone emoji; the daemon emitted paired and connected events. |
| Paired persistence | The new session file had mode 0600; subsequent shell restarts reconnected successfully. |
| Text send | Send returned a provisional acknowledgement, followed by a phone-synced OUTGOING_DELIVERED record. |
| Transaction identity | The live message event preserved the request's transaction ID, allowing reconciliation with the provisional bubble. |
| Attachment download after re-pairing | An image sent directly from the phone was delivered and downloaded by OmaChat into the recreated media directory, producing a 55,875-byte file. |

## Resolved: outgoing images with captions

The original send path combined an image and its caption in one
`MessagePayload.MessageInfo` list. Upload and send acknowledgements succeeded,
but the phone discarded the image and eventually delivered only the caption.
Both a tiny PNG and a 640-by-360 JPEG reproduced the failure. Sending an image
directly from the phone worked.

The follow-up isolated the cause with these controls:

| Test | Result |
| --- | --- |
| Upload a JPEG, download and decrypt it again | Exact byte match, 13,367 bytes |
| Add SIM routing to the combined request | Still failed; trial removed |
| Add image dimensions to the combined request | Still failed; trial removed |
| Send a blue PNG through the official Messages web client | Delivered with its attachment |
| Inspect only the authorized web test's outgoing request shape | Image and caption use separate requests; image request omits dimensions, part ID, and forced RCS |
| Send the same PNG through OmaChat without a caption | OUTGOING_DELIVERED with one attachment |
| Send a JPEG with a caption through the final fixed helper | Image and caption both OUTGOING_DELIVERED under distinct transaction IDs |
| Download the final delivered image through OmaChat | 640 by 360, image/jpeg, 13,367 bytes |

The helper now sends the attachment first and an optional caption in a
separate request. Each has its own transaction ID and provisional message.
Image failure stops the caption. Caption failure preserves the accepted image
result and reports a separate error; the UI removes the submitted attachment,
retains the caption as a text draft when the composer is empty, and tells the
user to check the conversation before retrying. Duplicate attachment
submissions while a send is in flight are blocked.

The image and caption are separate chat bubbles. The send acknowledgement is
still provisional; the successful delivery results above came from subsequent
phone-synced history, not from the initial RPC response.

The user confirmed that both the larger test image and its caption arrived
on the phone.

The diagnostic helper overlays were kept outside the repository and replaced
with the final normal build. A temporary browser observation hook captured
only synthetic test sends to the authorized self-chat, was removed afterward,
and did not record account credentials. The clipboard was restored to empty.
No real message contents, numbers, media IDs, or keys were added as fixtures.

Reference inspected during request comparison:
[upstream sender](https://github.com/mautrix/gmessages/blob/main/pkg/connector/handlematrix.go).
Its combined media/caption layout was a comparison point; the live web-client
control and OmaChat image-only send established the working behavior here.

## Review and limits

agy provided a read-only revocation review. An earlier media-path review
reached its eight-minute print timeout without a usable conclusion. In the
follow-up, agy suggested required image dimensions and part IDs; the lead
challenged those claims against upstream code and a working web-client send,
and agy retracted them as unsupported. The lead isolated the combined-part
failure with live controls and implemented the fix. agy then wrote focused
regression tests; the lead reviewed and strengthened the serialization and
cancellation checks and added real QML tests for split acknowledgements,
partial failures, and duplicate submission prevention.

Final checks passed: Go tests with the race detector, go vet, manifest
validation, model tests, and actual QML regression and isolated helper-build
checks. Grok was not used because its weekly usage exceeded the reserve.

An unpair RPC returning success alone does not establish remote revocation:
the current method logs upstream errors without returning them. This test used
the phone's paired-device list as independent confirmation.

Automatic recovery from deliberately expired cookies was not induced.
Browser-cookie extraction and explicit re-pairing did pass. Outgoing media now has a successful phone-synced delivery with its attachment
intact. These tests do not establish every carrier, format, or device combination.

## Final state

- The helper is connected, the phone is responding, and live refresh passes.
- The installed binary matches the final fixed local build.
- The original installed plugin was backed up outside the plugin directory
  before the update, under the user's local state directory.
- The unsuccessful SIM and dimensions trials are absent from source and installed code.
- The test messages were left in the user's self-chat.
- These live tests finished before publication. Subsequent publication is
  recorded by Git tags and GitHub Releases. Marketplace submission is deferred.
