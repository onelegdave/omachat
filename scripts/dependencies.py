#!/usr/bin/env python3
"""Read-only tool checks; installation requires an explicit interactive choice."""
import argparse
import json
import os
import re
import shutil
import subprocess
import sys


def tool(key, name, purpose, commands, package):
    return dict(id=key, name=name, purpose=purpose, commands=commands,
                packages=[package],
                sourceUrl="https://gitlab.archlinux.org/archlinux/packaging/packages/" + package)


TOOLS = [
    tool("go", "Go 1.27+", "Build the shared helper for all services.", ["go"], "go"),
    tool("compiler", "C compiler", "Build shared SQLite support. Either gcc or clang is sufficient.", ["gcc", "clang"], "gcc"),
    tool("qrencode", "QR encoder", "Pair WhatsApp, Telegram, or use Google's legacy QR fallback.", ["qrencode"], "qrencode"),
    tool("ffmpeg", "FFmpeg and FFplay", "Camera capture, WhatsApp GIF conversion, and Google/Telegram voice recording and playback.", ["ffmpeg", "ffplay"], "ffmpeg"),
    tool("sqlite", "SQLite CLI", "Read the selected Google browser profile for pairing.", ["sqlite3"], "sqlite"),
    tool("libsecret", "Secret Service tool", "Google browser pairing; also needs an unlocked desktop keyring.", ["secret-tool"], "libsecret"),
    tool("clipboard", "Wayland clipboard", "Copy message text.", ["wl-copy"], "wl-clipboard"),
    tool("xdg", "Desktop link opener", "Open links, source pages, and downloaded files.", ["xdg-open"], "xdg-utils"),
    tool("python", "Python 3", "Build and verify the helper, check releases, and run setup tools.", ["python3"], "python"),
]


def check(entry):
    result = {k: v for k, v in entry.items() if k != "commands"}
    present = [name for name in entry["commands"] if shutil.which(name)]
    installed = bool(present) if entry["id"] == "compiler" else len(present) == len(entry["commands"])
    detail = "Available: " + ", ".join(present) if installed else "Missing: " + ", ".join(n for n in entry["commands"] if n not in present)
    if entry["id"] == "go" and installed:
        try:
            proc = subprocess.run(["go", "version"], capture_output=True, text=True, timeout=5,
                                  env={**os.environ, "GOTOOLCHAIN": "local"})
            match = re.search(r"\bgo(\d+)\.(\d+)(?:\.(\d+))?\b", proc.stdout)
            installed = proc.returncode == 0 and match is not None and tuple(map(int, match.groups(default="0"))) >= (1, 27, 0)
            detail = proc.stdout.strip() if installed else "Go 1.27+ required; installed version is older or could not be verified."
        except (OSError, subprocess.TimeoutExpired):
            installed, detail = False, "Could not verify Go version."
    result.update(installed=bool(installed), detail=detail)
    return result


def install(key):
    entry = next((t for t in TOOLS if t["id"] == key), None)
    if entry is None:
        print("Unknown dependency. No command was run.", file=sys.stderr)
        return 2
    if not sys.stdin.isatty():
        print("Installation requires an interactive terminal. No command was run.", file=sys.stderr)
        return 2
    if check(entry)["installed"]:
        print("Already available. Nothing to install.")
        return 0
    if not shutil.which("pacman") or not shutil.which("sudo"):
        print("This action requires Arch's pacman and sudo. Install manually using your system's package manager.")
        return 2
    command = ["sudo", "pacman", "-S", "--needed", *entry["packages"]]
    print(entry["name"] + ": " + entry["purpose"])
    print("Review packaging source: " + entry["sourceUrl"])
    print("Command: " + " ".join(command))
    print("Pacman will show the transaction and ask for confirmation. Nothing is installed unless you agree.")
    print("This does not refresh package databases or upgrade your system. If your system needs an update, cancel and use your normal Omarchy update workflow.")
    try:
        if input("Continue to package manager? [y/N] ").strip().lower() != "y":
            print("Cancelled. Nothing installed.")
            return 0
        result = subprocess.run(command, check=False).returncode
        print("Tool available." if result == 0 and check(entry)["installed"] else "Tool not confirmed available. Review the output above; no success is assumed.")
        input("Return to OmaChat and choose Recheck. Press Enter to close. ")
        return result
    except (EOFError, KeyboardInterrupt):
        print("\nCancelled.")
        return 130
    except OSError as error:
        print("Could not start package manager: " + str(error), file=sys.stderr)
        return 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    action = parser.add_mutually_exclusive_group(required=True)
    action.add_argument("--check", action="store_true")
    action.add_argument("--install", metavar="DEPENDENCY_ID")
    args = parser.parse_args()
    if args.check:
        print(json.dumps([check(t) for t in TOOLS]))
        return 0
    return install(args.install)


if __name__ == "__main__":
    sys.exit(main())
