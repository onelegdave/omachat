# Service selection and opt-outs

Open **Settings > Services**, choose Enabled or Disabled for each service, then
select **Apply service choices**. Edits do not take effect until you apply them.
You can disable all services and leave Settings accessible.

## What changes

Applying a different selection briefly restarts the shared protocol helper.
Other enabled services reconnect too. The app waits for the replacement helper
to confirm the saved selection before reporting success or hiding disabled tabs.
The shell itself does not restart, and no systemd unit is involved.

Disabled services do not start their clients, pair, reconnect, sync, poll,
download media, or accept messaging operations. Their unread counts are excluded.
Their credentials and cached account files remain on disk for re-enabling.
**Unpair this desktop** remains a separate action; enable a service before
unpairing it from OmaChat, or revoke the linked device from your phone.

Text drafts remain isolated by service and conversation while the shell stays
running, including the expected helper restart. Leaving a service stops recordings
and clears staged attachments, as switching tabs normally does. Drafts are not
persisted across a full shell restart.

A message already submitted before a service change may still arrive. OmaChat
does not replay requests after restarting. Check the conversation before retrying
a send whose outcome was interrupted.

## First run and upgrades

- Existing installations without a saved service list keep the original three
  services. Messenger remains opt-in and never starts merely because an older
  installation is upgraded.
- A fresh installation with no configuration or account/session evidence starts
  with no services enabled and asks you to choose.
- Saving an explicit empty list means all services stay off after restarting;
  it is not treated as a missing preference.
- Re-enabling uses retained credentials. Expired credentials may require pairing.

Selecting fewer services does not reduce the shared helper's build requirements.
No service choice installs packages or creates accounts. Review optional tools
in the [dependency checklist](dependencies.md).

## Implementation boundary

The private configuration stores a validated `enabledServices` list. The global
`setEnabledServices` RPC writes it atomically and reports `restartRequired` while
the old helper is still running. New service operations are blocked during that
transition. The process boundary stops background work that upstream disconnect
methods do not fully cancel. The new helper starts only enabled services.

If restart cannot be confirmed, the app reports that condition instead of claiming
success. Restart the Omarchy shell and review the saved choices. No send is retried
automatically.
