"""Release checks use synthetic responses; builds never install dependencies."""
import importlib.util
import json
from pathlib import Path
import subprocess
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
        self.root = Path(self.tmp.name)
        (self.root / 'manifest.json').write_text('{"version":"0.3.11"}')
        for folder in ('cmd', 'internal', 'vendor'):
            (self.root / folder).mkdir()
        for name in ('go.mod', 'go.sum', 'cmd/main.go'):
            (self.root / name).write_text('source')
        for mock in (patch.object(u, 'ROOT', self.root), patch.object(u, 'state_path', return_value=self.root / 'state/updates.json')):
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


if __name__ == '__main__': unittest.main()
