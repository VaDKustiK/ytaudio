#!/usr/bin/env bash
set -euo pipefail

: "${GOOS:?GOOS is required}"
: "${GOARCH:?GOARCH is required}"
: "${ASSET_DIR:?ASSET_DIR is required}"

mkdir -p "$ASSET_DIR"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

download() {
  curl --fail --location --retry 3 --silent --show-error "$1" -o "$2"
}

verify_sha256() {
  printf '%s  %s\n' "$1" "$2" | sha256sum --check -
}

case "$GOOS/$GOARCH" in
  linux/amd64) yt_name=yt-dlp_linux; deno_name=deno-x86_64-unknown-linux-gnu.zip; ffmpeg_name=ffmpeg-master-latest-linux64-gpl.tar.xz ;;
  linux/arm64) yt_name=yt-dlp_linux_aarch64; deno_name=deno-aarch64-unknown-linux-gnu.zip; ffmpeg_name=ffmpeg-master-latest-linuxarm64-gpl.tar.xz ;;
  windows/amd64) yt_name=yt-dlp.exe; deno_name=deno-x86_64-pc-windows-msvc.zip; ffmpeg_name=ffmpeg-master-latest-win64-gpl.zip ;;
  windows/arm64) yt_name=yt-dlp_arm64.exe; deno_name=deno-aarch64-pc-windows-msvc.zip; ffmpeg_name=ffmpeg-master-latest-winarm64-gpl.zip ;;
  darwin/amd64) yt_name=yt-dlp_macos; deno_name=deno-x86_64-apple-darwin.zip; ffmpeg_name= ;;
  darwin/arm64) yt_name=yt-dlp_macos; deno_name=deno-aarch64-apple-darwin.zip; ffmpeg_name= ;;
  *) echo "unsupported platform: $GOOS/$GOARCH" >&2; exit 1 ;;
esac

if [[ "$GOOS" == windows ]]; then
  executable_suffix=.exe
else
  executable_suffix=
fi

yt="$tmp/yt-dlp"
download "https://github.com/yt-dlp/yt-dlp/releases/latest/download/$yt_name" "$yt"
checksums="$tmp/SHA2-256SUMS"
download https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS "$checksums"
yt_sum="$(awk -v name="$yt_name" '$2 == name || $2 == "*" name {print $1}' "$checksums")"
test -n "$yt_sum"
verify_sha256 "$yt_sum" "$yt"
cp "$yt" "$ASSET_DIR/yt-dlp$executable_suffix"

deno_archive="$tmp/deno.zip"
download "https://github.com/denoland/deno/releases/latest/download/$deno_name" "$deno_archive"
deno_checksums="$tmp/deno.sha256"
download "https://github.com/denoland/deno/releases/latest/download/$deno_name.sha256sum" "$deno_checksums"
deno_sum="$(awk '{print $1}' "$deno_checksums")"
verify_sha256 "$deno_sum" "$deno_archive"
unzip -q "$deno_archive" -d "$tmp/deno"
cp "$tmp/deno/deno$executable_suffix" "$ASSET_DIR/deno$executable_suffix"

if test -n "$ffmpeg_name"; then
  ffmpeg_archive="$tmp/ffmpeg"
  download "https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/$ffmpeg_name" "$ffmpeg_archive"
  ffmpeg_checksums="$tmp/ffmpeg-checksums"
  download https://github.com/yt-dlp/FFmpeg-Builds/releases/download/latest/checksums.sha256 "$ffmpeg_checksums"
  ffmpeg_sum="$(awk -v name="$ffmpeg_name" '$2 == name {print $1}' "$ffmpeg_checksums")"
  test -n "$ffmpeg_sum"
  verify_sha256 "$ffmpeg_sum" "$ffmpeg_archive"
  mkdir "$tmp/ffmpeg-extracted"
  if [[ "$ffmpeg_name" == *.zip ]]; then
    unzip -q "$ffmpeg_archive" -d "$tmp/ffmpeg-extracted"
  else
    tar -xJf "$ffmpeg_archive" -C "$tmp/ffmpeg-extracted"
  fi
  ffmpeg_path="$(find "$tmp/ffmpeg-extracted" -type f -name "ffmpeg$executable_suffix" | head -n 1)"
  test -n "$ffmpeg_path"
  cp "$ffmpeg_path" "$ASSET_DIR/ffmpeg$executable_suffix"
fi

chmod 700 "$ASSET_DIR"/*
