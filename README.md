# ytaudio

A local desktop app for saving YouTube audio. No account, hosted download server,
analytics, or telemetry. It uses established tools: yt-dlp, FFmpeg/FFprobe, Deno,
and Wails. Requests come from your computer.

## Using the app

1. Install the package for your operating system and processor.
2. Open **ytaudio**. Download tools install automatically on first launch; an internet connection is required. If setup fails, check **Info** and choose **Set up / repair**.
3. Select your download folder in **Settings**.
4. Paste a video or playlist link, choose a format, and select **Download audio**.

The queue runs one job at a time. You can cancel a job or dismiss its result.
There is no saved history: the queue disappears when the app closes. Playlists
require confirmation and contain at most 100 videos. The optional session job
limit is a courtesy control, not a guarantee against YouTube rate limits.

| Format | What it does |
| --- | --- |
| MP3 | Best-quality VBR MP3. Broad compatibility; a lossy conversion. |
| WAV | Uncompressed decoded audio. Large files, no additional lossy codec. |
| FLAC | Losslessly encodes the decoded audio. Does not restore source quality. |
| OGG | Highest-quality Vorbis conversion; another lossy encoding step. |

Every format starts with the best audio available from YouTube. YouTube has
already compressed the uploader's audio. MP3/OGG conversion cannot promise
identical quality; WAV/FLAC cannot recover information YouTube removed. Text
metadata is added when supported. Artwork is never downloaded or embedded;
each download saves only the audio file.

**Updates:** the bell lists newer stable GitHub releases. Selecting one opens
its release page in your browser so you can download the new installer. Updates
are optional and are checked once per launch; **Info** also has a manual check.
In-place application replacement/restart is not implemented. yt-dlp is checked
at most once per 24 hours when the GitHub API is available, updated while idle,
and updated/retried once after an eligible extraction error. Info also offers
an explicit yt-dlp update and missing-tool repair.

## Downloads and platform support

Build outputs are placed in `dist/`:

- **Windows 11 / 10:** per-user `*-setup.exe` and portable ZIP; x64 and ARM64.
  The app uses Microsoft WebView2. Windows 11 normally includes it; on older
  Windows installations, follow Wails' missing-runtime prompt if it appears.
  Unsigned builds can trigger SmartScreen. Prefer signed releases for friends.
- **macOS (Apple Silicon):** `.app` inside a DMG, plus ZIP. Drag the app into
  Applications. Local/CI builds are ad-hoc signed
  unless a signing identity is provided. Developer ID signing and notarization
  need the maintainer's Apple credentials before frictionless distribution.
- **Ubuntu, Debian, Mint:** install the DEB, for example
  `sudo apt install ./ytaudio-0.2.0-linux-amd64.deb`.
- **Fedora:** install the RPM with `sudo dnf install ./ytaudio-0.2.0-linux-amd64.rpm`.
- **Arch:** install the matching `.pkg.tar.zst` with `sudo pacman -U <file>`.
- **Other Linux systems:** extract the portable TAR archive. GTK 3 and
  WebKitGTK 4.1 must be installed. DEB/RPM/Arch packages declare those runtime
  requirements; a portable archive does not bundle the GUI libraries.

Linux builds require glibc and a compatible GTK/WebKit stack. x64 release CI
uses Ubuntu 22.04, ARM64 uses Ubuntu 24.04. An AppImage is not currently produced.
A successful build does not replace fresh-install testing on the intended OS.

## Build locally

Go 1.26+ is required; the module selects Go 1.27.3 automatically. Linux builds
also need a C compiler, `pkg-config`, GTK 3, and WebKitGTK 4.1 development files
(`libgtk-3-dev`, `libwebkit2gtk-4.1-dev` on Debian/Ubuntu).

```sh
go run -tags 'desktop production webkit2_41' ./cmd/ytaudio
```

To rebuild the local Linux executable without assigning a release version:

```sh
go build -tags 'desktop production webkit2_41' -o ytaudio ./cmd/ytaudio
./ytaudio
```

Local builds show **Development build** and skip app-release checks. Packaged
releases receive their version from the release workflow or the explicit
`--version` packaging argument; there is no fixed release version in the app.

On Windows/macOS, omit `webkit2_41`. Windows builds do not require CGO;
macOS/Linux use native libraries and CGO. The default browser mode is available
for development: `go run ./cmd/ytaudio --no-browser`. It binds only to 127.0.0.1.

For a Linux x64 package:

```sh
python3 -m pip install Pillow==13.0.0
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.45.2
python3 scripts/package.py --os linux --arch amd64 --version v0.2.0
```

Make sure Go's bin directory is on PATH. Packaging Linux also needs `tar` and
`zstd`; Windows packaging uses NSIS and
`go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0`.
macOS packaging uses `codesign`, `ditto`, and `hdiutil`. All package icons are
generated from `packaging/ytaudiologo.png`; no extra icon source files are needed.

Main application packages contain the app and its Go dependency license notices,
**no companion executables**. The optional analyzer has separate release assets. Missing tools download directly from their providers
into the user's private cache on first launch. Downloads are SHA-256 verified
before activation; existing working tools remain available after failed updates.
FFmpeg is a separate process, not linked into the app. Do not add cached tools
to release archives: redistributing them requires meeting their own licenses,
including corresponding-source obligations for the exact FFmpeg builds.

