"""Verify setup guidance and private storage using synthetic credentials only."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("telegram_setup", Path(__file__).resolve().parents[1] / "scripts/configure-telegram.py")
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)


class TelegramSetupTest(unittest.TestCase):
    def test_invalid_configuration_is_never_replaced(self):
        for content in ("", "null", "[]", "{", "   "):
            with self.subTest(content=content), tempfile.TemporaryDirectory() as directory:
                home = Path(directory)
                path = home / ".local/share/omachat/config.json"
                path.parent.mkdir(parents=True)
                path.write_text(content)
                with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="123"), patch.object(setup.getpass, "getpass", return_value="a" * 32), contextlib.redirect_stdout(io.StringIO()):
                    self.assertEqual(setup.main(), 1)
                self.assertEqual(path.read_text(), content)

    def test_permission_error_does_not_replace_configuration(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            path = home / ".local/share/omachat/config.json"
            path.parent.mkdir(parents=True)
            path.write_text('{"enabledServices":[]}')
            with patch.object(setup.Path, "home", return_value=home), patch.object(setup.Path, "read_text", side_effect=PermissionError), patch("builtins.input", return_value="123"), patch.object(setup.getpass, "getpass", return_value="a" * 32), patch.object(setup.os, "replace") as replace, contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(setup.main(), 1)
                replace.assert_not_called()
            self.assertEqual(path.read_text(), '{"enabledServices":[]}')

    def test_hidden_input_guidance_and_next_steps(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            path = home / ".local/share/omachat/config.json"
            path.parent.mkdir(parents=True)
            path.write_text(json.dumps({"enabledServices": ["telegram"]}))
            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="12345"), patch.object(setup.getpass, "getpass", return_value="a" * 32), patch.object(setup, "arm_restart_resume", return_value=True) as arm, contextlib.redirect_stdout(output):
                setup.main()
            self.assertIn("no characters or asterisks", output.getvalue())
            self.assertIn("omarchy restart shell", output.getvalue())
            self.assertIn("Pair with Telegram", output.getvalue())
            self.assertIn("will reopen to Telegram", output.getvalue())
            arm.assert_called_once_with()
            self.assertNotIn("a" * 32, output.getvalue())
            self.assertEqual(json.loads(path.read_text())["enabledServices"], ["telegram"])
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_invalid_input(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="invalid"), contextlib.redirect_stdout(output):
                setup.main()
            self.assertIn("must be a positive integer", output.getvalue())

            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="123"), patch.object(setup.getpass, "getpass", return_value="short"), contextlib.redirect_stdout(output):
                setup.main()
            self.assertIn("32-character hex string", output.getvalue())

    def test_read_parse_failure_preserving_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            path = home / ".local/share/omachat/config.json"
            path.parent.mkdir(parents=True)
            path.write_bytes(b"invalid \x80 json")
            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="123"), patch.object(setup.getpass, "getpass", return_value="a"*32), contextlib.redirect_stdout(output):
                setup.main()
            self.assertIn("corrupted or unreadable", output.getvalue())
            self.assertEqual(path.read_bytes(), b"invalid \x80 json")

    def test_no_opt_out_loss(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            path = home / ".local/share/omachat/config.json"
            path.parent.mkdir(parents=True)
            path.write_text('{"enabledServices": []}')
            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="123"), patch.object(setup.getpass, "getpass", return_value="a"*32), patch.object(setup, "arm_restart_resume", return_value=True), contextlib.redirect_stdout(output):
                setup.main()
            self.assertEqual(json.loads(path.read_text())["enabledServices"], [])
