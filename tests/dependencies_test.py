"""Dependency checks and install consent, without running a package manager."""
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("dependencies", Path(__file__).resolve().parents[1] / "scripts/dependencies.py")
deps = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deps)


class DependenciesTest(unittest.TestCase):
    def test_checks_do_not_install(self):
        with patch.object(deps.shutil, "which", return_value=None), patch.object(deps.subprocess, "run") as run:
            rows = [deps.check(t) for t in deps.TOOLS]
            self.assertTrue(all(not row["installed"] for row in rows))
            run.assert_not_called()

    def test_compiler_alternative(self):
        with patch.object(deps.shutil, "which", side_effect=lambda name: "/bin/clang" if name == "clang" else None):
            self.assertTrue(deps.check(deps.TOOLS[1])["installed"])

    def test_clipboard_requires_copy_and_paste_tools(self):
        entry = next(t for t in deps.TOOLS if t["id"] == "clipboard")
        with patch.object(deps.shutil, "which", side_effect=lambda name: "/bin/wl-copy" if name == "wl-copy" else None):
            self.assertFalse(deps.check(entry)["installed"])

    def test_go_minimum_and_no_toolchain_download(self):
        for version, expected in [("go1.26.4", False), ("go1.27.0", False), ("go1.27.0-X:nodwarf5", False), ("go1.27.1", False),
                                  ("go1.27rc1", False), ("go1.27.2", True), ("go1.27.2-X:nodwarf5", True), ("go1.27.10", True),
                                  ("go1.28.0", True), ("go1.28rc1", False), ("unknown", False)]:
            with self.subTest(version=version), patch.object(deps.shutil, "which", return_value="/bin/go"), patch.object(deps.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, "go version " + version)) as run:
                self.assertEqual(deps.check(deps.TOOLS[0])["installed"], expected)
                self.assertEqual(run.call_args.kwargs["env"]["GOTOOLCHAIN"], "local")

    def test_go_minimum_matches_go_mod(self):
        # The checklist and the compiler must refuse the same old toolchains.
        go_mod = (Path(__file__).resolve().parents[1] / "go.mod").read_text()
        directive = next(line for line in go_mod.splitlines() if line.startswith("go "))
        self.assertEqual(tuple(map(int, directive.split()[1].split("."))), deps.GO_MINIMUM)
        self.assertEqual(deps.GO_MINIMUM, (1, 27, 2))

    def test_unknown_and_noninteractive_refused(self):
        with patch.object(deps.subprocess, "run") as run, patch.object(deps.sys.stdin, "isatty", return_value=False):
            self.assertEqual(deps.install("; touch /tmp/not-allowed"), 2)
            self.assertEqual(deps.install("qrencode"), 2)
            run.assert_not_called()

    def test_signal_cli_uses_fixed_aur_command_and_restarts_shell_only_when_asked(self):
        for answer, restarted in (("y", True), ("", False)):
            with self.subTest(restart=answer):
                checks = iter([{"installed": False}, {"installed": True}])
                with patch.object(deps.sys.stdin, "isatty", return_value=True), \
                     patch.object(deps, "check", side_effect=lambda entry: next(checks)), \
                     patch.object(deps.shutil, "which", return_value="/bin/tool"), \
                     patch("builtins.input", side_effect=["y", answer, ""]), \
                     patch.object(deps.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as run:
                    self.assertEqual(deps.install("signalcli"), 0)
                    self.assertEqual(run.call_args_list[0].args[0], ["yay", "-S", "--needed", "signal-cli"])
                    commands = [call.args[0] for call in run.call_args_list[1:]]
                    self.assertEqual(commands, [["omarchy", "restart", "shell"]] if restarted else [])

    def test_cancel_never_launches(self):
        with patch.object(deps.sys.stdin, "isatty", return_value=True), patch.object(deps, "check", return_value={"installed": False}), patch.object(deps.shutil, "which", return_value="/bin/tool"), patch("builtins.input", return_value=""), patch.object(deps.subprocess, "run") as run:
            self.assertEqual(deps.install("qrencode"), 0)
            run.assert_not_called()

    def test_explicit_install_uses_fixed_argv_and_preserves_failure(self):
        with patch.object(deps.sys.stdin, "isatty", return_value=True), patch.object(deps, "check", return_value={"installed": False}), patch.object(deps.shutil, "which", return_value="/bin/tool"), patch("builtins.input", side_effect=["y", ""]), patch.object(deps.subprocess, "run", return_value=subprocess.CompletedProcess([], 1)) as run:
            self.assertEqual(deps.install("qrencode"), 1)
            run.assert_called_once_with(["sudo", "pacman", "-S", "--needed", "qrencode"], check=False)


if __name__ == "__main__":
    unittest.main()
