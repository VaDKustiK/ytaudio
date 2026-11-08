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
