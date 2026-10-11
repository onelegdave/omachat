#!/usr/bin/env python3
"""Fake wl-paste for isolated pipe and QML tests; never reads the desktop."""
import json
import os
from pathlib import Path
import sys
import time

settings = json.loads(Path(os.environ["OMACHAT_CLIPBOARD_FIXTURE"]).read_text())
if settings.get("record"):
    with open(settings["record"], "a") as output:
        output.write(json.dumps({"pid": os.getpid(), "argv": sys.argv[1:]}) + "\n")
mode = settings.get("mode", "text")
if mode == "stall":
    time.sleep(30)
elif mode == "trickle":
    while True:
        os.write(1, b"x")
        time.sleep(0.1)
elif mode == "stream":
    while True:
        os.write(1, b"x" * 4096)
elif mode == "close-and-stall":
    os.close(1)
    time.sleep(30)
elif mode == "invalid":
    os.write(1, b"\xff\xfe")
elif mode == "failure":
    os.write(1, b"PRIVATE_CLIPBOARD_FIXTURE")
    os.write(2, b"PRIVATE_PROVIDER_DIAGNOSTIC")
    sys.exit(1)
else:
    time.sleep(settings.get("delay", 0))
    value = settings.get("text", "fixture")
    if "--primary" in sys.argv:
        value = settings.get("primary", value)
    os.write(1, value.encode("utf-8"))
