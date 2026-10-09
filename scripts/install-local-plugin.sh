#!/usr/bin/env bash
# Build and install this working tree into the user-owned Omarchy plugin directory.
# The script never reads .env files, installs packages, or edits shell.json.
set -Eeuo pipefail

PLUGIN_ID="onelegdave.omachat"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd -P)"
PLUGIN_PARENT="${OMACHAT_PLUGIN_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/omarchy/plugins}"
DRY_RUN=0
YES=0
NO_RESTART=0

usage() {
  cat <<'EOF'
Usage: scripts/install-local-plugin.sh [--dry-run] [--yes] [--no-restart]

Builds the current working tree, validates it, then atomically replaces
~/.config/omarchy/plugins/onelegdave.omachat (or OMACHAT_PLUGIN_DIR). The
previous plugin is retained in a timestamped backup. .env files, Git-ignored
files, and known local data files are excluded; hashed source trees are kept
intact so the staged helper identity matches the build. Repository symlinks and
symlinks in the destination parent path are rejected.

Options:
  --dry-run     Show the target and actions without building or changing files
  --yes         Skip the install confirmation (still prompts before restart)
  --no-restart  Do not prompt to restart the Omarchy shell after installation
  -h, --help    Show this help
EOF
}

while (($#)); do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --yes) YES=1 ;;
    --no-restart) NO_RESTART=1 ;;
    -h|--help) usage; exit 0 ;;
    *) printf 'Unknown option: %s\n' "$1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if ((EUID == 0)); then
  printf 'Run this installer as the target desktop user, not with sudo.\n' >&2
  exit 1
fi

