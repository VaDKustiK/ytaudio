# Offline analyzer

Development is on `dev`. This component is separate from the Wails process;
FFmpeg decodes user audio and a CPU-only C++ process returns versioned JSON.
No Python is needed by the installed application. Python is development tooling
for model export and numerical comparison only.

## Milestone gates

Each gate must pass before the following milestone is accepted. Building an
executable is not an accuracy benchmark or evidence of native platform support.

1. Architecture/provenance: inspect upstream source and licenses, pin artifacts,
   define CLI/schema, retain Windows amd64/arm64, macOS arm64, Linux amd64/arm64.
2. Rhythm: native mel, ONNX inference and postprocessing compared with the
   official final0 reference, including chunk boundaries; robust tempo tests.
3. Harmony: reference/native CQT and model parity, controlled tuning/key tests,
   explicit unknown/mixed results, no fabricated measurements.
4. Integration: local picker/drop and completed-download detection, one cancellable
   analysis, bounded cache, persistent performance settings, regression tests.
5. Distribution: verified separate packages, atomic installation, offline tests,
   packaged runtime smoke tests on each supported target.
6. Evaluation: resource measurements and independently labelled representative
   beats. Synthetic fixtures alone do not establish real-world accuracy.

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

## Reproduce the local gates

Use the CPU Python 3.13 environment described in the root README, a C++20
compiler and CMake 3.20+. The pinned Python lock is tested on Linux. The reviewed
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
YTAUDIO_MEDIA_TESTS=1 go test ./internal/ytdlp -run TestLocalAudioConversion -v

python native-analyzer/tools/benchmark.py --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --app-binary /tmp/ytaudio-browser --report build/analyzer/benchmark.json
python native-analyzer/tools/evaluate_fixtures.py --binary build/analyzer/install/ytaudio-analyzer --models build/analyzer/install/models --report build/analyzer/fixtures.json
```

Playwright requires its Chromium installation. Local conversion regressions use
separately prepared ignored media-test tools, as described in the root README.
Raw reports, downloaded sources/models, environments and build products stay
outside Git. Benchmarks identify their exact executable with SHA-256.

## Validation record — 13.11.2026

| Gate | Observed status |
| --- | --- |
| 1: architecture, provenance, artifact pins | Complete; exact revisions/hashes in lock files. |
| 2: rhythm implementation/reference parity | Passed local unit, model, FFT and chunk-boundary gates. Genre accuracy remains unverified. |
| 3: harmony implementation/reference parity | Passed local CQT/model, class mapping, tuning, mixed/unknown and boundary tests. |
| 4: integration and regressions | Passed local Go race tests, actual FFmpeg/native analysis and browser UI flows. Native Linux WebView analysis/copy/drop-event smoke test passed; physical dialog/drop still needs manual testing. |
| 5: distribution/offline operation | Linux amd64 installation, repair, rollback and model reuse passed locally. Native package/input checks passed dev CI on Linux amd64/arm64 and macOS arm64. Windows requires a rerun with the MSVC fix. |
| 6: resource/accuracy review | Generated-fixture evaluation and resource measurements complete. Independently labelled real-world beats and full native platform validation remain outstanding. |

The commands above passed locally, including source-format tests for WAV, MP3,
FLAC, M4A, OGG and Opus at 44.1/48 kHz, Unicode paths, cache hits/invalidation,
request replacement, cancellation/manager shutdown during inference, temporary-file
cleanup, navigation during analysis, persistent file errors and stale-result
prevention when reselecting the same path.
Safety tests reject truncated, nonfinite, oversized and too-short PCM; silence,
noise-only input, anti-phase stereo, extreme clipping, sliding/distorted bass
and mixed tuning are handled explicitly.
Installer tests use real release archives with a simulated GitHub transport;
they do not publish a release or prove live GitHub asset availability. Browser
UI tests use real FFmpeg/native processing with simulated native picker/drop
callbacks and optional-component availability. A real Linux WebView smoke test
also passed for native picker callbacks, analysis, clipboard and Go-to-JS drop
events. Physical OS drag/drop and the actual native dialog remain manual checks.
Existing downloader UI and conversion regressions pass.

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
No representative producer-labelled trap/drill/hip-hop/R&B set was available,
and no audio was sent to Tunebat or Antares. The requested accuracy targets have
not been demonstrated. Neural key results, particularly relative major/minor,
still need musical verification; do not advertise commercial-detector parity.

### Local producer reference

A supplied 133.19-second stereo OGG (48 kHz), labelled by its producer as
130 BPM / F# major, reproduced two false warnings. Taking a median before
folding missed beats invented a 1.5x interval in two sections. Weak pitch
shoulders also triggered the previous competing-tuning rule. After fixing those
general rules, the full file reports 130.013 BPM, F# major without an uncertainty
flag, and standard tuning (440 Hz within the ±2.5-cent tolerance). The producer
did not supply an independent tuning measurement. The 24 generated-fixture
metrics above are unchanged; true tempo changes, genuinely competing tunings,
noise and sliding-bass checks still pass.

This is one development reference, not held-out evidence of general accuracy.
Its audio is neither required by CI nor included in app/model packages. Root
OGG files are ignored by Git and the supplied file can be deleted. To check
another local reference with Python's standard library and installed FFmpeg:

```sh
python native-analyzer/tools/check_reference_audio.py \
  --audio /path/to/reference.ogg \
  --binary build/analyzer/install/ytaudio-analyzer \
  --models build/analyzer/install/models \
  --expect-bpm 130 --expect-tonic 'F#' --expect-scale major \
  --expect-steady --expect-clear-key --report build/analyzer/reference-result.json
