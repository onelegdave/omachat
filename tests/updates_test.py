"""Release checks use synthetic responses; builds never install dependencies."""
import importlib.util
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch, MagicMock

spec = importlib.util.spec_from_file_location('updates', Path(__file__).resolve().parents[1] / 'scripts/updates.py')
u = importlib.util.module_from_spec(spec)
spec.loader.exec_module(u)


class UpdatesTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.fixture = Path(self.tmp.name)
        self.root = self.fixture / 'plugin'
        self.root.mkdir()
        (self.root / 'manifest.json').write_text('{"version":"0.3.11"}')
        for folder in ('cmd', 'internal', 'vendor'):
            (self.root / folder).mkdir()
        for name in ('go.mod', 'go.sum', 'cmd/main.go'):
            (self.root / name).write_text('source')
        for mock in (patch.object(u, 'ROOT', self.root), patch.object(u, 'state_path', return_value=self.fixture / 'state/updates.json')):
            mock.start()
            self.addCleanup(mock.stop)

    def test_opt_in_daily_backoff_and_dismissal(self):
        with patch.object(u, 'fetch_release', return_value='0.4.0') as fetch:
            self.assertFalse(u.update_state('daily').get('automatic', False))
            fetch.assert_not_called()
            u.update_state('auto-on')
            self.assertEqual(u.update_state('daily')['latest'], '0.4.0')
            u.update_state('daily')
            self.assertEqual(fetch.call_count, 1)
            self.assertEqual(u.update_state('dismiss')['dismissed'], '0.4.0')
            fetch.return_value = '0.4.1'
            result = u.update_state('check')
            self.assertNotEqual(result['latest'], result['dismissed'])
            u.update_state('auto-off')
            u.update_state('daily')
            self.assertEqual(fetch.call_count, 2)

    def test_offline_retains_last_success_and_backs_off(self):
        u.save_state({'automatic': True, 'latest': '0.4.0', 'checkedAt': 1})
        with patch.object(u, 'fetch_release', side_effect=TimeoutError) as fetch:
            result = u.update_state('daily')
            self.assertIn('Could not check', result['error'])
            self.assertEqual(result['latest'], '0.4.0')
            self.assertEqual(result['checkedAt'], 1)
            u.update_state('daily')
            self.assertEqual(fetch.call_count, 1)

    def test_corrupt_state_recovers_with_automatic_off(self):
        p = u.state_path()
        p.parent.mkdir()
        p.write_text('{not json')
        with patch.object(u, 'fetch_release') as fetch:
            u.update_state('daily')
            fetch.assert_not_called()
        p.write_text(json.dumps({'automatic': True, 'latest': '../evil'}))
        self.assertEqual(u.read_state(), {})

    def test_metadata_is_bounded_and_untrusted_urls_ignored(self):
        response = MagicMock()
        response.__enter__.return_value = response
        opener = MagicMock()
        opener.open.return_value = response
        with patch.object(u.urllib.request, 'build_opener', return_value=opener):
            response.read.return_value = json.dumps({'tag_name': 'v0.4.0', 'html_url': 'https://evil.test/'}).encode()
            self.assertEqual(u.fetch_release(), '0.4.0')
            self.assertEqual(opener.open.call_args.kwargs['timeout'], 12)
            response.read.return_value = b'x' * (u.MAX_RESPONSE + 1)
            with self.assertRaises(ValueError): u.fetch_release()
            for data in ({'tag_name': 'v1.2.3;bad'}, {'tag_name': 'v1.2.3', 'prerelease': True}, []):
                response.read.return_value = json.dumps(data).encode()
                with self.assertRaises(ValueError): u.fetch_release()
        with self.assertRaises(ValueError):
            u.NoRedirect().redirect_request(None, None, 302, '', {}, 'http://localhost/')

    def test_failed_build_preserves_executable_and_cleans_temp(self):
        (self.root / 'bin').mkdir()
        helper = self.root / 'bin/omachatd'
        helper.write_text('old')
        with patch.object(u.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'go')):
            with self.assertRaises(subprocess.CalledProcessError): u.build()
        self.assertEqual(helper.read_text(), 'old')
        self.assertEqual(list((self.root / 'bin').glob('.omachatd-*')), [])
        self.assertEqual(list(self.fixture.glob('.omachat-build-*')), [])

    def test_first_build_does_not_touch_plugin_until_helper_is_complete(self):
        def compile(argv, **kwargs):
            self.assertFalse((self.root / 'bin').exists())
            output = Path(argv[argv.index('-o') + 1])
            self.assertFalse(output.is_relative_to(self.root))
            output.write_text('complete')
        with patch.object(u.subprocess, 'run', side_effect=compile):
            u.build()
        self.assertEqual((self.root / 'bin/omachatd').read_text(), 'complete')
        self.assertEqual((self.root / 'bin/omachatd').stat().st_mode & 0o777, 0o755)
        self.assertEqual(list(self.fixture.glob('.omachat-build-*')), [])

    def test_failed_first_build_leaves_no_plugin_artifacts(self):
        with patch.object(u.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'go')):
            with self.assertRaises(subprocess.CalledProcessError):
                u.build()
        self.assertFalse((self.root / 'bin').exists())
        self.assertEqual(list(self.fixture.glob('.omachat-build-*')), [])

    def test_install_failure_preserves_old_helper_and_cleans_staging(self):
        (self.root / 'bin').mkdir()
        helper = self.root / 'bin/omachatd'
        helper.write_text('old')
        def compile(argv, **kwargs):
            Path(argv[argv.index('-o') + 1]).write_text('complete')
        with patch.object(u.subprocess, 'run', side_effect=compile), patch.object(u.os, 'replace', side_effect=OSError('Install failed')):
            with self.assertRaises(OSError):
                u.build()
        self.assertEqual(helper.read_text(), 'old')
        self.assertEqual(list(self.fixture.glob('.omachat-build-*')), [])

    def test_build_lock_blocks_overlapping_builds(self):
        def compile(argv, **kwargs):
            with self.assertRaises(BlockingIOError):
                u.build()
            Path(argv[argv.index('-o') + 1]).write_text('complete')
        with patch.object(u.subprocess, 'run', side_effect=compile):
            u.build()
        self.assertEqual((self.root / 'bin/omachatd').read_text(), 'complete')

    def test_build_rejects_state_inside_plugin_before_writing(self):
        before = sorted(self.root.rglob('*'))
        with patch.object(u, 'state_path', return_value=self.root / 'state/updates.json'):
            with self.assertRaisesRegex(ValueError, 'outside the plugin directory'):
                u.build()
        self.assertEqual(sorted(self.root.rglob('*')), before)

    @unittest.skipUnless(shutil.which('inotifywait'), 'inotifywait is needed for the reload regression')
    def test_first_watched_write_can_kill_builder_without_losing_install(self):
        # Match the shell's recursive event set. The observer kills the build
        # parent at its first plugin write, while the compiler uses fake bytes.
        code = '''import importlib.util, sys, time
from pathlib import Path
from unittest.mock import patch
spec = importlib.util.spec_from_file_location('updates', sys.argv[1])
u = importlib.util.module_from_spec(spec)
spec.loader.exec_module(u)
u.ROOT = Path(sys.argv[2])
def compile(argv, **kwargs):
    time.sleep(0.3)
    Path(argv[argv.index('-o') + 1]).write_text('complete')
with patch.object(u, 'state_path', return_value=Path(sys.argv[3])), patch.object(u.subprocess, 'run', side_effect=compile):
    u.build()
'''
        for existing in (False, True):
            with self.subTest(existing_helper=existing):
                if existing:
                    (self.root / 'bin/omachatd').write_text('old')
                watcher = subprocess.Popen(['inotifywait', '-m', '-r', '-e',
                    'close_write,create,delete,move', '--format', '%w%f', str(self.root)],
                    stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                builder = None
                try:
                    ready = b''
                    while b'Watches established.' not in ready:
                        self.assertTrue(select.select([watcher.stderr], [], [], 5)[0], 'Watcher did not start')
                        chunk = os.read(watcher.stderr.fileno(), 4096)
                        self.assertTrue(chunk, 'Watcher exited before establishing watches')
                        ready += chunk
                    builder = subprocess.Popen([sys.executable, '-B', '-c', code,
                        str(Path(u.__file__)), str(self.root), str(u.state_path())],
                        stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                    self.assertTrue(select.select([watcher.stdout], [], [], 5)[0], 'No installation event')
                    os.read(watcher.stdout.fileno(), 4096)
                    if builder.poll() is None:
                        builder.terminate()
                    _, error = builder.communicate(timeout=5)
                    self.assertEqual((self.root / 'bin/omachatd').read_text(), 'complete', error.decode())
                finally:
                    if builder and builder.poll() is None:
                        builder.kill()
                        builder.communicate(timeout=5)
                    watcher.terminate()
                    watcher.communicate(timeout=5)

    def test_build_replacement_and_mid_build_change(self):
        def compile(argv, **kwargs):
            self.assertEqual(kwargs['env']['GOPROXY'], 'off')
            self.assertEqual(kwargs['env']['GOTOOLCHAIN'], 'local')
            Path(argv[argv.index('-o') + 1]).write_text('new')
        with patch.object(u.subprocess, 'run', side_effect=compile):
            result = u.build()
        self.assertEqual(result['sourceID'], u.source_id())
        helper = self.root / 'bin/omachatd'
        self.assertEqual(helper.read_text(), 'new')
        helper.write_text('old')
        def changing_compile(argv, **kwargs):
            compile(argv, **kwargs)
            (self.root / 'cmd/main.go').write_text('changed during build')
        with patch.object(u.subprocess, 'run', side_effect=changing_compile):
            with self.assertRaisesRegex(RuntimeError, 'Source changed'): u.build()
        self.assertEqual(helper.read_text(), 'old')

    def test_update_keeps_native_confirmation_and_no_shell(self):
        with patch('sys.argv', ['updates.py', 'update']), patch('builtins.input', return_value=''), patch.object(u.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)) as run:
            self.assertEqual(u.main(), 1)
            run.assert_called_once_with(['omarchy', 'plugin', 'update', 'onelegdave.omachat'])

    def test_fingerprint_detects_vendor_and_source_not_tests(self):
        original = u.source_id()
        (self.root / 'internal/ignored_test.go').write_text('test')
        self.assertEqual(u.source_id(), original)
        (self.root / 'vendor/lib.go').write_text('changed')
        self.assertNotEqual(u.source_id(), original)


class HelperVcsTest(unittest.TestCase):
    def test_real_build_without_usable_git_metadata_keeps_source_identity(self):
        for git_metadata in ('absent', 'incomplete'):
            with self.subTest(git_metadata=git_metadata), tempfile.TemporaryDirectory() as directory:
                parent = Path(directory)
                root = parent / 'plugin'
                (root / 'cmd/omachatd').mkdir(parents=True)
                (root / 'internal/buildinfo').mkdir(parents=True)
                (root / 'vendor').mkdir()
                (root / 'manifest.json').write_text('{"version":"0.3.11"}')
                (root / 'go.mod').write_text('module github.com/onelegdave/omachat\n\ngo 1.24.0\n')
                (root / 'go.sum').write_text('')
                (root / 'internal/buildinfo/buildinfo.go').write_text(
                    'package buildinfo\nvar SourceID string\n')
                (root / 'cmd/omachatd/main.go').write_text(
                    'package main\nimport ("encoding/json"; "os"; '
                    '"github.com/onelegdave/omachat/internal/buildinfo")\n'
                    'var version string\nfunc main() { '
                    'json.NewEncoder(os.Stdout).Encode(map[string]string{'
                    '"version": version, "sourceID": buildinfo.SourceID}) }\n')
                if git_metadata == 'incomplete':
                    # An installed copy may retain an unusable .git directory.
                    # Prove the fixture actually reproduces Go's stamping failure.
                    (root / '.git').mkdir()
                    baseline = subprocess.run(
                        ['go', 'build', '-mod=vendor', '-o', str(parent / 'baseline'), './cmd/omachatd'],
                        cwd=root, env=dict(os.environ, GOPROXY='off', GOSUMDB='off',
                                           GOTOOLCHAIN='local', CGO_ENABLED='1'),
                        capture_output=True, text=True, timeout=60)
                    self.assertNotEqual(baseline.returncode, 0)
                    self.assertIn('error obtaining VCS status', baseline.stderr)
                with patch.object(u, 'ROOT', root), patch.object(
                        u, 'state_path', return_value=parent / 'state/updates.json'):
                    expected_source = u.source_id()
                    receipt = u.build()
                    self.assertEqual(receipt, {'sourceID': expected_source, 'installed': '0.3.11'})
                    helper = root / 'bin/omachatd'
                    result = subprocess.run([str(helper)], check=True, capture_output=True,
                                            text=True, timeout=10)
                    self.assertEqual(json.loads(result.stdout), {
                        'version': '0.3.11', 'sourceID': expected_source})
                    self.assertEqual(u.source_id(), expected_source)


if __name__ == '__main__': unittest.main()
