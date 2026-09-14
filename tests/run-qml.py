#!/usr/bin/env python3
"""Exercise real QML with a short-lived demo window and a mock service."""
import os
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[1]
omarchy = Path(os.environ.get("OMARCHY_PATH", "/usr/share/omarchy"))
if not os.environ.get("WAYLAND_DISPLAY"):
    raise SystemExit("QML integration checks need an active Wayland desktop and open a temporary demo window.")
with tempfile.TemporaryDirectory(prefix="omachat-qml-") as folder:
    config = Path(folder)
    for module in ("Ui", "Commons"):
        (config / module).symlink_to(omarchy / "shell" / module)
    (config / "Chat").symlink_to(repo)
    image = config / "demo.svg"
    image.write_text('<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32"><rect width="32" height="32" fill="#80a0c0"/></svg>')
    (config / "shell.qml").write_text((repo / "tests/qml/shell.qml").read_text())
    env = dict(os.environ, QT_QPA_PLATFORM="wayland", QT_QPA_PLATFORMTHEME="", QT_QUICK_CONTROLS_STYLE="Basic", OMACHAT_TEST_IMAGE=str(image))
    try:
        result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    except subprocess.TimeoutExpired as error:
        print(error.stdout)
        raise
    print(result.stdout)
    if result.returncode or "OMACHAT_QML_PASS" not in result.stdout or "OMACHAT_QML_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    build = config / "Build"
    build.mkdir()
    (build / "Service.qml").write_text((repo / "Service.qml").read_text())
    for source in ("go.mod", "go.sum", "vendor", "cmd", "internal"):
        (build / source).symlink_to(repo / source)
    (config / "shell.qml").write_text((repo / "tests/qml/build.qml").read_text())
    runtime = config / "runtime"
    runtime.mkdir(mode=0o700)
    env.update(XDG_DATA_HOME=str(config / "data"), XDG_CACHE_HOME=str(config / "cache"), XDG_RUNTIME_DIR=str(runtime), GOPROXY="off", GOTOOLCHAIN="local")
    # Keep Wayland reachable while isolating the daemon socket and credentials.
    if not os.path.isabs(env["WAYLAND_DISPLAY"]):
        env["WAYLAND_DISPLAY"] = str(Path(os.environ["XDG_RUNTIME_DIR"]) / env["WAYLAND_DISPLAY"])
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=60)
    print(result.stdout)
    if result.returncode or "OMACHAT_BUILD_PASS" not in result.stdout or "OMACHAT_BUILD_FAIL" in result.stdout:
        raise SystemExit(1)
    if not (build / "bin/omachatd").is_file():
        raise SystemExit("Build succeeded without the expected helper artifact")
