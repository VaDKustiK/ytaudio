"""Offline regression tests for canonical-model packaging and integrity checks."""
import hashlib
import json
import pathlib
import shutil
import sys
import tempfile
import unittest
import zipfile
from unittest.mock import patch

import package
import prepare


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
