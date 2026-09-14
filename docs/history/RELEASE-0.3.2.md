# OmaChat v0.3.2

- Improve theme-aware contrast, selected controls, and text sizes.
- Wrap and scroll long setup instructions instead of truncating them.
- Refresh Settings with current service guidance and third-party credits.
- Simplify public AI assistance credit.
- Recover automatically when the shell-owned helper starts slowly.
- Document the proposed optional-service selection design.

Verified with helper tests, Go vet, plugin validation, and QML regression
checks covering dark/light palettes, large text, and delayed helper startup.
Service-selection switches are planning only, not part of this release.
