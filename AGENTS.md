# OmaChat (Omarchy plugin)

Native Omarchy shell plugin: chat in the bar. Google Messages is the first network.

- Plugin id: `onelegdave.omachat`
- Kinds: `service` + `bar-widget`
- The shell owns `bin/omachatd` as a child process. Do not add a systemd unit.
- Build with `go build -mod=vendor`. Do not download modules at runtime. Do not ship an ELF in git. Do not auto-install Go.
- Install path is `omarchy plugin add`. The installer must not run hooks or sudo.
- QML uses `qs.Ui` and `qs.Commons` (`Panel`, `BarWidget`, `PanelHero`, `Color`, `Style`).
- Protocol helper lives under `cmd/omachatd` and `internal/`. It is adapted from Marc Ford's MIT-licensed daemon; keep that attribution in NOTICE and LICENSE.
- OneLegDave is the human owner and maintainer and remains the Git author and committer. For substantive Codex-assisted commits, add exactly one `Co-authored-by: Codex <noreply@openai.com>` trailer. Do not list AI tools as project owners or maintainers. Keep an honest AI-assistance disclosure in README prose.
- Codex leads agy and Grok, owns integration and final verification, and discusses material scope changes with OneLegDave. Teammates may use their own agents within assigned scope and quota. Follow `~/.config/ai-team/POLICY.md`; preserve 10% of each teammate's weekly allowance for the user.
- US English in user-facing copy. No em dashes.
