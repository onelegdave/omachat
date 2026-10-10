#!/usr/bin/env python3
"""Render the actual panel with fictional data and stock theme base palettes."""
import json
import os
from pathlib import Path
import subprocess
import shutil
import tempfile
import tomllib

repo = Path(__file__).resolve().parents[1]
omarchy = Path(os.environ.get("OMARCHY_PATH", "/usr/share/omarchy"))
output = repo / "docs/screenshots"
output.mkdir(parents=True, exist_ok=True)
palettes = []
for slug in ("tokyo-night", "catppuccin-latte", "gruvbox"):
    with (omarchy / "themes" / slug / "colors.toml").open("rb") as stream:
        colors = tomllib.load(stream)
    palettes.append(dict(slug=slug, **{k: colors[k] for k in
                                     ("background", "foreground", "accent", "muted", "red")}))
with tempfile.TemporaryDirectory(prefix="omachat-gallery-") as folder:
    config = Path(folder)
    for module in ("Ui", "Commons"):
        (config / module).symlink_to(omarchy / "shell" / module)
    (config / "Chat").symlink_to(repo)
    (config / "shell.qml").symlink_to(repo / "tests/qml/gallery.qml")
    display = os.environ.get("WAYLAND_DISPLAY", "")
    if not display:
        raise SystemExit("Screenshot rendering needs an active Wayland desktop.")
    if not os.path.isabs(display):
        display = str(Path(os.environ.get("XDG_RUNTIME_DIR", f"/run/user/{os.getuid()}")) / display)
    runtime = config / "runtime"
    runtime.mkdir(mode=0o700)
    env = dict(os.environ, QT_QPA_PLATFORM="wayland", QT_QPA_PLATFORMTHEME="",
               QT_QUICK_CONTROLS_STYLE="Basic", OMACHAT_UI_ARTIFACTS=str(output),
               OMACHAT_GALLERY_PALETTES=json.dumps(palettes), WAYLAND_DISPLAY=display,
               XDG_CONFIG_HOME=str(config / "settings"), XDG_DATA_HOME=str(config / "data"),
               XDG_CACHE_HOME=str(config / "cache"), XDG_STATE_HOME=str(config / "state"),
               XDG_RUNTIME_DIR=str(runtime))
    run = subprocess.run(["qs", "-p", str(config)], env=env, text=True,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(run.stdout)
    if run.returncode or "OMACHAT_GALLERY_PASS" not in run.stdout or "ERROR" in run.stdout:
        raise SystemExit(1)
    shutil.copyfile(output / "tokyo-night.png", repo / "preview.png")