```

Only pass the steady/clear-key expectations when appropriate to the reference.
The report records audio/executable hashes and results; the original is unchanged.

## Resource and package measurements

The resource measurements below predate the local-reference warning fixes;
their exact executable hash is retained in `build/analyzer/benchmark.json`.

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

| Target | Go build checked locally | Packaged native analyzer execution |
| --- | --- | --- |
| Linux amd64 | Desktop, native WebView smoke | Passed locally and in dev CI |
| Windows amd64 | Desktop cross-build | MSVC header fix awaiting native CI rerun |
| Windows arm64 | Desktop cross-build | MSVC header fix awaiting native CI rerun |
| macOS arm64, analyzer requires 13.4+ | Browser/core cross-build only | Packaged CLI/input checks passed dev CI; manual UI testing pending |
| Linux arm64 | Browser/core cross-build only | Packaged CLI/input checks passed dev CI; manual UI testing pending |

SDK archives for all five targets are hash-pinned. The native CI matrix builds
and executes unit/input/package smoke tests on actual target runners. Workflow
definitions are checked with actionlint. Production POSIX executables are stripped
before hashing/signing. Windows
packages exclude debug symbols and carry app-local permitted VC runtime DLLs; macOS
packages use loader-relative ORT paths and ad-hoc signatures; Linux uses an
executable-relative ORT path and the existing glibc release baselines. Inspection
of the pinned Apple Silicon SDK confirms minimum macOS 13.4 and
`@rpath/libonnxruntime.1.23.2.dylib`; its package ships that library once.

The first dev CI run exposed host-dependent KeyNet export bytes: numerical
parity passed, but the regenerated file did not match the local pinned hash.
Packaging now retains the canonical artifact and validates fresh exports
separately. Regression tests accept equivalent byte-different candidates and
reject changed inference, tampered canonical models and tampered beat models.
The canonical model and its Go/JSON digest pins remain unchanged.

Secret scanning uses the full default Gitleaks rules. `.gitleaks.toml` adds one
exception for the exact public KeyNet checksum in the two model-digest source
paths. The security job tests that other values in those files and that same
checksum outside those paths are still detected. Local history and current-source
scans pass, and the dev CI security check now passes too.
The security gate also identified GO-2026-6859 in Go 1.27.3; the module now
selects the fixed Go 1.27.4 toolchain for local builds and CI.

The following dev run passed model packaging, security, macOS ARM64 and both
Linux native jobs, but exposed an MSVC header conflict in the Windows builds.
Windows targets now define `NOMINMAX`; MSVC targets also use `/Zc:__cplusplus`
so r8brain selects its modern C++ implementation. The native platform regression
test includes the Windows SDK before r8brain/PocketFFT, checks the standard
version and absence of min/max macros, and exercises the resampler. Both Windows
architectures still need a fresh successful native CI run with these flags.

Before merging into `main`: run the dev CI matrix, test real native picker/drop,
installation/repair and offline analysis on Windows/Linux/macOS, check signed or
unsigned macOS Gatekeeper behavior, and evaluate independently labelled beats
covering the intended genres, sliding/distorted bass, changing keys/tempo and
mixed tunings. Real GitHub first-install behavior also needs a release containing
the new assets. This feature is ready for local development testing; these
remaining gates prevent a full production-readiness claim.
