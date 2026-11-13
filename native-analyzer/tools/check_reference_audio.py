#!/usr/bin/env python3
"""Check a local producer-labelled reference without storing/distributing its audio."""
import argparse
import hashlib
import json
import pathlib
import subprocess
import tempfile
import time


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--audio", type=pathlib.Path, required=True)
    parser.add_argument("--binary", type=pathlib.Path, required=True)
    parser.add_argument("--models", type=pathlib.Path, required=True)
    parser.add_argument("--expect-bpm", type=float, required=True)
    parser.add_argument("--expect-tonic", required=True)
    parser.add_argument("--expect-scale", choices=("major", "minor"), required=True)
    parser.add_argument("--expect-steady", action="store_true")
    parser.add_argument("--expect-clear-key", action="store_true")
    parser.add_argument("--report", type=pathlib.Path, required=True)
    args = parser.parse_args()
    original = digest(args.audio)
    with tempfile.TemporaryDirectory(prefix="ytaudio-reference-") as directory:
        pcm = pathlib.Path(directory) / "audio.f32"
        subprocess.run([
            "ffmpeg", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe",
            "-i", str(args.audio.resolve()), "-map", "0:a:0", "-vn", "-t", "1801",
            "-ac", "2", "-ar", "44100", "-f", "f32le", str(pcm),
        ], check=True)
        start = time.monotonic()
        process = subprocess.run([
            str(args.binary.resolve()), "--pcm", str(pcm),
            "--models", str(args.models.resolve()), "--threads", "4",
        ], capture_output=True, text=True, check=True, timeout=1800)
        elapsed = time.monotonic() - start
        result = json.loads(process.stdout)
    assert digest(args.audio) == original, "Reference audio was changed"
    tempo, key = result["tempo"], result["key"]
    checks = {
        "primary_tempo": tempo["bpm"] is not None and abs(tempo["bpm"] - args.expect_bpm) < 1,
        "exact_key": key["tonic"] == args.expect_tonic and key["scale"] == args.expect_scale,
        "steady_tempo": not args.expect_steady or not tempo["variable"],
        "clear_key": not args.expect_clear_key or key["status"] == "estimated",
    }
    report = {
        "reference_type": "producer-supplied local reference; not a held-out benchmark",
        "audio_sha256": original,
        "binary_sha256": digest(args.binary),
        "expected": {"bpm": args.expect_bpm, "tonic": args.expect_tonic, "scale": args.expect_scale},
        "seconds": elapsed,
        "checks": checks,
        "result": result,
    }
    args.report.parent.mkdir(parents=True, exist_ok=True)
    args.report.write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    assert all(checks.values()), "Reference checks failed; see the report"


if __name__ == "__main__":
    main()
