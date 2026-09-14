# Working on OmaChat

Created and maintained by OneLegDave. Report issues with your Omarchy version, whether `omachatd` was running, and the action that failed. Strip phone numbers, message bodies, cookies, and GIPHY keys from public diagnostics.

## Verify a change

```bash
go test -mod=vendor ./...
make helper
omarchy plugin validate .
```

`make helper` and the panel Build helper button compile `omachatd` with
`CGO_ENABLED=1`. WhatsApp session storage uses `github.com/mattn/go-sqlite3`,
which needs a C compiler (`gcc` or `clang`). The plugin does not install Go
or a C toolchain.

Edit the checkout under `~/Projects/omachat` or `~/.config/omarchy/plugins/onelegdave.omachat`. Never edit packaged files under `/usr/share/omarchy/`. After QML or helper changes, `omarchy restart shell`.

Build with `go build -mod=vendor`. Do not add an ELF to git. Never install,
update, or download dependencies on the user's behalf. Document requirements
in [the dependency guide](docs/dependencies.md) and explain missing tools in
the app so the user can choose whether to install them. Keep all three
[service guides](docs/README.md) aligned with actual supported behavior.

## Release

1. Match `manifest.json` version, README, and any tag.
2. Run the checks above. If dependency versions changed, regenerate `vendor/`
   deliberately during development and review the resulting licenses and diff.
   User builds must continue working with module downloads disabled.
3. Screenshots in git (`preview.png`, `docs/screenshots/`) must not show real contacts, phone numbers, message bodies, avatars of real people, hostnames, or accounts. Use fake demo data or crop/censor first.
4. Preserve upstream credits, licenses, and existing history.
5. Push `main` and, for a numbered release, an immutable `vX.Y.Z` tag and GitHub Release.

## Regression checks

Run `make test`, `make lint`, and `make validate` for the helper and manifest.
Use `go test -race -mod=vendor -count=1 ./...` to check the concurrency tests.

`make test-ui` runs JavaScript regression tests and the actual QML components
with synthetic contacts in a short-lived demo window on the active Wayland
desktop. This developer check needs Node.js, Python 3, Quickshell, QtTest, and
the Omarchy shell UI modules.
It also exercises the panel's Build helper button in a temporary checkout and
checks the refresh RPC against an isolated, unpaired daemon.
It does not pair with Google, use real conversations, or send messages.

The UI suite also checks long setup explanations and Settings on dark and
light palettes at a larger text size, selected-choice contrast, and recovery
when the helper starts after the first socket connection attempt. Set
`OMACHAT_UI_ARTIFACTS` to an existing local directory to save synthetic UI
screenshots for visual review. Never substitute real conversations in these
fixtures.
