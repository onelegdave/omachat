# OmaChat v0.3.5

- Replace the outdated preview with the current panel and a fictional chat.
  Add Tokyo Night, Catppuccin Latte, and Gruvbox base-palette captures, with
  a reproducible renderer that does not access accounts.
- Clarify browser-cookie access and refresh, same-user security limits,
  optional GIPHY network use, and the difference between separate services
  and a security sandbox.
- Tighten private-directory permissions without following leaf symlinks; fail
  closed when socket permissions cannot be restricted.
- Constrain helper GIPHY requests to public destinations, reject HTTPS
  downgrades, and keep API-key-bearing URLs out of transport error messages.
- Clean up discarded Telegram OGG voice recordings.
- Enable GitHub private vulnerability reporting and document its private form.
- Add marketplace-readiness notes and a second-machine acceptance checklist.

No dependencies are installed automatically. Marketplace submission remains
on hold until testing on oldsmaru is complete. Automated checks and source
review do not guarantee security, marketplace approval, or error-free operation.
