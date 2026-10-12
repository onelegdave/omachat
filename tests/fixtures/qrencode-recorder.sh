#!/bin/sh
# Stand-in qrencode for the pairing QR fixture: records argv and stdin, then
# exits like qrencode. `cat` only returns once the panel closes stdin.
printf '%s\n' "$@" > "$OMACHAT_QR_RECORD.argv.tmp"
cat > "$OMACHAT_QR_RECORD.stdin.tmp"
mv "$OMACHAT_QR_RECORD.argv.tmp" "$OMACHAT_QR_RECORD.argv"
mv "$OMACHAT_QR_RECORD.stdin.tmp" "$OMACHAT_QR_RECORD.stdin"
