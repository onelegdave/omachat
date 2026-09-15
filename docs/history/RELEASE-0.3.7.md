# OmaChat v0.3.7

- Show Telegram credential setup instructions once, while keeping distinct
  configuration errors visible.
- Keep Google Messages pairing active when a premature signed-out event arrives
  before confirmation. Genuine sign-outs after pairing still clear the session.
- Backport the upstream Google pairing request update for error 3/32
  (`CLIENT_ATTESTATION_MISSING`), with upstream attribution in NOTICE.
- Add regression tests for Telegram guidance, Google pairing lifecycle, and the
  updated protobuf request. Include the vendored pairing tests in `make test`.

The owner confirmed successful Google pairing and that the previous photo
flicker was gone on oldsmaru with the pairing hotfixes installed. That live
confirmation is separate from automated regression coverage.

No dependencies are installed automatically. Google Messages integration uses
an unofficial API and can require further updates when Google changes it.

Publishing this release is not marketplace submission or approval. Submission
remains on hold pending the owner's testing and explicit approval.

Verification: the uncached Go race suite, repeated vendored pairing tests, Go
vet, JavaScript and Python tests, real-QML UI/pagination/readability/photo/build/
reconnection checks, and local plugin validation passed. QML checks cover both
duplicate guidance suppression and preservation of distinct Telegram errors.
These checks do not guarantee security or marketplace approval.
