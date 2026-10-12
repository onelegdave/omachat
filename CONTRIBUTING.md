# Working on OmaChat

Created and maintained by OneLegDave. Report issues with your Omarchy version, whether `omachatd` was running, and the action that failed. Strip phone numbers, message bodies, cookies, and API credentials from public diagnostics.

## Verify a change

```bash
go test -mod=vendor ./...
make helper
omarchy plugin validate .
```

`make helper` and the panel Build helper button compile `omachatd` with
`CGO_ENABLED=1` through `scripts/updates.py build`. This stamps a source
fingerprint for the running-helper check and replaces the executable only
after a successful offline build. Go's automatic VCS stamping is disabled,
so installed copies do not need usable Git metadata; the helper retains its
release version and source fingerprint. Direct `go build` remains useful for
development, but an unstamped helper will prompt for a rebuild in the UI.
Builds use a private, hidden staging directory beside the plugin checkout and
keep their lock under `$XDG_STATE_HOME/omachat/` (default `~/.local/state/omachat/`).
The shell ignores hidden sibling directories. Only the completed executable,
or the complete `bin/` directory on first install, is renamed into the plugin.
The checkout's parent must be writable and on the same filesystem as `bin/`
for atomic installation. A failed build or rename preserves the old helper.
WhatsApp session storage uses `github.com/mattn/go-sqlite3`,
which needs a C compiler (`gcc` or `clang`). The plugin does not install Go
or a C toolchain automatically.

Edit the checkout under `~/Projects/omachat` or `~/.config/omarchy/plugins/onelegdave.omachat`. Never edit packaged files under `/usr/share/omarchy/`. After QML or helper changes, `omarchy restart shell`.

Build with `go build -mod=vendor`. Do not add an ELF to git. Never automatically
install, update, or download dependencies. The explicit Settings install action
uses allowlisted packages and preserves terminal and package-manager confirmation.
Do not exercise real installs in tests. Document requirements
in [the dependency guide](docs/dependencies.md) and explain missing tools in
the app so the user can choose whether to install them. Keep all three
[service guides](docs/README.md) aligned with actual supported behavior.

## Release

Keep original contributor authorship when integrating community commits. Record
each contributor's actual work in [CREDITS.md](CREDITS.md#community-contributors),
link the originating pull request or issue, and thank included contributors in
the release notes. Shared code in a combined PR remains credited to its original
contributors. Keep proposed features visibly separate from shipped features.

`make test-ui-headless` runs JavaScript/Python tests and thirteen real QML fixtures
using offscreen software rendering and isolated settings, data, and D-Bus.
It needs the already installed `qs` and `dbus-run-session` commands; it installs
nothing. This subset does not replace `make test-ui` in an isolated Wayland
session for native panel, keyboard, pop-out, unpair, build, upgrade, and connection
acceptance. Never count a pass marker followed by a crash as a successful test.

1. Keep the default branch release-ready: the native Omarchy updater fetches
   its HEAD, not the latest GitHub Release tag. Prepare release changes together
   so users do not receive an incomplete migration.
2. Match `manifest.json`, the README release link, the immutable `vX.Y.Z` tag,
   and the GitHub Release. Publish stable releases without the prerelease flag
   so the in-app latest-release check can discover them.
3. Run `make test test-ui lint validate` and the race checks below. If dependency
   versions changed, regenerate `vendor/` deliberately and review licenses and
   the diff. User builds must work with module downloads disabled.
4. Exercise an upgrade from the preceding release in an isolated profile:
   preserve saved settings and synthetic account data, confirm the release
   notice and native update instructions, detect the old helper, rebuild, and
   verify the running helper matches. Test cancellation, offline checks, a
   failed build retaining the previous executable, and a successful retry.
   Never use personal messages or credentials in fixtures.
5. Release notes must state the user-visible changes, security fixes without
   private data, known limits, the command
   `omarchy plugin update onelegdave.omachat`, whether rebuilding is required,
   and any migration or re-pairing steps. Do not promise account preservation
   when a release deliberately requires re-pairing.
6. Screenshots in git (`preview.png`, `docs/screenshots/`) must use synthetic
   data and contain no real contacts, phone numbers, messages, avatars,
   hostnames, or accounts. Preserve upstream credits, licenses, and history.
7. Publish the branch, immutable tag, and GitHub Release as one release task.
   After the [oldsmaru acceptance check](docs/oldsmaru-checklist.md) and owner
   submission approval, submit the matching version, release URL, and commit
   to the marketplace. Until then, preserve the documented submission hold.
   Verify the remote results, including the release metadata seen by the app.
   A submitted marketplace change awaiting review is pending, not published.

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
