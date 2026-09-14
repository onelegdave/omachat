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

    print("Enter the api_id and api_hash from my.telegram.org, not your Telegram login password.")
    data["telegramApiID"] = int(input("Telegram api_id: "))
    print("The api_hash input is hidden: typing or pasting shows no characters or asterisks.")
    print("The prompt will look blank. Enter the full api_hash, then press Enter.")
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
    print("Next, run: omarchy restart shell")
    print("This briefly reloads the whole shell and loads the saved credentials.")
    print("Then open OmaChat > Telegram > Pair with Telegram.")
    print("On your phone: Telegram > Settings > Devices > Link Desktop Device, then scan the QR code.")


if __name__ == "__main__":
    main()
