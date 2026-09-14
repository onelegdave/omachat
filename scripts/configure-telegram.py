#!/usr/bin/env python3
"""Prompt for Telegram API credentials and store them in OmaChat's private config."""

import getpass
import json
import os
import tempfile
from pathlib import Path


def main() -> None:
    path = Path.home() / ".local/share/omachat/config.json"
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    try:
        data = json.loads(path.read_text()) if path.exists() else {}
    except (OSError, json.JSONDecodeError):
        data = {}

    data["telegramApiID"] = int(input("Telegram api_id: "))
    data["telegramApiHash"] = getpass.getpass("Telegram api_hash: ").strip()

    payload = (json.dumps(data, indent=2) + "\n").encode()
    fd, temporary = tempfile.mkstemp(prefix=".config-", dir=path.parent)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "wb") as handle:
            handle.write(payload)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, path)
        os.chmod(path, 0o600)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    print(f"Saved securely to {path}")


if __name__ == "__main__":
    main()
