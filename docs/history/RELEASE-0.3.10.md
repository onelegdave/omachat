# OmaChat v0.3.10

- Add visual activity feedback and continuous animation while the helper is being built.
- Display a rotating spinner icon in the build guidance hero when compilation starts,
  replacing the static icon with a smooth 1200ms infinite rotation that resets cleanly
  upon build completion.
- Add an animated indeterminate progress indicator (`buildActivityIndicator`) below
  the build hero, providing continuous visual movement across the panel width while Go
  compiles in the background.
- Display a dedicated background compilation status label with a spinning indicator icon.
- Update the build button to show a spinning icon (`iconText: "󰑐"`, `iconSpinning: true`)
  and active "Building..." label while the build is underway.
- Update the QML test suite in `tests/qml/build.qml` to verify that the build activity
  indicator is present and visible, and that the build button spinning animation is active.

Verification: Go unit tests, race tests, QML integration tests, Go vet, and local
plugin validation passed. No dependencies are installed automatically.

Publishing is not marketplace submission or security certification. Marketplace
submission remains on hold pending the owner's testing and explicit approval.