The `Release` workflow builds all five targets after validation. A push to the
production `main` branch publishes `v0.2.<workflow run number>` only after all
builds succeed. A `vX.Y.Z` tag or manual workflow can select a version instead.
Use monotonically increasing versions for update notifications. Feature work
should stay on other branches until it is ready for friends.

## Validation

```sh
go test -race ./...
go vet ./...
go vet -tags 'desktop production webkit2_41' ./...
YTAUDIO_MEDIA_TESTS=1 go test ./internal/ytdlp -run TestLocalAudioConversion -v
```

Prepare local media-test tools separately (they are never embedded):

```sh
python3 scripts/prepare_assets.py --os linux --arch amd64 --output internal/deps/assets/linux-amd64
```

The media test uses these Linux tools and a generated local sine wave,
without contacting YouTube. UI regressions use Python Playwright:

```sh
pip install playwright==1.64.1
python -m playwright install chromium
go build -o /tmp/ytaudio ./cmd/ytaudio
python scripts/ui_test.py --binary /tmp/ytaudio
```

The native Linux WebView test can be built/run with
`go run -tags 'desktop production webkit2_41' ./scripts/desktop-smoke`.
Give tests isolated config/cache directories using `YTAUDIO_CONFIG_DIR` and
`YTAUDIO_CACHE_DIR` (the UI/live test scripts do this automatically). To test the UI offline without
automatic first-launch tool downloads, set `YTAUDIO_AUTO_SETUP=0`.

## Troubleshooting and privacy

Use **Info → Copy diagnostics** and send the result to the maintainer. The
latest download error is retained as a bounded diagnostic log. The app redacts
its known home/output paths and configured proxy URL; inspect copied logs
before sharing them. No diagnostics are uploaded automatically.

If a video fails, try **Info → Update yt-dlp**. For missing/broken tools, use
**Set up / repair**. Bot checks, private/age-restricted videos, region blocks,
and rate limits may still prevent a download. The app does not import browser
cookies or bypass sign-in. Check your connection and optional proxy setting.

If GitHub is unreachable, place verified native `yt-dlp`, `ffmpeg`, `ffprobe`,
and `deno` executables in the user's cache directory under `ytaudio/bin`; on
Linux that is usually `~/.cache/ytaudio/bin`. Settings live in the platform's
user config directory under `ytaudio/config.json`. Updated tools are preserved
across app restarts. Do not delete the cache while a download is running.

Network access includes YouTube and its media/image delivery domains, GitHub's
API and release/CDN endpoints for tool/app updates, and any configured proxy.
The operating system's WebView may have its own runtime update behavior.
There is no central download relay. See
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

The browser development server accepts local requests only and authenticates
changes with a per-launch token. Keep the app and its tools updated. Report
security concerns privately to the repository maintainer, without posting secrets
or personal download logs in public issues.

Use only where you have the necessary permission and respect copyright and
YouTube's terms. Desktop distribution does not itself grant downloading rights.

## BPM, key and tuning (development)

The **BPM and key detector** accepts local audio through Choose audio or drag
and drop. A completed download's **BPM and key detection** button opens it and starts analysis
on the actual saved file. Analysis never edits the source audio.

The optional native component downloads separately; the main app installer stays
small. Once the analyzer and FFmpeg are installed, local-file analysis works
completely offline. Tempo/key/tuning are estimates: half/double-time, relative
major/minor, mixed tuning, and uncertain results can be musically ambiguous.
The displayed and copied BPM prefer the faster of the two tempo interpretations,
rounded to a whole number. The slower alternative is hidden.
The detector supports the existing five CPU/OS targets. The analyzer's macOS
package targets macOS 13.4+, and native Windows/macOS/ARM64 validation must pass
before releasing this feature. See [native analyzer](native-analyzer/README.md)
for the exact validation status and measured limitations.

Local Linux development (Python is for building/exporting only):

```sh
uv venv --python 3.13 .venv-analyzer
uv pip install --python .venv-analyzer/bin/python torch==2.9.1 torchaudio==2.9.1 --index-url https://download.pytorch.org/whl/cpu
uv pip install --python .venv-analyzer/bin/python --index-strategy unsafe-best-match -r native-analyzer/tools/requirements.lock.txt
.venv-analyzer/bin/python native-analyzer/tools/package.py
# Existing local build tools also need a C++20 compiler and CMake 3.20+.
go build -tags 'desktop production webkit2_41' -o ytaudio ./cmd/ytaudio
./ytaudio
```

An unversioned executable next to `build/analyzer/install` finds that local
component automatically. For `go run`, browser builds elsewhere, or custom build
locations, set `YTAUDIO_ANALYZER_DIR` to the absolute component directory.
Performance settings apply to the next analysis; all modes use the same models.
Native drag and drop uses the original file path. The browser development UI
uses a temporary upload (256 MB limit), cleaned up when the app closes.

`dev` runs analyzer validation/build jobs without publishing releases. The release
workflow includes separate analyzer/runtime and shared-model assets only after
all required builds and validation jobs succeed.
