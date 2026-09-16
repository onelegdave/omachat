"""Keep high-risk public capability and credit claims aligned with the app."""

import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class ProductClaimsTest(unittest.TestCase):
    def test_telegram_reactions_are_documented_consistently(self):
        readme = (ROOT / "README.md").read_text()
        settings = (ROOT / "SettingsView.qml").read_text()
        self.assertIn("| Reactions | Yes | Yes | Yes (chat-dependent) | Yes |", readme)
        self.assertIn("standard emoji reactions (when allowed by the chat)", settings)

    def test_visible_credits_include_every_protocol_client(self):
        settings = (ROOT / "SettingsView.qml").read_text()
        for project in ("libgm", "whatsmeow", "gotd/td", "mautrix-meta"):
            self.assertIn(project, settings)

    def test_settings_do_not_advertise_unimplemented_webcam_capture(self):
        manifest = json.loads((ROOT / "manifest.json").read_text())
        defaults = manifest["barWidget"]["defaults"]
        keys = {row["key"] for row in manifest["barWidget"]["schema"]}
        self.assertNotIn("cameraDevice", defaults)
        self.assertNotIn("cameraDevice", keys)
        self.assertNotIn("Webcam photo capture", (ROOT / "docs/dependencies.md").read_text())

    def test_telegram_credential_copy_describes_real_network_use(self):
        settings = (ROOT / "SettingsView.qml").read_text()
        self.assertIn("uses them only to connect directly to Telegram", settings)
        self.assertNotIn("never transmitted elsewhere", settings)


if __name__ == "__main__":
    unittest.main()
