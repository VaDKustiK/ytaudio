# Offline analyzer

This component is separate from the Wails process;
FFmpeg decodes user audio and a CPU-only C++ process returns versioned JSON.
No Python is needed by the installed application. Python is development tooling
for model export and numerical comparison only.

Exact artifact URLs/sizes/digests are in [sources.lock.json](sources.lock.json);
packaged model digests are in [models.lock.json](models.lock.json).

## Protocol v1

`ytaudio-analyzer --pcm FILE --models DIRECTORY --threads N`

PCM is little-endian float32 stereo, 44100 Hz, decoded by the existing FFmpeg.
Maximum input duration is 30 minutes. The source audio is never changed.
Diagnostics/progress are JSON lines on stderr; stdout is one JSON result.
Missing/invalid files or models produce a nonzero exit status. Progress has
`stage` and `percent`. Results include schema/analyzer/model versions, nullable
`tempo.bpm`, `key.tonic`/`scale`, `tuning.reference_hz`/`offset_cents`, explicit
status/uncertainty, and warnings. Confidence is internal evidence, not a calibrated
probability of correctness. Cancellation terminates the analyzer process tree.

Performance modes use the same models and signal processing. Low impact runs
1–2 threads; Balanced 2–4; Maximum speed uses a bounded hardware-based budget.
The models run sequentially with thread spinning disabled and one active job.

## Provenance review

- Beat This! official source `b95c8ab0c58c2d9fcfd40508ae8dffbc05ac4f5c`:
  upstream explicitly licenses code and published weights MIT, and notes training
  audio has separate terms. Do not redistribute training audio.
- beat_this_cpp `07ab790a9ec2eda8093d52d249e3ec4f0510ee72`: MIT. Reuse mel and
  chunked inference components, not its mismatched public API/application.
  Its ONNX exporter identifies the official `final0` checkpoint as its source;
  require numerical parity against that checkpoint before acceptance.
- MusicalKeyCNN `61d8fc170f262410f66a12057adffc5aceea0b2c`: repository-wide MIT
  license, with `checkpoints/keynet.pt` in the same repository and no separate
  checkpoint license found in this revision. Preserve the license/provenance;
  no claim is made that the training datasets are licensed by the code license.
  Published exact-key accuracy is 66.72% on its evaluation, not a guarantee for
  hip-hop/trap/drill/R&B. CQT preprocessing must be reproduced and tested.
- ONNX Runtime 1.23.2: MIT, CPU only. Pin the native SDK per target.
- PocketFFT `0fa0ef591e38c2758e3184c6c23e497b9f732ffa`: BSD-3-Clause.
- r8brain `e71c31bf320f84210bb4bdcb57e296c39ce940f9`: MIT.
- nlohmann/json 3.12.0: MIT.

The downloader's final `after_move` path is registered before reducing the
presentation value to a basename. The frontend receives an opaque identifier;
analysis uses the original saved artifact. Analysis failures do not change the
download result.

## Components and preprocessing

- `src/`: native engine and DSP; `tests/`: rhythm/tuning/class-mapping tests.
- `tools/`: hash-verified source preparation, export, packaging, reference parity,
  input safety and generated-fixture evaluation. No runtime Python dependencies.
- `internal/analyzer/`: FFmpeg orchestration, result validation, installation,
  cancellation, cache, thread budgets and child-process priority.
- `internal/server/analyzer.go` and `web/analyzer.js`: authenticated local API,
  native picker/drop, browser fallback, results and completed-download handoff.
- `.github/workflows/analyzer.yml`: reference gates and five native build jobs;
  `release.yml` includes the separate assets after those jobs succeed.

Beat This! uses the full final0 ONNX artifact, 22050 Hz, 1024-point Hann FFT,
441-sample hop, 128 mel bins from 30–11000 Hz, and log-magnitude preprocessing.
1500-frame overlapping chunks retain the official `keep_first` aggregation.
Peak pooling is independent per channel; adjacent ties retain fractional frame
means and downbeats snap to the nearest beat. Tempo uses a supported interval
cluster and folds single/double beat intervals before checking section medians.
Only coherent sections support a tempo-change warning. The UI displays the
faster of the two tempo interpretations, rounded to a whole number, with no
secondary half/double-time value. Variable/uncertain warnings remain visible.
The native result and cache retain both unrounded values; the generated-fixture
tempo metrics below refer to those native results, before this display preference.

