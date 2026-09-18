# OmaChat v0.4.4

This release removes GIPHY search to meet the Omarchy marketplace security
review. Local GIF attachments are unchanged.

## Highlights

- Remove in-app GIPHY search from Google Messages, WhatsApp, and Messenger.
  GIPHY accepts its API key only as a URL query parameter, and the marketplace
  review does not accept credentials in request URLs.
- Remove the GIPHY API key field from Settings and the `gifSearch`,
  `gifFetch`, and `setGiphyKey` helper methods.
- Delete a `giphyApiKey` left in `config.json` by an earlier release when the
  helper starts, so no unused third-party credential remains on disk.
- Keep sending GIF files from your computer on Google Messages, WhatsApp
  (ffmpeg), and Messenger, and keep inline GIF playback.

## Update

```bash
omarchy plugin update onelegdave.omachat
```

Open OmaChat after updating and select **Rebuild helper** when prompted. The
helper is built locally from vendored source. No dependency is installed
automatically. This release does not require re-pairing.
