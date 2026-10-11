"""Real subprocess/pipe checks, using invented providers without clipboard access."""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest

REPO = Path(__file__).resolve().parents[1]


class ClipboardTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="omachat-clipboard-test-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        launcher = self.directory / "wl-paste"
        launcher.write_text("#!" + sys.executable + "\n" +
                            (REPO / "tests/fixtures/clipboard-provider.py").read_text())
        launcher.chmod(0o700)
        self.settings = self.directory / "provider.json"
        self.record = self.directory / "calls.jsonl"
        self.env = {**os.environ, "PATH": str(self.directory),
                    "OMACHAT_CLIPBOARD_FIXTURE": str(self.settings)}

    def run_reader(self, use_primary=False, **settings):
        self.settings.write_text(json.dumps({**settings, "record": str(self.record)}))
        started = time.monotonic()
        result = subprocess.run([sys.executable, str(REPO / "scripts/clipboard.py"),
                                 *(["--primary"] if use_primary else [])],
                                env=self.env, capture_output=True, timeout=4)
        self.assertLess(time.monotonic() - started, 2.5)
        self.assertEqual(result.stderr, b"")
        reply = json.loads(result.stdout)
        self.assertLess(len(result.stdout), 6 * 65536 + 100)
        self.assertEqual(result.returncode, 0 if reply["ok"] else 1)
        return reply

    def assert_provider_stopped(self):
        for line in self.record.read_text().splitlines():
            pid = json.loads(line)["pid"]
            with self.assertRaises(ProcessLookupError):
                os.kill(pid, 0)

    def test_unicode_and_primary_with_fixed_argv(self):
        value = "Invented text 😄\n'; $(false)"
        self.assertEqual(self.run_reader(text=value), {"ok": True, "text": value})
        self.assertEqual(self.run_reader(use_primary=True, text="ordinary", primary="selection"),
                         {"ok": True, "text": "selection"})
        calls = [json.loads(line)["argv"] for line in self.record.read_text().splitlines()]
        self.assertEqual(calls, [["--no-newline", "--type", "text"],
                                 ["--no-newline", "--type", "text", "--primary"]])

    def test_exact_byte_limit_and_oversize_rejection(self):
        value = "😄" * 16384
        self.assertEqual(self.run_reader(text=value)["text"], value)
        reply = self.run_reader(text=value + "x")
        self.assertFalse(reply["ok"])
        self.assertNotIn("text", reply)
        self.assertIn("too large", reply["error"])

    def test_infinite_stream_is_killed(self):
        self.assertIn("too large", self.run_reader(mode="stream")["error"])
        self.assert_provider_stopped()

    def test_absolute_deadline_for_stall_trickle_and_closed_pipe(self):
        for mode in ("stall", "trickle", "close-and-stall"):
            with self.subTest(mode=mode):
                reply = self.run_reader(mode=mode)
                self.assertFalse(reply["ok"])
                self.assertNotIn("text", reply)
                self.assertIn("timed out", reply["error"])
                self.assert_provider_stopped()

    def test_failure_and_invalid_utf8_do_not_leak_partial_data(self):
        for mode in ("failure", "invalid"):
            reply = self.run_reader(mode=mode)
            self.assertFalse(reply["ok"])
            self.assertNotIn("text", reply)
            self.assertNotIn("PRIVATE", json.dumps(reply))

    def test_missing_tool_fails_closed(self):
        self.settings.write_text("{}")
        (self.directory / "wl-paste").unlink()
        self.assertIn("wl-paste", self.run_reader()["error"])

    def test_cancellation_kills_provider(self):
        self.settings.write_text(json.dumps({"mode": "stall", "record": str(self.record)}))
        process = subprocess.Popen([sys.executable, str(REPO / "scripts/clipboard.py")],
                                   env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 2
            while not self.record.exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue(self.record.exists())
            process.send_signal(signal.SIGTERM)
            out, error = process.communicate(timeout=1)
            self.assertEqual(error, b"")
            self.assertFalse(json.loads(out)["ok"])
            self.assert_provider_stopped()
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()

    def test_forced_reader_death_stops_provider(self):
        self.settings.write_text(json.dumps({"mode": "stall", "record": str(self.record)}))
        process = subprocess.Popen([sys.executable, str(REPO / "scripts/clipboard.py")],
                                   env=self.env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            deadline = time.monotonic() + 2
            while not self.record.exists() and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertTrue(self.record.exists())
            pid = json.loads(self.record.read_text().splitlines()[0])["pid"]
            process.kill()
            process.wait(timeout=1)
            deadline = time.monotonic() + 1
            while time.monotonic() < deadline:
                stat = Path("/proc") / str(pid) / "stat"
                if not stat.exists() or stat.read_text().split(") ", 1)[1].startswith("Z "):
                    break
                time.sleep(0.01)
            else:
                self.fail("Clipboard provider survived forced reader death")
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()

    def test_all_product_inputs_use_guarded_component(self):
        import re
        for path in REPO.glob("*.qml"):
            if path.name == "SafeTextField.qml":
                continue
            self.assertIsNone(re.search(r"\b(?:TextField|TextInput|TextArea|TextEdit)\s*\{", path.read_text()), path.name)


if __name__ == "__main__":
    unittest.main()
