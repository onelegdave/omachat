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
    def test_hidden_input_guidance_and_next_steps(self):
        with tempfile.TemporaryDirectory() as directory:
            home = Path(directory)
            path = home / ".local/share/omachat/config.json"
            path.parent.mkdir(parents=True)
            path.write_text(json.dumps({"enabledServices": ["telegram"]}))
            output = io.StringIO()
            with patch.object(setup.Path, "home", return_value=home), patch("builtins.input", return_value="12345"), patch.object(setup.getpass, "getpass", return_value="a" * 32), contextlib.redirect_stdout(output):
                setup.main()
            self.assertIn("no characters or asterisks", output.getvalue())
            self.assertIn("omarchy restart shell", output.getvalue())
            self.assertIn("Pair with Telegram", output.getvalue())
            self.assertNotIn("a" * 32, output.getvalue())
            self.assertEqual(json.loads(path.read_text())["enabledServices"], ["telegram"])
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