KeyNet uses 44100 Hz CQT, 105 bins, 24 bins/octave, fmin 65 Hz, hop 8820,
`log1p(abs(CQT))`, float32 NCHW, and the upstream final-two-**time**-frame crop.
There is no additional normalization. ONNX export is opset 17 with dynamic time.
Bounded 100-frame windows cover the whole track; logits and agreement are
weighted by unique frame coverage, including very short tails. The 24 outputs
follow the checkpoint's Camelot class order, converted to conventional names.

Tuning uses stable interpolated spectral peaks, circular cents statistics,
frame-normalized evidence and competing-reference detection. It checks absolute
pitch concentration before subtracting a diffuse histogram floor. Mixed tuning
requires a separated peak with substantial valley prominence; a shoulder or
diffuse evidence alone does not establish competing references. Standard tolerance
is ±2.5 cents; uncertain measurements remain unknown or mixed. Reliable detuning
shifts the CQT frequency grid before classification, without modifying source
audio or changing rhythm timing. Harmonic evidence marks inconsistent keys
uncertain; a single sustained pitch cannot establish a major/minor scale.
Noise and insufficient pitched evidence yield an unavailable key. Mixed tuning
and near-half-semitone boundaries mark an otherwise supported key uncertain.
Internal evidence scores are not calibrated correctness probabilities.

## Build and test

Use CPU-only Python 3.13, a C++20 compiler and CMake 3.20+.
Create the export/test environment from the repository root:

```sh
uv venv --python 3.13 .venv-analyzer
uv pip install --python .venv-analyzer/bin/python torch==2.9.1 torchaudio==2.9.1 --index-url https://download.pytorch.org/whl/cpu
uv pip install --python .venv-analyzer/bin/python --index-strategy unsafe-best-match -r native-analyzer/tools/requirements.lock.txt
```

The pinned Python lock is tested on Linux. The reviewed
canonical KeyNet ONNX is stored in `models/` with its upstream MIT license;
every build checks its existing pinned SHA-256. Fresh exports go to
`build/analyzer/export-check/` and are checked against PyTorch and the canonical
artifact at 8–300 frames. They may have different hashes without affecting the
packaged bytes. All five native jobs use the same validated model asset.
Native packaging outside Linux only needs Python's standard library and models
from that asset (`--skip-export` skips only fresh-export comparison). Windows
packaging runs in a Visual Studio developer shell; macOS uses Apple's command-line
tools and ad-hoc signing.

```sh
source .venv-analyzer/bin/activate
uv pip install --python .venv-analyzer/bin/python playwright==1.64.1
export PLAYWRIGHT_BROWSERS_PATH="$PWD/build/analyzer/browsers"
python -m playwright install chromium
python native-analyzer/tools/prepare.py --target linux-amd64 --reference
python native-analyzer/tools/test_packaging.py
python native-analyzer/tools/test_export_key.py
python native-analyzer/tools/package.py --skip-prepare
ctest --test-dir build/analyzer/cmake --output-on-failure
python native-analyzer/tools/check_rhythm.py --reference build/analyzer/reference --checkpoint build/analyzer/final0.ckpt --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --report build/analyzer/rhythm-parity.json
python native-analyzer/tools/check_harmony.py --source build/analyzer/deps/key --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --report build/analyzer/harmony-parity.json
python native-analyzer/tools/check_inputs.py --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models
YTAUDIO_TEST_ANALYZER_DIR="$PWD/build/analyzer/install" go test -race ./internal/analyzer -v
go test -race ./...
go vet ./...
go vet -tags 'desktop production webkit2_41' ./...
staticcheck ./...
actionlint

go build -o /tmp/ytaudio-browser ./cmd/ytaudio
python scripts/ui_test.py --binary /tmp/ytaudio-browser
python scripts/analyzer_ui_test.py --binary /tmp/ytaudio-browser --component build/analyzer/install
YTAUDIO_MEDIA_TESTS=1 go test ./internal/ytdlp -run 'TestLocalAudioConversion|TestSoundCloudRejectsPreviewFormats' -v

python native-analyzer/tools/benchmark.py --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --app-binary /tmp/ytaudio-browser --report build/analyzer/benchmark.json
python native-analyzer/tools/evaluate_fixtures.py --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --report build/analyzer/fixtures.json
```

