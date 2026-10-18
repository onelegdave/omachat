#!/usr/bin/env python3
"""Public release checks and explicit, offline helper builds. No package installs."""
import argparse
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
API = 'https://api.github.com/repos/onelegdave/omachat/releases/latest'
RELEASES = 'https://github.com/onelegdave/omachat/releases'
MAX_RESPONSE = 256 * 1024
DAY = 86400


def version(value):
    if not isinstance(value, str) or not re.fullmatch(r'v?\d{1,6}\.\d{1,6}\.\d{1,6}', value):
        raise ValueError('Invalid release version')
    return tuple(int(x) for x in value.removeprefix('v').split('.'))


def installed():
    value = json.loads((ROOT / 'manifest.json').read_text())['version']
    version(value)
    return value


def source_id():
    digest = hashlib.sha256()
    paths = [ROOT / 'go.mod', ROOT / 'go.sum', ROOT / 'manifest.json']
    if (ROOT / 'scripts/updates.py').is_file():
        paths.append(ROOT / 'scripts/updates.py')
    for folder in ('cmd', 'internal', 'vendor'):
        paths.extend(p for p in (ROOT / folder).rglob('*') if p.is_file())
    for path in sorted(paths):
        # Tests do not contribute to the executable.
        if path.name.endswith('_test.go'):
            continue
        digest.update(str(path.relative_to(ROOT)).encode() + b'\0')
        with path.open('rb') as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                digest.update(chunk)
        digest.update(b'\0')
    return digest.hexdigest()


def state_path():
    return Path(os.environ.get('XDG_STATE_HOME') or Path.home() / '.local/state') / 'omachat/updates.json'


def read_state():
    try:
        data = json.loads(state_path().read_text())
        if not isinstance(data, dict):
            return {}
        out = {'automatic': data.get('automatic') is True}
        for key in ('latest', 'dismissed'):
            if data.get(key):
                version(data[key])
                out[key] = data[key]
        for key in ('checkedAt', 'attemptedAt'):
            value = data.get(key, 0)
            if isinstance(value, (float, int)) and 0 <= value <= time.time() + DAY:
                out[key] = value
        return out
    except (OSError, ValueError, TypeError):
        return {}


def save_state(data):
    path = state_path()
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, name = tempfile.mkstemp(dir=path.parent, prefix='.updates-')
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(data, stream)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise ValueError('Unexpected redirect from release server')


def fetch_release():
    request = urllib.request.Request(API, headers={
        'Accept': 'application/vnd.github+json', 'User-Agent': 'OmaChat-update-check'})
    with urllib.request.build_opener(NoRedirect).open(request, timeout=12) as response:
        body = response.read(MAX_RESPONSE + 1)
    if len(body) > MAX_RESPONSE:
        raise ValueError('Release response is too large')
    data = json.loads(body)
    if not isinstance(data, dict) or data.get('draft') or data.get('prerelease'):
        raise ValueError('No stable release found')
    tag = data.get('tag_name')
    version(tag)
    return tag.removeprefix('v')