reject_symlink_components() {
  local path="$1" current="/" component
  local -a components
  if [[ "$path" != /* ]]; then
    path="$PWD/$path"
  fi
  IFS='/' read -r -a components <<< "${path#/}"
  for component in "${components[@]}"; do
    case "$component" in
      ''|.) continue ;;
      ..)
        current="${current%/*}"
        [[ -n "$current" ]] || current="/"
        continue
        ;;
    esac
    if [[ "$current" == "/" ]]; then
      current="/$component"
    else
      current="$current/$component"
    fi
    if [[ -L "$current" ]]; then
      printf 'Refusing symlink in plugin parent path: %s\n' "$current" >&2
      return 1
    fi
  done
}

reject_symlink_components "$PLUGIN_PARENT" || exit 1
TARGET="${PLUGIN_PARENT%/}/$PLUGIN_ID"
if ((DRY_RUN)); then
  printf 'Source:  %s\nTarget:  %s\n' "$REPO_ROOT" "$TARGET"
  printf 'Would build, stage, validate, back up any existing plugin, and atomically install the current worktree.\n'
  printf 'No files were changed. .env files and ignored/local data files are excluded.\n'
  exit 0
fi

for command_name in make python3 git omarchy; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    printf 'Required command not found: %s\n' "$command_name" >&2
    exit 1
  fi
done

if ((YES == 0)); then
  if [[ ! -t 0 ]]; then
    printf 'Refusing to replace the local plugin non-interactively; rerun with --yes.\n' >&2
    exit 1
  fi
  printf 'This will replace:\n  %s\n' "$TARGET"
  printf 'An existing plugin will be kept as a timestamped backup. Continue? [y/N] '
  read -r answer || answer=''
  case "$answer" in
    y|Y|yes|YES) ;;
    *) printf 'Cancelled; the installed plugin was not changed.\n'; exit 0 ;;
  esac
fi

mkdir -p -- "$PLUGIN_PARENT"
PLUGIN_PARENT="$(cd -- "$PLUGIN_PARENT" && pwd -P)"
reject_symlink_components "$PLUGIN_PARENT" || exit 1
TARGET="$PLUGIN_PARENT/$PLUGIN_ID"
STAGING_ROOT="$(mktemp -d "$PLUGIN_PARENT/.${PLUGIN_ID}.stage.XXXXXXXX")"
PACKAGE="$STAGING_ROOT/$PLUGIN_ID"
mkdir -p -- "$PACKAGE"
cleanup() {
  if [[ -n "${STAGING_ROOT:-}" && -d "$STAGING_ROOT" ]]; then
    rm -rf -- "$STAGING_ROOT"
  fi
}
trap cleanup EXIT

# Copy tracked and non-ignored working-tree files, including in-progress source
# files, without copying repository credentials or ignored local data.
python3 - "$REPO_ROOT" "$PACKAGE" <<'PY'
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys

root = Path(sys.argv[1])
destination = Path(sys.argv[2])
listing = subprocess.run(
    ["git", "-C", str(root), "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
    check=True, stdout=subprocess.PIPE,
).stdout
private_names = {
    "config.json", "session.json", "cookies.json", "whatsapp.db",
    "whatsapp_history.sqlite", "whatsapp_store.json", "telegram.session",
    "telegram_store.json", "messenger_session.json", "messenger.db",
    "signal_store.json",
}
private_suffixes = (".db", ".sqlite", ".sqlite3", ".session", ".pem", ".key", ".token", ".sock")
for raw in listing.split(b"\0"):
    if not raw:
        continue
    relative = PurePosixPath(os.fsdecode(raw))
    parts = relative.parts
    if relative.is_absolute() or ".." in parts:
        raise SystemExit(f"Unsafe repository path: {relative}")
    source = root.joinpath(*parts)
    for length in range(1, len(parts) + 1):
        component = root.joinpath(*parts[:length])
        if component.is_symlink():
            raise SystemExit(f"Refusing symlink in plugin source: {relative}")
    if any(part == ".git" for part in parts):
        continue
    hashed_tree = bool(parts and parts[0] in {"cmd", "internal", "vendor"})
    if parts and parts[0] == "bin":
        continue
    if not hashed_tree and "__pycache__" in parts:
        continue
    name = relative.name
    if any(part == ".env" or part.startswith(".env.") for part in parts):
        continue
    if not hashed_tree and (name in private_names or name.endswith("_store.json") or name.endswith(private_suffixes)):
        continue
    if not source.exists():
        continue
    target = destination.joinpath(*parts)
    target.parent.mkdir(parents=True, exist_ok=True)
    if source.is_file():
        shutil.copy2(source, target)
PY

printf 'Checking staged helper source identity...\n'
python3 - "$REPO_ROOT" "$PACKAGE" <<'PY'
import json
from pathlib import Path
import subprocess
import sys

def inspect(directory):
    script = Path(directory) / "scripts" / "updates.py"
    output = subprocess.run(
        ["python3", str(script), "inspect"], check=True,
        text=True, stdout=subprocess.PIPE,
    ).stdout
    return json.loads(output)

source = inspect(sys.argv[1])
staged = inspect(sys.argv[2])
if source.get("sourceID") != staged.get("sourceID") or source.get("installed") != staged.get("installed"):
    raise SystemExit("Staged source identity differs from the built worktree; existing plugin was left untouched.")
print("Staged source identity matches the build.")
PY

printf 'Building helper inside the staged plugin...\n'
make -C "$PACKAGE" helper
HELPER="$PACKAGE/bin/omachatd"
if [[ -L "$PACKAGE/bin" || -L "$HELPER" || ! -x "$HELPER" ]]; then
  printf 'Staged build did not produce a regular executable helper.\n' >&2
  exit 1
fi

printf 'Validating staged plugin...\n'
omarchy plugin validate "$PACKAGE"

reject_symlink_components "$PLUGIN_PARENT" || exit 1
if [[ -L "$TARGET" ]]; then
  printf 'Refusing to replace a symlinked plugin target: %s\n' "$TARGET" >&2
  exit 1
fi

BACKUP=''
if [[ -e "$TARGET" || -L "$TARGET" ]]; then
  stamp="$(date +%Y%m%d-%H%M%S)"
  BACKUP="$PLUGIN_PARENT/.${PLUGIN_ID}.backup-$stamp"
  suffix=0
  while [[ -e "$BACKUP" || -L "$BACKUP" ]]; do
    suffix=$((suffix + 1))
    BACKUP="$PLUGIN_PARENT/.${PLUGIN_ID}.backup-$stamp-$suffix"
  done
  mv -- "$TARGET" "$BACKUP"
fi
if ! mv -- "$PACKAGE" "$TARGET"; then
  if [[ -n "$BACKUP" && ( -e "$BACKUP" || -L "$BACKUP" ) ]]; then
    mv -- "$BACKUP" "$TARGET" || true
  fi
  printf 'Could not install the staged plugin; attempted to restore the previous copy.\n' >&2
  exit 1
fi

printf 'Installed current working tree at %s\n' "$TARGET"
if [[ -n "$BACKUP" ]]; then
  printf 'Previous plugin backup: %s\n' "$BACKUP"
fi
printf 'The script did not edit shell.json or enable/disable the plugin.\n'

if ((NO_RESTART)); then
  printf 'Shell restart skipped. When ready, run: omarchy restart shell\n'
elif [[ -t 0 ]]; then
  printf 'Restart the Omarchy shell now to load the plugin? [y/N] '
  read -r answer || answer=''
  case "$answer" in
    y|Y|yes|YES) omarchy restart shell ;;
    *) printf 'Restart skipped. When ready, run: omarchy restart shell\n' ;;
  esac
else
  printf 'Non-interactive run: shell restart skipped. When ready, run: omarchy restart shell\n'
fi