Playwright requires its Chromium installation. Local conversion regressions use
separately prepared ignored media-test tools:

```sh
python3 scripts/prepare_assets.py --os linux --arch amd64 --output internal/deps/assets/linux-amd64
```

Raw reports, downloaded sources/models, environments and build products stay
outside Git. Benchmarks identify their exact executable with SHA-256.

Artifact downloads retry temporary HTTP/network failures up to three times.
The build-time `final0.ckpt` reference checkpoint also has a revision-pinned
Hugging Face mirror in `sources.lock.json` for outages of the original server.
Both sources must match the same pinned size and SHA-256. Integrity failures
stop the build; they never select different model weights or skip validation.

## Validation and limitations

Native/reference tests compare preprocessing, inference and postprocessing.
Input tests cover invalid and too-short PCM, silence, noise, anti-phase stereo,
clipping, sliding bass and mixed tuning. Integration tests cover source formats,
cache invalidation, cancellation, navigation and completed-download handoff.
Browser tests use simulated native picker/drop callbacks; actual dialogs and
physical drag and drop still require testing in the native application.

Reference comparisons:

| Comparison | Observed maximum absolute difference | Gate tolerance |
| --- | --- | --- |
| Native mel vs official torchaudio on identical resampled PCM | 0.000245 | 0.003 |
| Beat/downbeat ONNX logits vs official PyTorch on identical mel | 0.000028 | 0.003 |
| Beat/downbeat timestamps on identical mel | <0.000002 seconds; same event counts | 0.00001 seconds |
| Native CQT vs librosa 0.11.0 | 0.000140; mean <0.000005 | max 0.03; mean 0.002 |
| Native KeyNet aggregation vs original PyTorch | 0.000003; same predicted class on every parity fixture | 0.03 |
| Exported KeyNet vs PyTorch, 8–300 time frames | <0.000001; identical classes | 0.0001 |

Rhythm fixtures span 2, 29.6, 30, 30.3 and 64 seconds; harmony fixtures span
2–60.4 seconds, including short final windows and 0, -31.77, +23 and +49 cent
offsets. Raw and tuning-corrected key predictions were compared: the +49 cent
fixture changed from G minor to the expected C major after correction; -31.77
and +23 cent fixtures retained C major either way. The boundary result stays
marked uncertain. This limited check does not prove a universal accuracy gain.
r8brain and official soxr HQ have different transition bands: complete
pipeline waveform RMS differs by 0.00135–0.00162, mel RMS by 0.060–0.062 and beat
logit RMS by 0.045–0.083. This is **not** sample-identical resampling. All five
full-pipeline fixtures nevertheless retain identical beat/downbeat counts and
event times within 0.000002 seconds; the full-pipeline gate allows one 20 ms
frame. This small generated set does not establish equivalence on every song.

Generated 24-key fixture evaluation (16-second chord progressions/percussion,
all natural major/minor keys, 84/140 BPM, 440/432 Hz):

| Metric | Result |
| --- | --- |
| Exact key | 19/24 (79.2%) |
| Correct natural major/minor scale-note set, including relative keys | 20/24 (83.3%) |
| Primary tempo within 1 BPM | 11/24 (45.8%) |
| Tempo within 1 BPM including half/double-time | 22/24 (91.7%) |
| Keys marked uncertain | 5/24 |
| Incorrect exact keys without an uncertainty flag | 2/24 |
| Tuning measurements available | 24/24 |
| Maximum tuning error | 0.442 cents / 0.111 Hz |

These are controlled generated fixtures, **not real-world or held-out accuracy**.
Neural key results, particularly relative major/minor, still need musical
verification. Representative independently labelled audio is needed to measure
accuracy for a specific genre or compare with commercial detectors. Local audio
can be checked with `tools/check_reference_audio.py`; it does not store or
distribute the source recording.

## Resource and package measurements

These single-run development measurements are illustrative. Use
`tools/benchmark.py` to measure the current build on your hardware.

A generated three-minute C-major progression with bass/percussion, 140 BPM and
432 Hz was analyzed on Linux amd64, Intel i5-13500H, 16 logical CPUs, GCC 14.2.
Times include the native worker's input read, both models and DSP; they exclude
FFmpeg decoding, first-time installation and cached results. Measurements are
single runs, not statistical latency guarantees. Thread scheduling/contention
can make Maximum speed slower than Balanced. Worker benchmarks apply the thread
budgets directly; Go additionally lowers Low impact process priority. All three modes use the same
algorithms and reported C major, 70 BPM with 140 alternative, and about 432.2 Hz.

