"""Ensure byte-different equivalent exports pass, while changed inference fails."""
import json
import pathlib
import sys
import tempfile
import unittest
from unittest.mock import patch

import onnx
from onnx import numpy_helper

import export_key


class ExportParityTest(unittest.TestCase):
    def check_export(self, changed_weights):
        root = export_key.ROOT
        canonical = root / 'native-analyzer/models/keynet.onnx'
        original = canonical.read_bytes()
        with tempfile.TemporaryDirectory(prefix='ytaudio-export-test-') as directory:
            candidate = pathlib.Path(directory) / 'candidate.onnx'

            def different_export(model, example, output, **kwargs):
                graph = onnx.load(canonical)
                graph.doc_string = 'Equivalent export with different serialization metadata'
                if changed_weights:
                    for tensor in graph.graph.initializer:
                        array = numpy_helper.to_array(tensor).copy()
                        array.fill(0)
                        tensor.CopyFrom(numpy_helper.from_array(array, tensor.name))
                onnx.save(graph, output)

            arguments = ['export_key.py', '--source', str(root / 'build/analyzer/deps/key'),
                         '--output', str(candidate), '--reference', str(canonical)]
            with patch.object(sys, 'argv', arguments), patch.object(export_key.torch.onnx, 'export', side_effect=different_export):
                if changed_weights:
                    with self.assertRaisesRegex(ValueError, 'parity failed'):
                        export_key.main()
                else:
                    export_key.main()
                    self.assertNotEqual(candidate.read_bytes(), original)
                    report = json.loads(candidate.with_suffix('.json').read_text())
                    self.assertNotEqual(report['onnx_sha256'], report['reference_sha256'])
                    self.assertTrue(all(row['export_reference_max_abs_error'] < 1e-4 for row in report['parity']))
            self.assertEqual(canonical.read_bytes(), original)

    def test_equivalent_byte_different_export_passes(self):
        self.check_export(False)

    def test_changed_weights_fail(self):
        self.check_export(True)


if __name__ == '__main__':
    unittest.main()
