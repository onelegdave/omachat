# OmaChat v0.3.4

- Choose Google Messages, WhatsApp, and Telegram independently in Settings >
  Services. Apply explicitly; all services may be disabled.
- Disabled services do not start their clients, and their tabs and unread counts
  are hidden. Saved credentials remain available for re-enabling.
- Fresh installations ask which services to enable. Existing installations
  without a saved selection keep all three enabled.
- Applying a changed selection briefly restarts the shell-owned shared helper.
  Other enabled services reconnect too. Success is reported only after the
  replacement helper confirms the saved choices.
- Text drafts remain while the shell stays running. Pending attachments are
  cleared when leaving a service. Interrupted sends are never automatically
  replayed; check the conversation before retrying.
- Fix QML boolean evaluation warnings during helper startup.

No dependency is installed automatically. Selecting fewer services does not
change the shared helper's build requirements. Unpairing remains separate.

Regression coverage includes fresh and legacy configuration, failed saves,
disabled RPC guards, a real isolated helper opt-in/opt-out restart cycle,
draft retention, and dark/light large-text readability.