| Mode | Threads | Elapsed | Peak RSS | CPU time | Average CPU |
| --- | --- | --- | --- | --- | --- |
| low | 2 | 15.55 s | 825.6 MiB | 27.77 s | 179% |
| balanced | 4 | 10.50 s | 821.7 MiB | 30.69 s | 292% |
| maximum | 15 | 11.58 s | 818.7 MiB | 99.69 s | 861% |

CPU percent counts 100% per logical CPU. Peak RSS is the native worker, not the
entire desktop application; memory grows with input duration. CPU arenas and
memory-pattern retention are disabled, models run sequentially, thread spinning
is disabled and the worker exits after each job. No ML worker or loaded models
remain idle between analyses.

Linux browser-backend idle samples over five seconds each measured 0.0%
CPU before and 0.0% after initializing the analyzer manager, with no
child processes. Backend RSS changed from 14.07 to 16.32 MiB
(+2.25 MiB). This excludes the GUI renderer and is a short sampled
measurement, not a guarantee of zero idle CPU.

Linux amd64 packages are approximately 8.0 MiB runtime ZIP + 73.5 MiB shared
model ZIP, 103.1 MiB installed together. Exact byte sizes from this build:
runtime ZIP 8424588; model ZIP 77032145;
installed 108136968. No Python environment, GPU runtime or FFmpeg is bundled.

## Native Linux WebView smoke test

On a desktop with a display, the existing `scripts/desktop-smoke` also supports
an optional analyzer gate. Set `YTAUDIO_SMOKE_AUDIO` to a generated, standard-tuned
harmonic WAV fixture (the test expects measured 440 Hz), and
`YTAUDIO_SMOKE_DROP_AUDIO` to a second local file with a different filename.
Set `YTAUDIO_ANALYZER_DIR` to the absolute installed component directory and use
isolated `YTAUDIO_CONFIG_DIR`/`YTAUDIO_CACHE_DIR`, with `YTAUDIO_AUTO_SETUP=0`.
Then run:

```sh
go run -tags 'desktop production webkit2_41' ./scripts/desktop-smoke
```

This opens a temporary native test window. It exercises the actual WebView,
audio-picker callback API, analysis, native clipboard copy and the Go-to-JS
Wails drop event. The clipboard is restored. The OS dialog and physical external
drag gesture are intentionally left for manual testing. The analyzer gate is
skipped when `YTAUDIO_SMOKE_AUDIO` is unset.

## Versioning, installation and platform checks

`packaging/analyzer.json` is the shared Go/C++/packaging version source. Bump
`engine_version` when native algorithms or runtime dependencies change; bump
`models_version` and update both model digest locks only when model artifacts
change. Keep schema version separate from engine version. An app update that
requires a different component offers installation again; verified unchanged
models are reused. Development caches also include the executable digest, so
rebuilding without bumping the unreleased version does not reuse stale results.

Packages contain licenses, source provenance and a per-file SHA-256 manifest.
Installation validates the repository/release asset URL, GitHub's SHA-256 digest,
the manifest and pinned model digests before activation. It stages a versioned
engine directory and updates its pointer only after verification. A failed
replacement leaves the active installation available. Analysis never downloads
models or audio; first setup requires connectivity. The main installer and
ordinary downloader dependency repair remain separate.

SDK archives for all five targets are hash-pinned. The native CI matrix builds
and executes unit/input/package smoke tests on actual target runners. Workflow
definitions are checked with actionlint. Production POSIX executables are stripped
before hashing/signing. Windows
packages exclude debug symbols and carry app-local permitted VC runtime DLLs; macOS
packages use loader-relative ORT paths and ad-hoc signatures; Linux uses an
executable-relative ORT path and the existing glibc release baselines. Inspection
of the pinned Apple Silicon SDK confirms minimum macOS 13.4 and
`@rpath/libonnxruntime.1.23.2.dylib`; its package ships that library once.

Before a release, run the native CI matrix and test installation, repair,
offline analysis, native file selection and drag and drop on the supported
operating systems. macOS signing/notarization behavior needs a native check.
