# OmaChat live test results

Date: 2026-09-14

The user authorized live pairing, revocation, and message tests to their own
number. Test messages and account details are not included as fixtures in this
repository. The installed plugin now contains the reviewed fixes, and its
binary was checked against the local build.

## Passed during the original live tests

| Test | Evidence |
| --- | --- |
| Updated helper starts in the shell | A new child helper process started; shell IPC answered and the plugin connected. |
| Live inbox refresh | The network refresh RPC succeeded and returned an inbox of 50 conversations. |
| Actual panel rendering | The installed panel was opened and visually inspected. |
| Local unpair cleanup | Status became unpaired; the session file disappeared; the media directory was recreated with mode 0700. |
| No credential resurrection on restart | Restarting the shell/helper left the session file absent and status unpaired. |
| Remote revocation | The user confirmed the OmaChat device disappeared from the phone's Device pairing list before re-pairing started. |
| Browser-cookie re-pairing | The existing Brave profile supplied cookies; the phone confirmed the saxophone emoji; the daemon emitted paired and connected events. |
| Paired persistence | The new session file had mode 0600; earlier restart checks reported connected. A later restart exposed premature readiness and automatic re-pairing; see the follow-up below. |
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

Independent code reviews and focused regression tests were used during the
image investigation. The final behavior was checked against upstream code and
a working web-client send. The combined-part failure was isolated with live
controls and fixed by sending the attachment and caption separately. The lead
reviewed and strengthened serialization and cancellation checks and added QML
tests for split acknowledgements, partial failures, and duplicate submission
prevention.

Final checks passed: Go tests with the race detector, go vet, manifest
validation, model tests, and actual QML regression and isolated helper-build

The original unpair method logged upstream errors without returning them.
The phone's paired-device list supplied independent confirmation in that test.
The later unpair reporting fix returns those errors and displays a warning.
An accepted protocol request still does not independently prove the device has
disappeared from the phone.

Automatic recovery from deliberately expired cookies was not induced.
Browser-cookie extraction and explicit re-pairing did pass. Outgoing media now has a successful phone-synced delivery with its attachment
intact. These tests do not establish every carrier, format, or device combination.

## State at the end of the image tests

- The helper is connected, the phone is responding, and live refresh passes.
- The installed binary matches the final fixed local build.
- The original installed plugin was backed up outside the plugin directory
  before the update, under the user's local state directory.
- The unsuccessful SIM and dimensions trials are absent from source and installed code.
- The test messages were left in the user's self-chat.
- These live tests finished before publication. Subsequent publication is
  recorded by Git tags and GitHub Releases. Marketplace submission is deferred.


## Reconnect follow-up after v0.1.1

After the release restart, the helper reported connected while a Gaia emoji
challenge was still active. The user received an unexpected pairing request
and dismissed it. Code inspection found automatic pairing in authentication
recovery and premature connected transitions before phone sync.

The installed follow-up removes automatic pairing, requires authenticated sync
before connected, and rejects late callbacks from invalid or replaced sessions.
The first live restart with the invalid stored session now reports a clear
instruction to select Pair with Google, with no emoji or paired event.

| Follow-up test | Result |
| --- | --- |
| Invalid stored session after restart | Explicit renewal error throughout 30 seconds; zero emoji or paired events |
| Intentional Pair with Google through the panel | User confirmed pairing completed |
| Authenticated refresh after pairing | Succeeded; 50 conversations returned |
| Credential persistence | Session file mode 0600 |
| Restart with the newly confirmed session | Restored connected state; live refresh succeeded |
| Observe restarted helper for 36 seconds | No pairing state, emoji challenge, or paired event |
| Installed helper identity | SHA-256 matched the locally built helper |

Current follow-up status: the patched helper is installed and connected, and
live refresh passes after restart. The earlier successful image delivery remains
valid. These checks verify the reported reconnect failure and recovery; they do
not establish indefinite session validity against future Google-side changes.
No new release or marketplace submission has been made for this follow-up.


## Unpair reporting verification

Remote refusal and local cleanup failures were tested with synthetic accounts,
injected revocation outcomes, and a local rejecting HTTP proxy. Real QML tests
verified the warning, failed RPC replies, duplicate request protection, and late
replies during a newer pairing. No live account was unpaired to induce a failure.

The updated helper and QML were installed and matched the local build/source.
After the shell restart, the existing account reconnected and a live inbox
refresh succeeded with no pairing challenge.


## Older-message pagination verification

A read-only cursor check against the authorized self-chat fetched an initial
five-message page and a second five-message page using the returned cursor ID
and timestamp. All five records on the second page were distinct and older,
and the cursor advanced. Counts and ordering were recorded without adding real
message content or identifiers as repository fixtures.

The new UI was tested separately with over 60 synthetic messages in actual QML.
It preserved the visible message and pixel offset through pagination, incoming
messages, and refresh. Stale replies, retries, overlapping pages, cursor stalls,
and end-of-history behavior passed. The live check used small pages to validate
the protocol without reading an unrelated conversation.

The installed pagination UI matched the local QML and JavaScript sources.
After installation and shell restart, the saved session reconnected and live
inbox refresh succeeded without a pairing challenge in the observed status.
