# OmaChat (Omarchy plugin)

Native Omarchy shell plugin: chat in the bar. Google Messages is the first network.

- Plugin id: `onelegdave.omachat`
- Kinds: `service` + `bar-widget`
- The shell owns `bin/omachatd` as a child process. Do not add a systemd unit.
- Build with `go build -mod=vendor`. Do not download modules at runtime. Do not ship an ELF in git. Do not auto-install Go.
- Install path is `omarchy plugin add`. The installer must not run hooks or sudo.
- QML uses `qs.Ui` and `qs.Commons` (`Panel`, `BarWidget`, `PanelHero`, `Color`, `Style`).
- Protocol helper lives under `cmd/omachatd` and `internal/`. It is adapted from Marc Ford's MIT-licensed daemon; keep that attribution in NOTICE and LICENSE.
- Authorship is OneLegDave. Do not add AI tools as authors or Co-authored-by trailers.
- US English in user-facing copy. No em dashes.
