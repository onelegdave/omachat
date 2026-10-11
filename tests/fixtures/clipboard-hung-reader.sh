#!/bin/bash
# Fake python3 for the paste-watchdog fixture: hangs past the QML watchdog and
# never reads a clipboard. "graceful" mimics the real reader's SIGTERM cleanup.
record="$OMACHAT_CLIPBOARD_HANG_RECORD"
echo "start $$" >> "$record"
if [[ $(cat "$OMACHAT_CLIPBOARD_HANG_MODE") == ignore ]]; then
  trap '' TERM
else
  trap 'sleep 0.2; echo "cleaned $$" >> "$record"; exit 1' TERM
fi
while :; do sleep 0.05; done
