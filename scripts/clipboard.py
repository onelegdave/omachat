#!/usr/bin/env python3
"""Read Wayland text outside the shell, with a total byte limit and deadline.

Only a complete, successful UTF-8 transfer is returned. Clipboard contents and
provider diagnostics are never logged, written to disk, or passed as arguments.
"""
import argparse
import ctypes
import json
import os
import resource
import selectors
import signal
import subprocess
import sys
import time

MAX_BYTES = 64 * 1024
TIMEOUT_SECONDS = 1.5


class ClipboardError(Exception):
    pass


def child_limits(parent):
    # Linux parent-death signal also covers a shell crash or forced termination
    # of this reader, where Python's normal finally cleanup cannot run.
    if ctypes.CDLL(None, use_errno=True).prctl(1, signal.SIGKILL, 0, 0, 0) != 0:
        raise OSError("Could not supervise clipboard reader")
    if parent != os.getppid():
        os.kill(os.getpid(), signal.SIGKILL)
    resource.setrlimit(resource.RLIMIT_AS, (128 * 1024 * 1024,) * 2)
    resource.setrlimit(resource.RLIMIT_CPU, (2, 2))
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))


def read_clipboard(primary=False):
    deadline = time.monotonic() + TIMEOUT_SECONDS
    command = ["wl-paste", "--no-newline", "--type", "text"]
    if primary:
        command.append("--primary")
    try:
        parent = os.getpid()
        child = subprocess.Popen(command, stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                 start_new_session=True, preexec_fn=lambda: child_limits(parent))
    except (OSError, subprocess.SubprocessError):
        raise ClipboardError("Paste needs wl-paste from wl-clipboard.") from None
    try:
        data = bytearray()
        os.set_blocking(child.stdout.fileno(), False)
        with selectors.DefaultSelector() as selector:
            selector.register(child.stdout, selectors.EVENT_READ)
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not selector.select(remaining):
                    raise ClipboardError("Paste timed out. Try copying the text again.")
                chunk = os.read(child.stdout.fileno(), min(4096, MAX_BYTES + 1 - len(data)))
                if not chunk:
                    break
                data.extend(chunk)
                if len(data) > MAX_BYTES:
                    raise ClipboardError("Paste is too large (maximum 64 KiB).")
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ClipboardError("Paste timed out. Try copying the text again.")
        try:
            code = child.wait(timeout=remaining)
        except subprocess.TimeoutExpired:
            raise ClipboardError("Paste timed out. Try copying the text again.") from None
        if code != 0:
            raise ClipboardError("Clipboard text is unavailable. Try copying it again.")
        try:
            return data.decode("utf-8", errors="strict")
        except UnicodeDecodeError:
            raise ClipboardError("Clipboard text is not valid UTF-8.") from None
    finally:
        # Kill the process group even if the pipe closed or the leader exited:
        # descendants must not outlive a failed or cancelled paste request.
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.stdout.close()
        child.wait()


def cancelled(signum, frame):
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    raise ClipboardError("Paste cancelled.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--primary", action="store_true")
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, cancelled)
    signal.signal(signal.SIGINT, cancelled)
    try:
        result = {"ok": True, "text": read_clipboard(args.primary)}
    except (ClipboardError, OSError) as error:
        result = {"ok": False, "error": str(error) if isinstance(error, ClipboardError)
                  else "Clipboard text could not be read."}
    # JSON expands at most six times the capped bytes, plus a fixed envelope.
    print(json.dumps(result, ensure_ascii=True))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
