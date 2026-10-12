# Attachment download limits

The v0.5.3 candidate corrects the attachment limit bypass reported by
[HANCORE-linux](https://github.com/omacom/omarchy-plugin-marketplace/issues/10861#issuecomment-6122021629).
WhatsApp and encrypted Messenger previously embedded `*os.File` in their capped
writers. Its promoted `ReadFrom` let the vendored downloader's `io.Copy` bypass
the custom `Write` methods. Hash checks happened after the oversized file had
already reached disk. Metadata checks and request timeouts did not bound bytes.

Both now pass [boundedfile.File](../internal/boundedfile/file.go) to the
unchanged vendored downloader. The wrapper owns a private file reference and
exposes only the required file interface. It has no `ReadFrom`, `WriteString`,
or other promoted write shortcut. Destination copy loops and source `WriterTo`
implementations therefore reach the checked `Write` method.

Writes, positional writes, seeking, and truncation cannot grow the file past
25 MiB. Subtraction-based checks reject negative and overflowing offsets before
touching the file. The limit tracks file extent instead of cumulative bytes,
so retries can overwrite partially downloaded content and decryption can write
back in place. Hash verification and decryption remain in the vendored client.
Failures leave no published file and the backend removes its temporary file.
Cached files already larger than the cap are refused without deleting them.

Messenger profile images use the same file wrapper with their existing 5 MiB
limit, in addition to the HTTP reader limit. Google Messages and WhatsApp
profile downloads already use capped readers; Telegram streams through a
writer that does not embed a file. Shared media cache pruning remains unchanged.

## Offline verification

```sh
GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local go test -mod=vendor ./internal/boundedfile ./internal/whatsapp ./internal/messenger
```

The tests execute the vendored download, encrypted-hash, HMAC, decryption,
plaintext-hash, retry, and cancellation code with a fixture HTTP transport.
There are no live-provider calls or real credentials. Cases include missing or
understated length metadata, an oversized body, a continuous stream, exact-limit
encrypted media across multiple decryption blocks, invalid hashes, and partial
download retries. They check file size on disk, not only returned errors.

Separate checks cover copy optimization routes, string writing, negative and
overflowing offsets, sparse-file growth, and truncation. Backend tests check
failure cleanup and refusal of oversized old cache entries. A controlled
negative check restoring unbounded `ReadFrom` makes the new copy and vendored
download regressions fail. Fixture streams are themselves bounded or cancellable
so a deliberately regressed implementation cannot fill the test machine's disk.

This patch changes download enforcement and release metadata. The existing
clipboard editors, keyboard behavior, unified inbox, history, notification,
profile identity, build/update workflow, and protocol implementations are
preserved. Their existing suites remain part of release validation. Offline
fixtures do not establish live-provider acceptance or Marketplace approval.
