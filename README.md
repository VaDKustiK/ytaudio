# ytaudio

Local, private YouTube audio downloader. It runs in a native desktop window and makes requests from your computer.

## Run

```sh
go run -tags desktop ./cmd/ytaudio
```

The desktop app opens a native Wails window. On Linux, install WebKitGTK development/runtime packages first. The app binds its internal handler to the local Wails window. It downloads yt-dlp from its official GitHub release and verifies its SHA256 checksum. Install `ffmpeg` and Deno 2.3+ with your OS package manager or the official [Deno installer](https://docs.deno.com/runtime/getting_started/installation/) before setup. Deno is required by current yt-dlp YouTube extraction.

Browser fallback:

```sh
go run ./cmd/ytaudio
```

Release packaging is being migrated to embed verified yt-dlp, Deno, and FFmpeg assets and extract them into the private user cache on first launch.

The first yt-dlp download is large and may take a few minutes on a slow connection. Setup reports errors after 10 minutes.

For an unsigned download, choose **More info > Run anyway** on Windows, right-click and choose **Open** on macOS (or run `xattr -d com.apple.quarantine ytaudio`), and run `chmod +x ytaudio` on Linux. The macOS download is a universal Intel/Apple Silicon binary. If GitHub is unreachable, install `ffmpeg` and Deno, then place the verified yt-dlp binary in your user cache directory under `ytaudio/bin`.

Use only for personal content and respect copyright and YouTube's terms. See [SECURITY.md](SECURITY.md) for reporting security issues.
