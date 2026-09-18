# Credits and third-party notices

OmaChat is a Native Omarchy Plugin created and maintained by OneLegDave.
It builds on the work of the following projects and their contributors.

| Project | Contribution to OmaChat | License or source notice |
| --- | --- | --- |
| [Marc Ford's gmessages-omarchy-plugin](https://github.com/MarcFord/gmessages-omarchy-plugin) | Original protocol-helper adaptation | MIT; Marc Ford's copyright is retained in [LICENSE-MIT](LICENSE-MIT) |
| [mautrix-gmessages / libgm](https://github.com/mautrix/gmessages) | Google Messages protocol client | [GNU AGPL v3](vendor/go.mau.fi/mautrix-gmessages/LICENSE) and [upstream exceptions](vendor/go.mau.fi/mautrix-gmessages/LICENSE.exceptions) |
| [whatsmeow](https://github.com/tulir/whatsmeow) | WhatsApp protocol and linked-device client | [MPL-2.0](vendor/go.mau.fi/whatsmeow/LICENSE) |
| [gotd/td](https://github.com/gotd/td) | Telegram MTProto client | [MIT](vendor/github.com/gotd/td/LICENSE), copyright Aleksandr Razumov |
| [mautrix-meta](https://github.com/mautrix/meta) | Facebook Messenger client, including encrypted conversations | [GNU AGPL v3](vendor/go.mau.fi/mautrix-meta/LICENSE) and [upstream exceptions](vendor/go.mau.fi/mautrix-meta/LICENSE.exceptions) |
| [go-sqlite3](https://github.com/mattn/go-sqlite3) | WhatsApp device database adapter | [MIT](vendor/github.com/mattn/go-sqlite3/LICENSE), copyright Yasuhiro Matsumoto; bundled SQLite notices retained upstream |
| [Omarchy](https://github.com/basecamp/omarchy) | Shell lifecycle, native UI components, and theme system | Supplied by the user's Omarchy installation |
| [Quickshell](https://quickshell.org/) and [Qt](https://www.qt.io/) | QML rendering and desktop integration | Supplied by the user's desktop installation |
| [FFmpeg](https://ffmpeg.org/) | Optional external recording and playback | Supplied and installed separately by the user |

OmaChat v0.4.3 and later is offered under [AGPL-3.0-or-later](LICENSE) so the
combined helper follows the copyleft terms of its Google Messages and Messenger
dependencies. OmaChat releases published before v0.4.3 retain their original
[MIT grant](LICENSE-MIT), as does Marc Ford's adapted code. Third-party source
retains its own license; the WhatsApp dependency includes MPL terms. See the actual license files and
[NOTICE](NOTICE) for local upstream modifications.

Other vendored contributors include the Go authors, Google, Uber, the
OpenTelemetry authors, and the maintainers of the modules listed in
[vendor/modules.txt](vendor/modules.txt). Their copyright and license files
remain alongside their code in [vendor/](vendor/). The inventory below links
the retained notices directly.

Created and maintained by [OneLegDave](https://www.onelegdave.dev/)
([GitHub](https://github.com/onelegdave) · [X](https://x.com/OneLegDavePDX)), with AI assistance from Codex.

## Vendored license inventory

- [filippo.io/edwards25519/LICENSE](vendor/filippo.io/edwards25519/LICENSE)
- [github.com/andybalholm/brotli/LICENSE](vendor/github.com/andybalholm/brotli/LICENSE)
- [github.com/andybalholm/brotli/flate/LICENSE](vendor/github.com/andybalholm/brotli/flate/LICENSE)
- [github.com/beeper/argo-go/LICENSE](vendor/github.com/beeper/argo-go/LICENSE)
- [github.com/cenkalti/backoff/v4/LICENSE](vendor/github.com/cenkalti/backoff/v4/LICENSE)
- [github.com/cespare/xxhash/v2/LICENSE.txt](vendor/github.com/cespare/xxhash/v2/LICENSE.txt)
- [github.com/coder/websocket/LICENSE.txt](vendor/github.com/coder/websocket/LICENSE.txt)
- [github.com/dlclark/regexp2/LICENSE](vendor/github.com/dlclark/regexp2/LICENSE)
- [github.com/elliotchance/orderedmap/v3/LICENSE](vendor/github.com/elliotchance/orderedmap/v3/LICENSE)
- [github.com/fatih/color/LICENSE.md](vendor/github.com/fatih/color/LICENSE.md)
- [github.com/ghodss/yaml/LICENSE](vendor/github.com/ghodss/yaml/LICENSE)
- [github.com/go-faster/errors/LICENSE](vendor/github.com/go-faster/errors/LICENSE)
- [github.com/go-faster/jx/LICENSE](vendor/github.com/go-faster/jx/LICENSE)
- [github.com/go-faster/xor/LICENSE](vendor/github.com/go-faster/xor/LICENSE)
- [github.com/go-faster/yaml/LICENSE-APACHE](vendor/github.com/go-faster/yaml/LICENSE-APACHE)
- [github.com/go-faster/yaml/LICENSE-MIT](vendor/github.com/go-faster/yaml/LICENSE-MIT)
- [github.com/go-faster/yaml/NOTICE](vendor/github.com/go-faster/yaml/NOTICE)
- [github.com/godbus/dbus/v5/LICENSE](vendor/github.com/godbus/dbus/v5/LICENSE)
- [github.com/google/uuid/LICENSE](vendor/github.com/google/uuid/LICENSE)
- [github.com/gotd/ige/LICENSE](vendor/github.com/gotd/ige/LICENSE)
- [github.com/gotd/log/LICENSE](vendor/github.com/gotd/log/LICENSE)
- [github.com/gotd/neo/LICENSE](vendor/github.com/gotd/neo/LICENSE)
- [github.com/gotd/td/LICENSE](vendor/github.com/gotd/td/LICENSE)
- [github.com/klauspost/compress/LICENSE](vendor/github.com/klauspost/compress/LICENSE)
- [github.com/klauspost/compress/internal/snapref/LICENSE](vendor/github.com/klauspost/compress/internal/snapref/LICENSE)
- [github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt](vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt)
- [github.com/mattn/go-colorable/LICENSE](vendor/github.com/mattn/go-colorable/LICENSE)
- [github.com/mattn/go-isatty/LICENSE](vendor/github.com/mattn/go-isatty/LICENSE)
- [github.com/mattn/go-sqlite3/LICENSE](vendor/github.com/mattn/go-sqlite3/LICENSE)
- [github.com/ogen-go/ogen/LICENSE](vendor/github.com/ogen-go/ogen/LICENSE)
- [github.com/petermattis/goid/LICENSE](vendor/github.com/petermattis/goid/LICENSE)
- [github.com/refraction-networking/utls/LICENSE](vendor/github.com/refraction-networking/utls/LICENSE)
- [github.com/refraction-networking/utls/dicttls/LICENSE](vendor/github.com/refraction-networking/utls/dicttls/LICENSE)
- [github.com/rs/zerolog/LICENSE](vendor/github.com/rs/zerolog/LICENSE)
- [github.com/segmentio/asm/LICENSE](vendor/github.com/segmentio/asm/LICENSE)
- [github.com/shopspring/decimal/LICENSE](vendor/github.com/shopspring/decimal/LICENSE)
- [github.com/vektah/gqlparser/v2/LICENSE](vendor/github.com/vektah/gqlparser/v2/LICENSE)
- [github.com/yuin/goldmark/LICENSE](vendor/github.com/yuin/goldmark/LICENSE)
- [go.mau.fi/libsignal/LICENSE](vendor/go.mau.fi/libsignal/LICENSE)
- [go.mau.fi/mautrix-gmessages/LICENSE](vendor/go.mau.fi/mautrix-gmessages/LICENSE)
- [go.mau.fi/mautrix-gmessages/LICENSE.exceptions](vendor/go.mau.fi/mautrix-gmessages/LICENSE.exceptions)
- [go.mau.fi/mautrix-meta/LICENSE](vendor/go.mau.fi/mautrix-meta/LICENSE)
- [go.mau.fi/mautrix-meta/LICENSE.exceptions](vendor/go.mau.fi/mautrix-meta/LICENSE.exceptions)
- [go.mau.fi/util/LICENSE](vendor/go.mau.fi/util/LICENSE)
- [go.mau.fi/whatsmeow/LICENSE](vendor/go.mau.fi/whatsmeow/LICENSE)
- [go.opentelemetry.io/otel/LICENSE](vendor/go.opentelemetry.io/otel/LICENSE)
- [go.opentelemetry.io/otel/metric/LICENSE](vendor/go.opentelemetry.io/otel/metric/LICENSE)
- [go.opentelemetry.io/otel/trace/LICENSE](vendor/go.opentelemetry.io/otel/trace/LICENSE)
- [go.uber.org/atomic/LICENSE.txt](vendor/go.uber.org/atomic/LICENSE.txt)
- [go.uber.org/multierr/LICENSE.txt](vendor/go.uber.org/multierr/LICENSE.txt)
- [go.uber.org/zap/LICENSE](vendor/go.uber.org/zap/LICENSE)
- [golang.org/x/crypto/LICENSE](vendor/golang.org/x/crypto/LICENSE)
- [golang.org/x/exp/LICENSE](vendor/golang.org/x/exp/LICENSE)
- [golang.org/x/mod/LICENSE](vendor/golang.org/x/mod/LICENSE)
- [golang.org/x/net/LICENSE](vendor/golang.org/x/net/LICENSE)
- [golang.org/x/sync/LICENSE](vendor/golang.org/x/sync/LICENSE)
- [golang.org/x/sys/LICENSE](vendor/golang.org/x/sys/LICENSE)
- [golang.org/x/text/LICENSE](vendor/golang.org/x/text/LICENSE)
- [golang.org/x/tools/LICENSE](vendor/golang.org/x/tools/LICENSE)
- [google.golang.org/protobuf/LICENSE](vendor/google.golang.org/protobuf/LICENSE)
- [gopkg.in/yaml.v2/LICENSE](vendor/gopkg.in/yaml.v2/LICENSE)
- [gopkg.in/yaml.v2/LICENSE.libyaml](vendor/gopkg.in/yaml.v2/LICENSE.libyaml)
- [gopkg.in/yaml.v2/NOTICE](vendor/gopkg.in/yaml.v2/NOTICE)
- [rsc.io/qr/LICENSE](vendor/rsc.io/qr/LICENSE)
