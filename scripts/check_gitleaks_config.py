#!/usr/bin/env python3
"""Verify the model-checksum exception does not hide other credential findings."""
import argparse
import hashlib
import json
import pathlib
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', default='gitleaks')
    args = parser.parse_args()
    root = pathlib.Path(__file__).resolve().parent.parent
    public = json.loads((root / 'native-analyzer/models.lock.json').read_text())['keynet.onnx']
    # Generated synthetic scanner inputs, never credentials for any service.
    other = hashlib.sha256(b'ytaudio scanner regression fixture').hexdigest()
    paths = ('internal/analyzer/models.go', 'native-analyzer/models.lock.json')
    cases = [(name, public, False) for name in paths]
    cases += [(name, other, True) for name in paths]
    cases += [('unexpected/config.txt', public, True)]
    for relative, value, should_detect in cases:
        with tempfile.TemporaryDirectory(prefix='ytaudio-gitleaks-test-') as directory:
            directory = pathlib.Path(directory)
            target = directory / relative
            target.parent.mkdir(parents=True)
            target.write_text('api_key = "' + value + '"\n')
            report = directory / 'report.json'
            run = subprocess.run([
                args.binary, 'dir', '--config', str(root / '.gitleaks.toml'),
                '--redact=100', '--no-banner', '--report-format', 'json',
                '--report-path', str(report), '.',
            ], cwd=directory, capture_output=True, text=True)
            if run.returncode not in (0, 1):
                raise RuntimeError('Gitleaks failed to execute its configuration check')
            findings = json.loads(report.read_text())
            if bool(findings) != should_detect or run.returncode != int(should_detect):
                raise AssertionError(f'Incorrect checksum exception behavior for {relative}')
    print('Gitleaks exception: public checksum allowed only in the two intended paths; other values detected')


if __name__ == '__main__':
    main()
