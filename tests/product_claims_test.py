"""Keep high-risk public capability and credit claims aligned with the app."""

import json
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]


class ProductClaimsTest(unittest.TestCase):
    def test_reactions_are_documented_consistently(self):
        readme = (ROOT / "README.md").read_text()
        settings = (ROOT / "SettingsView.qml").read_text()
        self.assertIn("| Reactions | Yes | Yes | Yes (chat-dependent) | Yes | Yes |", readme)
        self.assertIn("standard emoji reactions (when allowed by the chat)", settings)

    def test_visible_credits_include_every_protocol_client(self):
        settings = (ROOT / "SettingsView.qml").read_text()
        for project in ("libgm", "whatsmeow", "gotd/td", "mautrix-meta", "signal-cli"):
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

    def test_sign_in_diagnostics_claims_match_the_code(self):
        settings = (ROOT / "SettingsView.qml").read_text()
        security = (ROOT / "SECURITY.md").read_text()
        guide = (ROOT / "docs/services/google-messages.md").read_text()
        config = (ROOT / "internal/store/config.go").read_text()
        signin = (ROOT / "internal/browser/signin.go").read_text()
        authlog = (ROOT / "internal/daemon/authlog.go").read_text()
        # Off by default, everywhere it is described.
        self.assertIn('DebugLogging bool `json:"debugLogging,omitempty"`', config)
        self.assertIn("property bool debugLogging: false", settings)
        self.assertIn("it's off unless you turn it on", settings)
        self.assertIn("is off by default", security)
        # The browser check is keyless, read-only and in place, as the text says.
        self.assertIn("It doesn't unlock your keyring, doesn't decrypt anything", settings)
        self.assertIn("never runs `secret-tool`, never decrypts", security)
        self.assertNotIn("secret-tool", signin.replace("it never runs secret-tool", ""))
        self.assertNotIn("decrypt", signin.replace("never decrypts", "").replace("never decrypted", ""))
        self.assertIn('"?mode=ro&immutable=1"', signin)
        # Size and location claims match the implementation.
        self.assertIn("authLogMaxBytes  = 1 << 20", authlog)
        self.assertIn("about 2 MB", settings)
        self.assertIn("about 2 MB", security)
        self.assertIn("authLogBodyCap   = 64 << 10", authlog)
        self.assertIn("up to 64 KiB", security)
        for text in (security, guide):
            self.assertIn("~/.local/state/omachat/auth.log", text)


if __name__ == "__main__":
    unittest.main()
