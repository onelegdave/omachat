#!/usr/bin/env python3
"""Measure how much clipboard data each field type reads when the clipboard changes.

Through Qt 6.12.0 an editable TextInput reads the whole clipboard on every
change (QTBUG-149610), with no paste action. This opens a short-lived window
per field type on the real Wayland desktop, offers 8 MB of text, and counts the
bytes the shell process reads. The native TextField is the control: it shows
the probe can see the read. It replaces the clipboard, then restores plain text
or clears it. Run it manually; it is not part of `make test`.
"""
import os
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

repo = Path(__file__).resolve().parents[1]
omarchy = Path(os.environ.get("OMARCHY_PATH", "/usr/share/omarchy"))
LIMIT = 64 * 1024
FIELDS = {
    "native TextField (control)": 'Controls.TextField { id: field; width: 300; text: "probe" }',
    "SafeTextField": 'Chat.SafeTextField { id: field; width: 300; text: "probe" }',
    "SafePasswordField": 'Chat.SafePasswordField { id: field; width: 300; text: "probe" }',
}
if not os.environ.get("WAYLAND_DISPLAY") or not all(map(shutil.which, ("qs", "wl-copy", "wl-paste"))):
    raise SystemExit("Needs a Wayland desktop with quickshell and wl-clipboard.")


def read_bytes(pid):
    for line in (Path("/proc") / str(pid) / "io").read_text().splitlines():
        if line.startswith("rchar:"):
            return int(line.split()[1])
    raise RuntimeError("rchar missing")


def wait_for_focus(pid, timeout=8):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        time.sleep(0.5)
        if not shutil.which("hyprctl"):
            time.sleep(2)
            return
        active = subprocess.run(["hyprctl", "activewindow", "-j"], capture_output=True, text=True, timeout=3)
        if f'"pid": {pid},' in active.stdout:
            time.sleep(0.5)
            return
    raise SystemExit("The probe window did not get keyboard focus; retry without switching windows.")


saved = subprocess.run(["wl-paste", "--no-newline", "--type", "text"], capture_output=True, timeout=3)
results = {}
with tempfile.TemporaryDirectory(prefix="omachat-probe-") as folder:
    config = Path(folder)
    for module in ("Ui", "Commons"):
        (config / module).symlink_to(omarchy / "shell" / module)
    (config / "Chat").symlink_to(repo)
    big = config / "big.txt"
    big.write_bytes(b"x" * 8_000_000)
    try:
        for name, field in FIELDS.items():
            (config / "shell.qml").write_text(
                'import QtQuick\nimport QtQuick.Controls as Controls\nimport Quickshell\nimport "Chat" as Chat\n'
                'ShellRoot { FloatingWindow { implicitWidth: 340; implicitHeight: 60; visible: true; title: "omachat-clipboard-probe"\n'
                + field + '\nComponent.onCompleted: field.forceActiveFocus() } }\n')
            shell = subprocess.Popen(["qs", "-p", str(config)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                # Wayland only sends the selection to the focused client.
                wait_for_focus(shell.pid)
                subprocess.run(["wl-copy", "small"], check=True, timeout=3)
                time.sleep(1)
                before = read_bytes(shell.pid)
                with big.open("rb") as data:
                    subprocess.run(["wl-copy"], stdin=data, check=True, timeout=10)
                time.sleep(3)
                results[name] = read_bytes(shell.pid) - before
            finally:
                shell.terminate()
                shell.wait(timeout=5)
    finally:
        if saved.returncode == 0 and saved.stdout:
            subprocess.run(["wl-copy"], input=saved.stdout, timeout=3)
        else:
            subprocess.run(["wl-copy", "--clear"], timeout=3)

for name, count in results.items():
    print(f"{name:28} {count:>10,} bytes read after an 8 MB clipboard change")
control = results["native TextField (control)"]
safe = [results["SafeTextField"], results["SafePasswordField"]]
if control < 4_000_000:
    sys.exit("Inconclusive: the native control did not read the offer. Keep the probe window focused and retry.")
if max(safe) > LIMIT:
    sys.exit("FAIL: a guarded field read clipboard data without a paste.")
print("PASS: guarded fields read no clipboard payload on clipboard changes.")
