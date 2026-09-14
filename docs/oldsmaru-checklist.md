# Second-machine acceptance check

Marketplace submission is on hold until OneLegDave completes this check on
oldsmaru. These are pending manual checks, not completed test claims.

- Install the published release using the README instructions. Confirm the
  plugin loads without QML errors and Settings can be opened.
- Review Settings > Tools. Confirm missing dependencies are explained, source
  links work, and nothing installs without choosing Install and confirming.
  Cancel an installation once to verify that cancellation changes nothing.
- Build the helper with the existing or deliberately chosen Go/C toolchain.
  No modules should download. Check an actionable error if a tool is missing.
- On a genuinely fresh account-data directory, confirm the initial service
  chooser appears before any service connects. Do not delete existing data
  merely to test this case; use a separate test account if needed.
- Enable only the services wanted on this machine. Pair them intentionally,
  following their service guides. Do not assume copied credentials will work.
- Disable a service and apply. Confirm its tab and unread count disappear;
  enabled services reconnect. Re-enable and check retained credentials.
- Disable all services. Confirm Settings stays accessible and the selection
  survives a shell restart. Restore the desired service selection afterward.
- Check dark and light themes, larger text, long setup errors, scrolling,
  keyboard focus, drafts, and resizing/pop-out behavior where available.
- Use an explicitly chosen test recipient for text, photo/caption, and supported
  voice features. Verify delivery on the receiving device, not just a successful
  request. Do not test Telegram sends without separate authorization.
- Restart the shell and check reconnect behavior. A full shell restart is
  expected to discard in-memory drafts. Interrupted sends must not replay.

Record release/tag, Omarchy version, enabled services, actions tested, and any
errors. Redact credentials, phone numbers, contacts, and message contents from
public reports. Do not submit to the marketplace until this review is complete.
