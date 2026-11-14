"""Offline regressions for verified artifact downloads and model packaging."""
import contextlib
import hashlib
import io
import json
import pathlib
import shutil
import sys
import tempfile
import unittest
import urllib.error
import zipfile
from unittest.mock import patch

import package
import prepare


class ArtifactDownloadTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix='ytaudio-download-test-')
        self.addCleanup(directory.cleanup)
        self.cache = pathlib.Path(directory.name)
        self.data = b'pinned reference checkpoint fixture'
        self.primary = 'https://primary.invalid/checkpoint'
        self.mirror = 'https://mirror.invalid/pinned-revision/checkpoint'
        self.entry = {'url': self.primary, 'size': len(self.data),
                      'sha256': hashlib.sha256(self.data).hexdigest()}
        self.addCleanup(patch.stopall)
        patch.object(prepare, 'LOCK', {'checkpoint': self.entry}).start()
        self.sleep = patch.object(prepare.time, 'sleep').start()
        # Tests assert failure behavior without producing fake CI warnings.
        self.enterContext(contextlib.redirect_stderr(io.StringIO()))

    def http_error(self, code):
        return urllib.error.HTTPError(self.primary, code, 'fixture error', {}, None)

    def assert_no_partial_files(self):
        self.assertEqual(sorted(p.name for p in self.cache.iterdir()), ['checkpoint'] if (self.cache / 'checkpoint').exists() else [])

    def test_valid_cache_never_contacts_server(self):
        (self.cache / 'checkpoint').write_bytes(self.data)
        with patch.object(prepare.urllib.request, 'urlopen') as download:
            self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
        download.assert_not_called()

    def test_temporary_http_failure_recovers(self):
        for code in (408, 429, 500, 502, 503, 504):
            with self.subTest(code=code):
                (self.cache / 'checkpoint').unlink(missing_ok=True)
                with patch.object(prepare.urllib.request, 'urlopen', side_effect=[self.http_error(code), io.BytesIO(self.data)]) as download:
                    self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
                self.assertEqual(download.call_count, 2)
                self.assert_no_partial_files()

    def test_persistent_server_error_is_bounded(self):
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=self.http_error(500)) as download:
            with self.assertRaisesRegex(RuntimeError, 'Could not download checkpoint.*HTTP Error 500'):
                prepare.fetch('checkpoint', self.cache)
        self.assertEqual(download.call_count, 3)
        self.assertEqual([call.args[0] for call in self.sleep.call_args_list], [1, 2])
        self.assert_no_partial_files()

    def test_permanent_http_error_is_not_retried(self):
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=self.http_error(404)) as download:
            with self.assertRaisesRegex(RuntimeError, 'HTTP Error 404'):
                prepare.fetch('checkpoint', self.cache)
        download.assert_called_once()
        self.sleep.assert_not_called()
        self.assert_no_partial_files()

    def test_official_outage_uses_pinned_mirror(self):
        self.entry['mirrors'] = [self.mirror]
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=[self.http_error(500), io.BytesIO(self.data)]) as download:
            self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
        self.assertEqual([call.args[0].full_url for call in download.call_args_list], [self.primary, self.mirror])
        self.sleep.assert_not_called()
        self.assert_no_partial_files()

    def test_interrupted_response_restarts_from_empty_file(self):
        class Interrupted(io.BytesIO):
            def read(self, size=-1):
                if self.tell():
                    raise ConnectionResetError('fixture connection reset')
                return super().read(4)
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=[Interrupted(self.data), io.BytesIO(self.data)]) as download:
            self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
        self.assertEqual(download.call_count, 2)
        self.assert_no_partial_files()

    def test_truncated_response_is_retried(self):
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=[io.BytesIO(self.data[:4]), io.BytesIO(self.data)]) as download:
            self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
        self.assertEqual(download.call_count, 2)
        self.assert_no_partial_files()

    def test_network_timeout_is_retried(self):
        with patch.object(prepare.urllib.request, 'urlopen', side_effect=[TimeoutError('fixture timeout'), io.BytesIO(self.data)]):
            self.assertEqual(prepare.fetch('checkpoint', self.cache).read_bytes(), self.data)
        self.assert_no_partial_files()

    def test_same_size_bad_hash_fails_without_fallback(self):
        self.entry['mirrors'] = [self.mirror]
        previous = b'invalid cached data'
        (self.cache / 'checkpoint').write_bytes(previous)
        with patch.object(prepare.urllib.request, 'urlopen', return_value=io.BytesIO(b'x' * len(self.data))) as download:
            with self.assertRaisesRegex(ValueError, 'Integrity failure'):
                prepare.fetch('checkpoint', self.cache)
        download.assert_called_once()
        self.assertEqual((self.cache / 'checkpoint').read_bytes(), previous)
        self.assert_no_partial_files()

    def test_oversized_response_is_rejected(self):
        with patch.object(prepare.urllib.request, 'urlopen', return_value=io.BytesIO(self.data + b'extra')) as download:
            with self.assertRaisesRegex(ValueError, 'Unexpected artifact size'):
                prepare.fetch('checkpoint', self.cache)
        download.assert_called_once()
        self.assert_no_partial_files()

    def test_local_write_failure_is_not_retried(self):
        with patch.object(prepare.tempfile, 'NamedTemporaryFile', side_effect=PermissionError('fixture read-only cache')):
            with self.assertRaises(PermissionError):
                prepare.fetch('checkpoint', self.cache)
        self.sleep.assert_not_called()


class CanonicalPackagingTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix='ytaudio-packaging-test-')
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)
        self.models = self.root / 'build/analyzer/models'
        self.models.mkdir(parents=True)
        self.canonical = self.root / 'native-analyzer/models/keynet.onnx'
        self.canonical.parent.mkdir(parents=True)
        shutil.copyfile(prepare.ROOT / 'native-analyzer/models/keynet.onnx', self.canonical)
        self.original = self.canonical.read_bytes()
        (self.models / 'beat_this.onnx').write_bytes(b'pinned beat fixture')
        lock = {'version': package.MODELS_VERSION}
        for name, path in (('keynet.onnx', self.canonical), ('beat_this.onnx', self.models / 'beat_this.onnx')):
            lock[name] = hashlib.sha256(path.read_bytes()).hexdigest()
        (self.root / 'native-analyzer/models.lock.json').write_text(json.dumps(lock))
        for relative in ('build/analyzer/deps/beat/LICENSE', 'build/analyzer/deps/key/LICENSE', 'native-analyzer/licenses/beat-this.txt'):
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('fixture license\n')
        self.addCleanup(patch.stopall)
        patch.object(prepare, 'ROOT', self.root).start()
        patch.object(package, 'ROOT', self.root).start()

    def test_export_variation_does_not_change_packaged_bytes(self):
        def validate_candidate(command):
            output = pathlib.Path(command[command.index('--output') + 1])
            reference = pathlib.Path(command[command.index('--reference') + 1])
            self.assertEqual(reference.read_bytes(), self.original)
            self.assertNotEqual(output, reference)
            output.parent.mkdir(parents=True, exist_ok=True)
            output.write_bytes(b'different candidate bytes; parity checked by export_key.py')
            output.with_suffix('.json').write_text('{}')

        # A stale host export/report must not leak into the shared model asset.
        (self.models / 'keynet.onnx').write_bytes(b'stale export')
        (self.models / 'keynet.json').write_text('stale report')
        with patch.object(package, 'run', side_effect=validate_candidate) as run:
            package.package('linux', 'amd64', True, False, sys.executable, models_only=True)
        run.assert_called_once()
        archive = self.root / f'dist/ytaudio-models-{package.MODELS_VERSION}.zip'
        with zipfile.ZipFile(archive) as asset:
            self.assertEqual(asset.read('keynet.onnx'), self.original)
            self.assertNotIn('keynet.json', asset.namelist())
            manifest = json.loads(asset.read('manifest.json'))
            for name, expected in manifest['files'].items():
                self.assertEqual(hashlib.sha256(asset.read(name)).hexdigest(), expected)
            self.assertIn('licenses/MusicalKeyCNN-MIT.txt', manifest['files'])

    def test_changed_canonical_model_is_rejected(self):
        self.canonical.write_bytes(b'changed canonical artifact')
        with self.assertRaisesRegex(ValueError, 'Canonical key model hash mismatch'):
            package.package('windows', 'arm64', True, True, sys.executable, models_only=True)
        self.assertFalse((self.root / 'dist').exists())

    def test_changed_beat_model_is_rejected(self):
        (self.models / 'beat_this.onnx').write_bytes(b'changed beat artifact')
        with self.assertRaisesRegex(ValueError, 'Model hash mismatch: beat_this.onnx'):
            package.package('darwin', 'arm64', True, True, sys.executable, models_only=True)


if __name__ == '__main__':
    unittest.main()
