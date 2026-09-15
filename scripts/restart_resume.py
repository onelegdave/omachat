#!/usr/bin/env python3
"""Reopen OmaChat once after an explicitly requested shell restart."""

from __future__ import annotations

import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time


PLUGIN_ID = "onelegdave.omachat"
STATE_NAME = "restart-resume.json"
LOCK_NAME = "restart-resume.lock"
DEFAULT_TTL = 10 * 60


def runtime_root(env: dict[str, str] | None = None) -> Path:
    values = os.environ if env is None else env
    base = values.get("XDG_RUNTIME_DIR", "")
    if not base:
        base = f"/run/user/{os.getuid()}"
    return Path(base) / "omachat"


def process_start(pid: int) -> str:
    try:
        stat_line = Path(f"/proc/{pid}/stat").read_text()
    except (OSError, ValueError):
        return ""
    # The parenthesized process name can contain spaces, so split only after it.
    close = stat_line.rfind(")")
    fields = stat_line[close + 2:].split() if close >= 0 else []
    return fields[19] if len(fields) > 19 else ""


def process_is_same(pid: int, start: str) -> bool:
    return bool(start) and process_start(pid) == start


def write_state(root: Path, state: dict) -> None:
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(root, 0o700)
    fd, temporary = tempfile.mkstemp(prefix=".restart-resume-", dir=root)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w") as handle:
            json.dump(state, handle)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(temporary, root / STATE_NAME)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def read_state(root: Path) -> dict | None:
    try:
        state = json.loads((root / STATE_NAME).read_text())
    except (OSError, ValueError):
        return None
    if not isinstance(state, dict):
        return None
    return state


def remove_state(root: Path, expected: dict | None = None) -> bool:
    path = root / STATE_NAME
    if expected is not None:
        current = read_state(root)
        if current is None:
            return False
        for key, value in expected.items():
            if current.get(key) != value:
                return False
    try:
        path.unlink()
        return True
    except FileNotFoundError:
        return False


def spawn_watcher(root: Path) -> bool:
    # A shell restart kills the shell service's whole cgroup. Put the bounded
    # watcher in its own transient user unit so it can survive that one event.
    try:
        result = subprocess.run([
            "systemd-run", "--user", "--unit=omachat-restart-resume",
            "--collect", "--quiet", sys.executable, str(Path(__file__).resolve()),
            "watch", "--runtime-root", str(root),
        ], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
           stderr=subprocess.DEVNULL, check=False, timeout=3)
        if result.returncode == 0:
            return True
        active = subprocess.run([
            "systemctl", "--user", "is-active", "--quiet",
            "omachat-restart-resume.service",
        ], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
           stderr=subprocess.DEVNULL, check=False, timeout=3)
        return active.returncode == 0
    except (OSError, subprocess.TimeoutExpired):
        return False


def arm(old_pid: int, reason: str, service: str = "", *, root: Path | None = None,
        ttl: int = DEFAULT_TTL, spawn: bool = True, now: float | None = None) -> bool:
    start = process_start(old_pid)
    if not start:
        return False
    target_root = runtime_root() if root is None else root
    current_time = time.time() if now is None else now
    write_state(target_root, {
        "version": 1,
        "oldPid": old_pid,
        "oldStart": start,
        "reason": reason,
        "service": service if service in ("gmessages", "whatsapp", "telegram") else "",
        "expiresAt": current_time + max(1, ttl),
    })
    if spawn and not spawn_watcher(target_root):
        remove_state(target_root, {"oldPid": old_pid, "oldStart": start})
        return False
    return True


def shell_pid() -> int:
    expected = str(Path(os.environ.get("OMARCHY_PATH", "/usr/share/omarchy")) / "shell")
    candidates: list[int] = []
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        try:
            argv = (entry / "cmdline").read_bytes().split(b"\0")
            args = [part.decode(errors="replace") for part in argv if part]
        except OSError:
            continue
        if not args or Path(args[0]).name != "quickshell":
            continue
        for index, value in enumerate(args[:-1]):
            if value == "-p" and args[index + 1] == expected:
                candidates.append(int(entry.name))
                break
    return max(candidates) if candidates else 0


def run_ipc(argv: list[str]) -> tuple[int, str]:
    try:
        result = subprocess.run(argv, text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.STDOUT, timeout=3)
    except (OSError, subprocess.TimeoutExpired):
        return 1, ""
    return result.returncode, result.stdout.strip()


def watch(root: Path, *, now_fn=time.time, same_process_fn=process_is_same,
          run_fn=run_ipc, sleep_fn=time.sleep) -> int:
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    lock_fd = os.open(root / LOCK_NAME, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        try:
            fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return 0

        while True:
            state = read_state(root)
            if state is None:
                return 0
            try:
                old_pid = int(state["oldPid"])
                old_start = str(state["oldStart"])
                expires_at = float(state["expiresAt"])
            except (KeyError, TypeError, ValueError):
                remove_state(root)
                return 1
            if now_fn() >= expires_at:
                remove_state(root, {"oldPid": old_pid, "oldStart": old_start})
                return 0
            if same_process_fn(old_pid, old_start):
                sleep_fn(0.25)
                continue
            break

        while now_fn() < expires_at:
            code, output = run_fn(["omarchy-shell", "shell", "ping"])
            if code == 0 and output == "ok":
                code, output = run_fn([
                    "omarchy-shell", "shell", "summon", PLUGIN_ID, "{}"
                ])
                if code == 0 and output == "ok":
                    service = str(state.get("service", ""))
                    if service in ("gmessages", "whatsapp", "telegram"):
                        run_fn(["omarchy-shell", PLUGIN_ID, "showService", service])
                    remove_state(root, {"oldPid": old_pid, "oldStart": old_start})
                    return 0
            sleep_fn(0.25)

        remove_state(root, {"oldPid": old_pid, "oldStart": old_start})
        return 0
    finally:
        os.close(lock_fd)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="command", required=True)

    arm_parser = sub.add_parser("arm")
    arm_parser.add_argument("--old-pid", type=int, required=True)
    arm_parser.add_argument("--reason", required=True)
    arm_parser.add_argument("--service", default="")

    current_parser = sub.add_parser("arm-current")
    current_parser.add_argument("--reason", required=True)
    current_parser.add_argument("--service", default="")

    cancel_parser = sub.add_parser("cancel")
    cancel_parser.add_argument("--old-pid", type=int, required=True)
    cancel_parser.add_argument("--reason", required=True)

    watch_parser = sub.add_parser("watch")
    watch_parser.add_argument("--runtime-root", type=Path, required=True)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    if args.command == "watch":
        return watch(args.runtime_root)
    if args.command == "cancel":
        remove_state(runtime_root(), {"oldPid": args.old_pid, "reason": args.reason})
        return 0
    if args.command == "arm-current":
        pid = shell_pid()
        return 0 if pid and arm(pid, args.reason, args.service) else 1
    return 0 if arm(args.old_pid, args.reason, args.service) else 1


if __name__ == "__main__":
    sys.exit(main())
