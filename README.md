# ytaudio

A desktop app for downloading YouTube and SoundCloud audio and finding its
BPM, musical key, and tuning. Processing runs on your computer.

## Download

Get the latest version from [Releases](https://github.com/VaDKustiK/ytaudio/releases/latest).

| System | Choose |
| --- | --- |
| Windows 10/11 | `windows-amd64-setup.exe` for most PCs; `windows-arm64-setup.exe` for ARM PCs. Portable ZIPs are also available. |
| macOS, Apple Silicon | `darwin-arm64.dmg`, then drag ytaudio into Applications. The analyzer requires macOS 13.4 or later. |
| Linux, x64 or ARM64 | `.deb` for Debian/Ubuntu/Mint, `.rpm` for Fedora, `.pkg.tar.zst` for Arch, or `.tar.gz` for a portable copy. |

`amd64` means x64 for Intel and AMD processors. `arm64` means ARM processors.
Linux needs GTK 3 and WebKitGTK 4.1; Windows needs WebView2. Unsigned installers
may show a Windows/macOS security prompt.

## Use

1. Open the app and let the download tools install on first launch.
2. Choose your download folder in **Settings**.
3. Open **YouTube downloader** or **SoundCloud downloader**, paste a link,
   choose MP3, WAV, FLAC, or OGG, and select **Download audio**.

Tracks and playlists are supported. Playlists ask for confirmation and are
limited to 100 items. Downloads run one at a time. The session list clears when you close the app.

Downloads start with the best full audio available without signing in. MP3 and
OGG add a lossy conversion; WAV and FLAC preserve the decoded audio but cannot
restore quality lost in the source. SoundCloud previews and tracks that require
an account or subscription show an error instead of saving a preview.

## Updates and troubleshooting

The bell checks for releases at startup. Starting with v1.1.0, choose **Install
update** from the bell or **Info** to download a verified update and reopen the
app. Updates are optional. Older versions need one last manual installation.
On Linux, portable copies update in place. System packages use PackageKit where
available and may prompt for approval. On other setups, use the release link and
your package manager.

In **Info**, use **Update yt-dlp** for extraction problems, **Set up / repair**
for broken or missing tools, and **Copy diagnostics** to report a problem.
Review copied logs before sharing them. Platform restrictions, unavailable
tracks, bot checks, and rate limits can still prevent a download.

## Tools and models

| Component | Purpose |
| --- | --- |
| Go + Wails | Desktop interface and application backend. |
| yt-dlp | YouTube and SoundCloud audio downloads. |
| FFmpeg / FFprobe | Audio conversion, decoding, and file inspection. |
| Deno | JavaScript support for YouTube extraction. |
| Beat This! | Beat tracking and BPM detection. |
| MusicalKeyCNN (KeyNet) | Musical key detection. |
| ONNX Runtime | Runs the BPM/key models locally on the CPU. |
| Native C++ signal processing | Tuning and detune estimation. |

## Development and releases

Use Go 1.26+; the module selects the required toolchain. On Linux, install a C
compiler, `pkg-config`, `libgtk-3-dev`, and `libwebkit2gtk-4.1-dev`, then run:

```sh
go run -tags 'desktop production webkit2_41' ./cmd/ytaudio
```

Omit `webkit2_41` on Windows/macOS. For browser development, use
`go run ./cmd/ytaudio --no-browser`. For a local Linux executable:

```sh
go build -tags 'desktop production webkit2_41' -o ytaudio ./cmd/ytaudio
./ytaudio
```

Local builds show **Development build**. See the [analyzer guide](native-analyzer/README.md)
for its separate build requirements, model licenses, and validation commands.
Download tools are installed separately and verified before use; do not bundle
cached executables into app packages. See [third-party notices](THIRD_PARTY_NOTICES.md).

## License

The application is [MIT licensed](LICENSE). Dependencies and models retain their
own licenses. Download only content you have permission to save and respect the
platforms' terms.
