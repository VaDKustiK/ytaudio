# ytaudio

Local, private YouTube audio downloader. It runs in a native desktop window and makes requests from your computer.

## Run

```sh
go run -tags 'desktop production webkit2_41' ./cmd/ytaudio
```

The desktop app opens a native Wails window. On Linux, install GTK/WebKitGTK development packages before building locally (`libgtk-3-dev` and `libwebkit2gtk-4.1-dev` on Debian 13). Release builds embed verified yt-dlp, Deno, and FFmpeg assets where official checksums are available; development/browser builds can use tools installed on `PATH`. Deno 2.3+ is required by current yt-dlp YouTube extraction.

Browser fallback:

```sh
go run ./cmd/ytaudio
```

The application checks the public GitHub Releases page for newer versions and links to the release when one is available. It does not silently replace or restart itself.

The first yt-dlp download is large and may take a few minutes on a slow connection. Setup reports errors after 10 minutes.

For an unsigned download, choose **More info > Run anyway** on Windows, right-click and choose **Open** on macOS (or run `xattr -d com.apple.quarantine ytaudio`), and run `chmod +x ytaudio` on Linux. The macOS download is a universal Intel/Apple Silicon binary. If GitHub is unreachable, install `ffmpeg` and Deno, then place the verified yt-dlp binary in your user cache directory under `ytaudio/bin`.

Use only for personal content and respect copyright and YouTube's terms. See [SECURITY.md](SECURITY.md) for reporting security issues.
