#!/usr/bin/env python3
"""Exercise real QML with a short-lived demo window and a mock service."""
import os
import shlex
import shutil
import hashlib
from pathlib import Path
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[1]
go_cache = subprocess.check_output(["go", "env", "GOCACHE"], text=True,
                                 env=dict(os.environ, GOTOOLCHAIN="local", GOPROXY="off")).strip()
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

    (config / "shell.qml").write_text((repo / "tests/qml/pagination.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(result.stdout)
    if result.returncode or "OMACHAT_PAGINATION_PASS" not in result.stdout or "OMACHAT_PAGINATION_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    (config / "shell.qml").write_text((repo / "tests/qml/readability.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(result.stdout)
    if result.returncode or "OMACHAT_READABILITY_PASS" not in result.stdout or "OMACHAT_READABILITY_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    (config / "shell.qml").write_text((repo / "tests/qml/media.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(result.stdout)
    if result.returncode or "OMACHAT_MEDIA_PASS" not in result.stdout or "OMACHAT_MEDIA_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    (config / "shell.qml").write_text((repo / "tests/qml/popout.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(result.stdout)
    if result.returncode or "OMACHAT_POPOUT_PASS" not in result.stdout or "OMACHAT_POPOUT_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    for fixture, marker in (("review", "OMACHAT_REVIEW"), ("panel-keyboard", "OMACHAT_PANEL_KEYBOARD"), ("updates", "OMACHAT_UPDATES")):
        (config / "shell.qml").write_text((repo / ("tests/qml/" + fixture + ".qml")).read_text())
        result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
        print(result.stdout)
        if result.returncode or marker + "_PASS" not in result.stdout or marker + "_FAIL" in result.stdout or "ERROR" in result.stdout:
            raise SystemExit(1)

    build = config / "Build"
    build.mkdir()
    (build / "Service.qml").write_text((repo / "Service.qml").read_text())
    for source in ("go.mod", "go.sum", "vendor", "cmd", "internal", "manifest.json", "UpdateManager.qml"):
        (build / source).symlink_to(repo / source)
    (build / "scripts").mkdir()
    (build / "scripts/updates.py").write_text((repo / "scripts/updates.py").read_text())
    (config / "shell.qml").write_text((repo / "tests/qml/build.qml").read_text())
    runtime = config / "runtime"
    runtime.mkdir(mode=0o700)
    env.update(XDG_STATE_HOME=str(config / "state"), XDG_DATA_HOME=str(config / "data"), XDG_CACHE_HOME=str(config / "cache"), XDG_RUNTIME_DIR=str(runtime), GOPROXY="off", GOTOOLCHAIN="local", GOCACHE=go_cache)
    # Keep Wayland reachable while isolating the daemon socket and credentials.
    if not os.path.isabs(env["WAYLAND_DISPLAY"]):
        env["WAYLAND_DISPLAY"] = str(Path(os.environ["XDG_RUNTIME_DIR"]) / env["WAYLAND_DISPLAY"])
    try:
        result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    except subprocess.TimeoutExpired as error:
        print(error.stdout)
        raise
    print(result.stdout)
    if result.returncode or "OMACHAT_BUILD_PASS" not in result.stdout or "OMACHAT_BUILD_FAIL" in result.stdout:
        raise SystemExit(1)
    if not (build / "bin/omachatd").is_file():
        raise SystemExit("Build succeeded without the expected helper artifact")

    # A missing build launcher must clear the busy state and keep the old helper.
    helper_before = hashlib.sha256((build / "bin/omachatd").read_bytes()).digest()
    (config / "shell.qml").write_text((repo / "tests/qml/build-unavailable.qml").read_text())
    result = subprocess.run([shutil.which("qs"), "-p", str(config)], env=dict(env, PATH="/nonexistent"),
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=15)
    print(result.stdout)
    if result.returncode or "OMACHAT_BUILD_UNAVAILABLE_PASS" not in result.stdout or "OMACHAT_BUILD_UNAVAILABLE_FAIL" in result.stdout:
        raise SystemExit(1)
    if hashlib.sha256((build / "bin/omachatd").read_bytes()).digest() != helper_before:
        raise SystemExit("Missing Python changed the existing executable")

    # Simulate an installed source update with the old executable still present.
    # Replace only the temporary checkout's go.mod symlink, never the real source.
    old_mod = (build / "go.mod").read_text()
    (build / "go.mod").unlink()
    (build / "go.mod").write_text(old_mod + "\n")
    (config / "shell.qml").write_text((repo / "tests/qml/upgrade.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    print(result.stdout)
    if result.returncode or "OMACHAT_UPGRADE_PASS" not in result.stdout or "OMACHAT_UPGRADE_FAIL" in result.stdout or "ERROR" in result.stdout:
        raise SystemExit(1)

    # Real desktop startup can be slower than the first socket attempt.
    # Delay only this temporary helper, using isolated data and runtime paths.
    binary = build / "bin/omachatd-real"
    (build / "bin/omachatd").rename(binary)
    launcher = build / "bin/omachatd"
    launcher.write_text("#!/bin/sh\nsleep 2\nexec " + shlex.quote(str(binary)) + ' "$@"\n')
    launcher.chmod(0o700)
    (config / "shell.qml").write_text((repo / "tests/qml/connection.qml").read_text())
    result = subprocess.run(["qs", "-p", str(config)], env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(result.stdout)
    if result.returncode or "OMACHAT_CONNECTION_PASS" not in result.stdout or "OMACHAT_CONNECTION_FAIL" in result.stdout:
        raise SystemExit(1)
