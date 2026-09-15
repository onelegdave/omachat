"""Focused tests for the exactly-once shell restart handoff."""

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "restart_resume", Path(__file__).resolve().parents[1] / "scripts/restart_resume.py"
)
resume = importlib.util.module_from_spec(spec)
spec.loader.exec_module(resume)


class RestartResumeTest(unittest.TestCase):
    def test_arm_is_private_short_lived_and_spawns_watcher(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / "omachat"
            with patch.object(resume, "process_start", return_value="start-token"), \
                    patch.object(resume, "spawn_watcher") as spawn:
                self.assertTrue(resume.arm(
                    123, "missing-service", root=root, ttl=90, now=1000
                ))
            state = json.loads((root / resume.STATE_NAME).read_text())
            self.assertEqual(state["oldPid"], 123)
            self.assertEqual(state["oldStart"], "start-token")
            self.assertEqual(state["reason"], "missing-service")
            self.assertEqual(state["expiresAt"], 1090)
            self.assertEqual((root / resume.STATE_NAME).stat().st_mode & 0o777, 0o600)
            self.assertEqual(root.stat().st_mode & 0o777, 0o700)
            spawn.assert_called_once_with(root)

    def test_watcher_waits_for_old_shell_then_delivers_once(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            resume.write_state(root, {
                "version": 1,
                "oldPid": 123,
                "oldStart": "old-start",
                "reason": "telegram-setup",
                "service": "telegram",
                "expiresAt": 2000,
            })
            alive = iter([True, False])
            calls = []
            summon_attempts = 0

            def run(argv):
                nonlocal summon_attempts
                calls.append(argv)
                if argv[1:3] == ["shell", "ping"]:
                    return 0, "ok"
                if argv[1:3] == ["shell", "summon"]:
                    summon_attempts += 1
                    if summon_attempts == 1:
                        return 0, "unknown"
                    return 0, "ok"
                return 0, ""

            self.assertEqual(resume.watch(
                root,
                now_fn=lambda: 1000,
                same_process_fn=lambda _pid, _start: next(alive),
                run_fn=run,
                sleep_fn=lambda _seconds: None,
            ), 0)
            self.assertEqual(calls, [
                ["omarchy-shell", "shell", "ping"],
                ["omarchy-shell", "shell", "summon", resume.PLUGIN_ID, "{}"],
                ["omarchy-shell", "shell", "ping"],
                ["omarchy-shell", "shell", "summon", resume.PLUGIN_ID, "{}"],
                ["omarchy-shell", resume.PLUGIN_ID, "showService", "telegram"],
            ])
            self.assertFalse((root / resume.STATE_NAME).exists())

            self.assertEqual(resume.watch(
                root, now_fn=lambda: 1000,
                same_process_fn=lambda _pid, _start: False,
                run_fn=run, sleep_fn=lambda _seconds: None,
            ), 0)
            self.assertEqual(len(calls), 5)

    def test_expired_request_never_summons(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            resume.write_state(root, {
                "oldPid": 123,
                "oldStart": "old-start",
                "reason": "missing-service",
                "service": "",
                "expiresAt": 999,
            })
            calls = []
            self.assertEqual(resume.watch(
                root, now_fn=lambda: 1000,
                same_process_fn=lambda _pid, _start: False,
                run_fn=lambda argv: calls.append(argv),
                sleep_fn=lambda _seconds: None,
            ), 0)
            self.assertEqual(calls, [])
            self.assertFalse((root / resume.STATE_NAME).exists())

    def test_cancel_only_removes_its_own_setup_reason(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            resume.write_state(root, {
                "oldPid": 123,
                "oldStart": "old-start",
                "reason": "telegram-setup",
                "expiresAt": 2000,
            })
            self.assertFalse(resume.remove_state(
                root, {"oldPid": 123, "reason": "missing-service"}
            ))
            self.assertTrue((root / resume.STATE_NAME).exists())
            self.assertTrue(resume.remove_state(
                root, {"oldPid": 123, "reason": "telegram-setup"}
            ))


if __name__ == "__main__":
    unittest.main()
