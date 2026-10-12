# OmaChat v0.5.0

## Highlights

- Optional unified inbox combines enabled services while preserving provider
  identity, message routing, and drafts. It defaults off. Its status badge
  summarizes enabled services, including a count when some are offline.
- Configurable keyboard navigation and bundled offline emoji search, with
  optional search-text retention.
- Bounded WhatsApp history with cached paging, older phone-page requests,
  recoverable migration, contact identity aliases, and profile photos.
  History defaults to 128 MB; photo and message availability depends on the phone.
- Conversation restoration, sidebar controls, desktop notifications, Telegram
  photos, and optional automatic pop-out opening.
- Empty-thread unread and Messenger read-state fixes; helper builds work without
  Git metadata and stage completed artifacts outside watched plugin source.
- Fresh fictional screenshots and expanded in-app contributor credits.

## Thank you

- **Andrew Birklid (@tumbleweedlabs)**: [#2](https://github.com/onelegdave/omachat/pull/2),
  [#3](https://github.com/onelegdave/omachat/pull/3),
  [#5](https://github.com/onelegdave/omachat/pull/5), and
  [#7](https://github.com/onelegdave/omachat/pull/7) for unread/read-state fixes,
  Git-independent builds, and automatic pop-out.
- **Alex Vlissidis (@alexvlis)**: [#8](https://github.com/onelegdave/omachat/pull/8)
  for conversation restoration, sidebar controls, notifications, Telegram photos,
  and scrolling to the latest messages on reopen.
- **Matthias MB (@Hizoul)**: selected original work from
  [#9](https://github.com/onelegdave/omachat/pull/9) for keyboard and emoji UX,
  WhatsApp history/photos, and the optional unified inbox. Shared changes on
  that combined branch remain credited to their original authors above.
- **@ozz1ee-dev**: [build-loop reproduction and staging discussion](https://github.com/onelegdave/omachat/issues/1).

Original contributor authorship, Marc Ford's MIT attribution, and vendored
licenses/notices are retained. OneLegDave remains the maintainer. Codex provided
integration, fixes, and verification; historical Claude assistance remains
acknowledged. See [full credits](../../CREDITS.md).

## Verification and limits

The maintainer reports successful fresh-install use with Google Messages
disabled. Google Messages was not exercised in this acceptance pass. Automated
fixtures and demonstration screenshots do not establish every live provider
or recovery scenario. The All-tab connection correction has dedicated model
and native-panel checks.

Signal ([#4](https://github.com/onelegdave/omachat/pull/4)) and new conversation
creation ([#6](https://github.com/onelegdave/omachat/pull/6)) remain separate reviews
and are not features of this release.

## Update and marketplace

Update through the native plugin updater:

```sh
omarchy plugin update onelegdave.omachat
```

Rebuild the helper with the panel's **Build helper** action after updating, then
restart the shell if prompted. Existing settings and paired credentials are
retained; this release does not require deliberate re-pairing. WhatsApp history
migrates separately from its device credentials and is bounded to the configured
cache size. The optional unified inbox stays off unless enabled in Settings.

The helper continues to build locally from vendored source. No executable is
shipped in Git, no dependency is installed automatically, and the shell owns
the helper process. Daily release checks, unified inbox, and automatic pop-out
remain opt-in.

Fresh marketplace compatibility/security checks and explicit maintainer
approval must cover the final public commit. Previous marketplace approval
does not cover this release. The existing verified snapshot remains in place
until the Marketplace approves and publishes the update.
