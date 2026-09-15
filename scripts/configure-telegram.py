#!/usr/bin/env python3
"""Prompt for Telegram API credentials and store them in OmaChat's private config."""

import getpass
import json
import os
import tempfile
import fcntl
import stat
import subprocess
import sys
from pathlib import Path


def arm_restart_resume() -> bool:
    script = Path(__file__).with_name("restart_resume.py")
    try:
        result = subprocess.run([
            sys.executable, str(script), "arm-current",
            "--reason", "telegram-setup", "--service", "telegram",
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=4)
    except (OSError, subprocess.TimeoutExpired):
        return False
    return result.returncode == 0


def main() -> int:
    print("Enter the api_id and api_hash from my.telegram.org, not your Telegram login password.")
    try:
        api_id = int(input("Telegram api_id: "))
        if api_id <= 0:
            raise ValueError("must be positive")
    except ValueError:
        print("Error: api_id must be a positive integer.")
        return 1

    print("The api_hash input is hidden: typing or pasting shows no characters or asterisks.")
    print("The prompt will look blank. Enter the full api_hash, then press Enter.")
    api_hash = getpass.getpass("Telegram api_hash: ").strip()
    if len(api_hash) != 32 or not all(c in "0123456789abcdefABCDEF" for c in api_hash):
        print("Error: api_hash must be a 32-character hex string.")
        return 1

    path = Path.home() / ".local/share/omachat/config.json"
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock_path = path.with_name(path.name + ".lock")

    try:
        fd_lock = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    except OSError as e:
        print(f"Error opening lock file {lock_path}: {e}")
        return 1

    try:
        if not stat.S_ISREG(os.fstat(fd_lock).st_mode):
            print("Error: configuration lock must be a regular file.")
            return 1
        os.fchmod(fd_lock, 0o600)
        try:
            fcntl.flock(fd_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print("Another process is currently updating the configuration. Please try again.")
            return 1

        try:
            data = json.loads(path.read_text())
        except FileNotFoundError:
            data = {}
        except (OSError, ValueError):
            print("Error: configuration is corrupted or unreadable. Nothing was changed. Resolve the problem and retry.")
            return 1
        if not isinstance(data, dict):
            print("Error: configuration is not a JSON object. Nothing was changed.")
            return 1

        data["telegramApiID"] = api_id
        data["telegramApiHash"] = api_hash

        payload = (json.dumps(data, indent=2) + "\n").encode()
        fd, temporary = tempfile.mkstemp(prefix=".config-", dir=path.parent)
        try:
            os.fchmod(fd, 0o600)
            with os.fdopen(fd, "wb") as handle:
                handle.write(payload)
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    finally:
        os.close(fd_lock)

    resume_armed = arm_restart_resume()
    print(f"Saved securely to {path}")
    print("Next, run: omarchy restart shell")
    print("This briefly reloads the whole shell and loads the saved credentials.")
    if resume_armed:
        print("OmaChat will reopen to Telegram once the new shell is ready.")
    else:
        print("Open OmaChat again after the restart.")
    print("Then select Telegram > Pair with Telegram.")
    print("On your phone: Telegram > Settings > Devices > Link Desktop Device, then scan the QR code.")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (EOFError, KeyboardInterrupt):
        print("\nCancelled. Credentials were not saved.")
        sys.exit(130)
    except OSError:
        print("Could not save configuration. Check permissions and available storage, then retry.")
        sys.exit(1)
