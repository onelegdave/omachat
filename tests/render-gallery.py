#!/usr/bin/env python3
"""Render the actual panel with fictional data and stock theme base palettes."""
import json
import os
from pathlib import Path
import subprocess
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
    env = dict(os.environ, QT_QPA_PLATFORM="wayland", QT_QPA_PLATFORMTHEME="",
               QT_QUICK_CONTROLS_STYLE="Basic", OMACHAT_UI_ARTIFACTS=str(output),
               OMACHAT_GALLERY_PALETTES=json.dumps(palettes))
    run = subprocess.run(["qs", "-p", str(config)], env=env, text=True,
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=20)
    print(run.stdout)
    if run.returncode or "OMACHAT_GALLERY_PASS" not in run.stdout or "ERROR" in run.stdout:
        raise SystemExit(1)
