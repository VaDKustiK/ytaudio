# Third-party software

ytaudio's Go application code is MIT licensed. The release packages include
license texts for its Go dependencies under `third-party/`. Wails is MIT licensed:
https://github.com/wailsapp/wails . System GTK/WebKit and Microsoft WebView2
retain their own licenses and are provided by the operating system/runtime.

## Separately downloaded tools

App releases do not embed, bundle, mirror, or redistribute yt-dlp, FFmpeg,
FFprobe, or Deno executables. On first launch, the app downloads missing tools
directly from their providers to the user's cache and verifies their SHA-256
checksums before activation. Existing compatible tools can also be used.
These separate programs retain their own licenses, including licenses of
libraries included in their standalone builds:

- yt-dlp: Unlicense for its source; standalone builds have additional licenses.
  https://github.com/yt-dlp/yt-dlp/blob/master/LICENSE
  https://github.com/yt-dlp/yt-dlp/blob/master/THIRD_PARTY_LICENSES.txt
- Deno: MIT, with additional bundled component notices.
  https://github.com/denoland/deno/blob/main/LICENSE.md
- FFmpeg/FFprobe: LGPL or GPL depending on the build configuration and linked
  libraries. https://ffmpeg.org/legal.html

Pinned macOS, Linux, and Windows x64 FFmpeg downloads come from
https://github.com/eugeneware/ffmpeg-static/releases/tag/b6.1.1 . Their upstream
README/license assets document build provenance. Windows ARM64 downloads come
from https://github.com/yt-dlp/FFmpeg-Builds . Exact platform URLs and pinned
hashes are recorded in `internal/deps/pins.go` and `scripts/sources.lock.json`.
Other tool versions are resolved from provider releases with published checksums.

The local media-test preparation script is a developer utility, not a release
bundling step. Its downloaded executables are ignored by Git and never embedded.
If you redistribute any downloaded binaries yourself, preserve their notices
and meet their specific licenses, including complete corresponding source for
the exact FFmpeg build where required. A generic FFmpeg source link, checksum,
or copy of the application source does not meet that source requirement.

## Optional offline music analyzer

The main app installer does not contain the native analyzer or its models. They
are separate versioned release assets, installed on request and verified before
use. After installation, analysis needs no network access and never uploads audio.
The analyzer packages redistribute these components with their license texts:

- Beat This! code and published final0 weights: MIT. Official source:
  https://github.com/CPJKU/beat_this . Training audio has separate licenses and
  is not distributed by ytaudio.
- beat_this_cpp preprocessing/inference components: MIT.
  https://github.com/mosynthkey/beat_this_cpp
- MusicalKeyCNN architecture and keynet checkpoint from its MIT-licensed
  repository: https://github.com/a1ex90/MusicalKeyCNN . No separate checkpoint
  license was found in the pinned revision; the repository license does not
  license or transfer ownership of the training audio.
- ONNX Runtime CPU 1.23.2: MIT, with its own third-party notices.
  https://github.com/microsoft/onnxruntime
- PocketFFT: BSD-3-Clause. https://github.com/mreineck/pocketfft
- r8brain-free-src: MIT. https://github.com/avaneev/r8brain-free-src
- nlohmann/json 3.12.0: MIT. https://github.com/nlohmann/json
- Native CQT adaptation of librosa 0.11.0: ISC. The license is preserved under
  `native-analyzer/licenses/`. https://github.com/librosa/librosa

Windows native packages also contain the required Microsoft Visual C++ runtime
DLLs from Visual Studio's permitted redistributable directory. These remain
Microsoft software under its redistribution terms, not MIT-licensed app code:
https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files

Exact source revisions, artifact SHA-256 hashes, model exports and verification
commands are recorded in `native-analyzer/`. Python, PyTorch, librosa and their
Python environments are development/export tools and are not included in the
installed analyzer. FFmpeg remains an independently downloaded executable and is
not linked into, or redistributed in, the analyzer packages.