def update_state(action):
    path = state_path()
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Serialize shell reloads and simultaneous panels without losing preferences.
    with (path.parent / 'updates.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        data = read_state()
        error = ''
        if action in ('auto-on', 'auto-off'):
            data['automatic'] = action == 'auto-on'
        elif action == 'dismiss':
            data['dismissed'] = data.get('latest', '')
        elif action == 'check' or (action == 'daily' and data.get('automatic') and
                                  time.time() - data.get('attemptedAt', 0) >= DAY):
            data['attemptedAt'] = time.time()
            save_state(data)  # Back off even if a process is interrupted or offline.
            try:
                data['latest'] = fetch_release()
                data['checkedAt'] = time.time()
            except Exception as exc:
                error = 'Could not check GitHub releases. Try again later. (' + type(exc).__name__ + ')'
        save_state(data)
        return dict(data, installed=installed(), error=error)


def cache_dir():
    return Path(os.environ.get('XDG_CACHE_HOME') or Path.home() / '.cache') / 'omachat'


def unsupported_layout(destination):
    return RuntimeError('OmaChat cannot install its helper safely: no folder outside the plugin '
                        'shares a filesystem with ' + str(destination) + '. Set XDG_CACHE_HOME to a '
                        'folder on that filesystem, then rebuild.')


def staging_parent(target):
    """Pick where to build: outside the shell's view, on the helper's filesystem.

    The shell reloads a plugin on every write within it, but ignores hidden
    entries at the top of its plugins folder, so a hidden sibling of ROOT is
    the first choice and the cache folder the second. The finished helper is
    renamed into target, or into ROOT as target on a first install, and a
    rename is only atomic within one filesystem. Copying into the plugin
    instead would let the shell reload before the copy is complete, so a
    layout with neither folder on that filesystem is refused before any write.
    """
    destination = target if target.is_dir() else ROOT
    device = os.stat(destination).st_dev
    for candidate in (ROOT.parent, cache_dir()):
        try:
            # Check before mkdir, following symlinks: creating a cache folder
            # inside the plugin would itself be a write the shell reloads on.
            if candidate.resolve().is_relative_to(ROOT.resolve()):
                continue
            candidate.mkdir(parents=True, exist_ok=True, mode=0o700)
            if os.stat(candidate).st_dev == device:
                return candidate
        except OSError:
            continue
    raise unsupported_layout(destination)


def sync_file(path):
    with open(path, 'rb') as stream:
        os.fsync(stream.fileno())


def remove_build_leftovers(target):
    """Remove partial helpers and the old lock left in bin by releases before 0.5.0."""
    for leftover in list(target.glob('.omachatd-*')) + [target / '.build.lock']:
        try:
            if leftover.is_file() and not leftover.is_symlink():
                leftover.unlink()
        except OSError:
            pass


def build():
    target = ROOT / 'bin'
    lock_dir = state_path().parent
    if lock_dir.resolve().is_relative_to(ROOT.resolve()):
        raise ValueError('Build state must be outside the plugin directory.')
    lock_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    with (lock_dir / 'build.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        source = source_id()
        release = installed()
        with tempfile.TemporaryDirectory(dir=staging_parent(target), prefix='.omachat-build-') as stage:
            staged_bin = Path(stage) / 'bin'
            staged_bin.mkdir(mode=0o755)
            name = staged_bin / 'omachatd'
            env = dict(os.environ, GOPROXY='off', GOSUMDB='off', GOTOOLCHAIN='local', CGO_ENABLED='1')
            flags = '-X main.version=' + release + ' -X github.com/onelegdave/omachat/internal/buildinfo.SourceID=' + source
            # SourceID above is the reproducible build identity. Avoid Go's VCS
            # stamping because installed plugins are not necessarily Git trees.
            subprocess.run(['go', 'build', '-buildvcs=false', '-mod=vendor', '-ldflags', flags, '-o', name, './cmd/omachatd'],
                           cwd=ROOT, env=env, check=True)
            if source_id() != source or installed() != release:
                raise RuntimeError('Source changed during the build. Please rebuild again.')
            os.chmod(name, 0o755)
            sync_file(name)
            # Nothing above wrote inside ROOT. The rename below is the first
            # write the shell sees; a reload it causes can only follow it.
            # A first install publishes the directory and complete helper at
            # once; creating an empty bin earlier would also trigger reload.
            # A failed rename writes nothing, so the old helper stays usable.
            try:
                if target.is_dir():
                    os.replace(name, target / 'omachatd')
                else:
                    os.replace(staged_bin, target)
            except OSError as exc:
                # Same device numbers can still be separate mounts.
                if exc.errno != errno.EXDEV:
                    raise
                raise unsupported_layout(target if target.is_dir() else ROOT) from exc
            remove_build_leftovers(target)
    return {'sourceID': source, 'installed': release}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['inspect', 'build', 'state', 'check', 'daily', 'auto-on', 'auto-off', 'dismiss', 'update'])
    action = parser.parse_args().action
    if action == 'update':
        print('OmaChat update: review the changes and confirm in the native updater.\n', flush=True)
        result = subprocess.run(['omarchy', 'plugin', 'update', 'onelegdave.omachat'])
        print('\nReturn to OmaChat Settings > Updates. Rebuild the helper if requested.\n'
              'If the panel has not reloaded, run: omarchy restart shell', flush=True)
        try:
            input('Press Enter to close this terminal. ')
        except EOFError:
            pass
        return result.returncode
    try:
        if action == 'inspect':
            result = {'sourceID': source_id(), 'installed': installed()}
        elif action == 'build':
            result = build()
        else:
            result = update_state(action)
        print(json.dumps(result))
        return 0
    except Exception as exc:
        print(json.dumps({'error': str(exc)}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
