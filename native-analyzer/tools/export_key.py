#!/usr/bin/env python3
"""Validate a fresh KeyNet export without replacing the canonical model."""
import argparse
import hashlib
import json
import pathlib
import sys

import numpy as np
import onnx
import onnxruntime as ort
import torch

from prepare import ROOT, LOCK


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def compare(expected, actual, frames, label):
    if expected.shape != actual.shape or not np.isfinite(actual).all():
        raise ValueError(f"Invalid {label} outputs at {frames} frames")
    error = float(np.max(np.abs(expected - actual)))
    if not np.isfinite(error) or error >= 1e-4 or expected.argmax() != actual.argmax():
        raise ValueError(f"{label} parity failed at {frames} frames: max error {error}")
    return error


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=pathlib.Path, required=True)
    parser.add_argument('--output', type=pathlib.Path, required=True)
    parser.add_argument('--reference', type=pathlib.Path)
    args = parser.parse_args()
    canonical = ROOT / 'native-analyzer/models/keynet.onnx'
    if args.output.resolve() == canonical.resolve() or (
        args.reference and args.output.resolve() == args.reference.resolve()
    ):
        raise ValueError('Export to a separate candidate file; do not overwrite the reference')
    checkpoint = args.source / 'checkpoints/keynet.pt'
    if digest(checkpoint) != LOCK['keynet.pt']['sha256']:
        raise ValueError('Source checkpoint hash mismatch')
    reference_hash = None
    if args.reference:
        reference_hash = digest(args.reference)
        lock = json.loads((ROOT / 'native-analyzer/models.lock.json').read_text())
        if reference_hash != lock['keynet.onnx']:
            raise ValueError('Canonical key model hash mismatch')

    sys.path.insert(0, str(args.source.resolve()))
    from model import KeyNet
    model = KeyNet().eval()
    model.load_state_dict(torch.load(checkpoint, map_location='cpu', weights_only=True))
    torch.set_num_threads(2)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    torch.manual_seed(42)
    example = torch.rand(1, 1, 105, 100)
    torch.onnx.export(
        model, example, str(args.output), opset_version=17,
        input_names=['cqt'], output_names=['key'],
        dynamic_axes={'cqt': {3: 'frames'}}, dynamo=False,
    )
    onnx.checker.check_model(onnx.load(args.output))
    options = ort.SessionOptions()
    options.intra_op_num_threads = 2
    options.inter_op_num_threads = 1
    session = ort.InferenceSession(str(args.output), sess_options=options, providers=['CPUExecutionProvider'])
    reference = None
    if args.reference:
        onnx.checker.check_model(onnx.load(args.reference))
        reference = ort.InferenceSession(str(args.reference), sess_options=options, providers=['CPUExecutionProvider'])
    results = []
    for frames in (8, 17, 100, 151, 300):
        x = torch.rand(1, 1, 105, frames)
        with torch.no_grad():
            expected = model(x).numpy()
        actual = session.run(None, {'cqt': x.numpy()})[0]
        record = {'frames': frames, 'max_abs_error': compare(expected, actual, frames, 'Fresh export')}
        if reference:
            reviewed = reference.run(None, {'cqt': x.numpy()})[0]
            record['reference_max_abs_error'] = compare(expected, reviewed, frames, 'Canonical model')
            record['export_reference_max_abs_error'] = compare(reviewed, actual, frames, 'Export/reference')
        results.append(record)
    record = {
        'source_checkpoint_sha256': digest(checkpoint),
        'onnx_sha256': digest(args.output), 'onnx_size': args.output.stat().st_size,
        'reference_sha256': reference_hash, 'opset': 17, 'parity': results,
    }
    args.output.with_suffix('.json').write_text(json.dumps(record, indent=2) + '\n')
    print(json.dumps(record, indent=2))


if __name__ == '__main__':
    main()
