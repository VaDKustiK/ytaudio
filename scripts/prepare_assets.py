#!/usr/bin/env python3
"""Fetch verified companion tools for local media tests only. Never package these
executables in app releases; the app downloads tools directly on first launch.
Sources: GitHub releases of yt-dlp/yt-dlp, denoland/deno,
eugeneware/ffmpeg-static, and yt-dlp/FFmpeg-Builds (Windows ARM64).
All assets are verified using release SHA-256 digests, or yt-dlp SHA2-256SUMS.
"""
import argparse, hashlib, io, json, os, pathlib, shutil, tempfile, urllib.request, urllib.error, zipfile


def get(url):
    request = urllib.request.Request(url, headers={"User-Agent": "ytaudio-release"})
    if url.startswith("https://api.github.com/") and os.environ.get("GH_TOKEN"):
        request.add_header("Authorization", "Bearer " + os.environ["GH_TOKEN"])
    with urllib.request.urlopen(request, timeout=180) as response:
        return response.read()


def release(repo, tag=None):
    try:
        info = ({"tag_name":tag,"assets":[]} if repo=="eugeneware/ffmpeg-static" else json.loads(get(f"https://api.github.com/repos/{repo}/releases/" + (f"tags/{tag}" if tag else "latest"))))
    except urllib.error.HTTPError as error:
        if error.code != 403: raise
        # Public release downloads remain available when the unauthenticated API
        # quota is exhausted. Continue to require upstream/pinned checksums.
        info = {"tag_name": tag or "latest", "assets": []}
    info["repo"] = repo
    return info


def asset(info, name):
    entry = next((a for a in info["assets"] if a["name"] == name), None)
    if not entry:
        repo, tag = info["repo"], info["tag_name"]
        base = f"https://github.com/{repo}/releases/" + ("latest/download/" if tag == "latest" and repo != "yt-dlp/FFmpeg-Builds" else f"download/{tag}/")
        data = get(base + name)
        if repo == "eugeneware/ffmpeg-static":
            expected = json.loads((pathlib.Path(__file__).parent / "sources.lock.json").read_text())[name]
        else:
            checksum_name = "SHA2-256SUMS" if repo == "yt-dlp/yt-dlp" else "checksums.sha256" if repo == "yt-dlp/FFmpeg-Builds" else name + ".sha256sum"
            sums = get(base + checksum_name).decode()
            expected = next(line.split()[0] for line in sums.splitlines() if len(line.split()) >= 2 and line.split()[1].lstrip("*") == name)
        if hashlib.sha256(data).hexdigest() != expected: raise RuntimeError(f"Checksum mismatch: {name}")
        return data, {"asset":name,"url":base+name,"sha256":expected,"release":tag}
    data = get(entry["browser_download_url"])
    digest = entry.get("digest", "")
    if digest.startswith("sha256:"):
        expected = digest[7:]
    elif name.startswith("yt-dlp"):
        sums = get(next(a["browser_download_url"] for a in info["assets"] if a["name"] == "SHA2-256SUMS")).decode()
        expected = next(line.split()[0] for line in sums.splitlines() if len(line.split()) == 2 and line.split()[1].lstrip("*") == name)
    else:
        raise RuntimeError(f"No SHA-256 digest for {name}")
    if hashlib.sha256(data).hexdigest() != expected:
        raise RuntimeError(f"Checksum mismatch: {name}")
    return data, {"asset": name, "url": entry["browser_download_url"], "sha256": expected, "release": info["tag_name"]}


def prepare(goos, arch, output):
    output = pathlib.Path(output)
    output.mkdir(parents=True, exist_ok=True)
    ext = ".exe" if goos == "windows" else ""
    manifest = []
    def save(name, payload, record):
        target = output / name
        with tempfile.NamedTemporaryFile(dir=output, delete=False) as temp:
            temp.write(payload)
            path = pathlib.Path(temp.name)
        path.chmod(0o755)
        path.replace(target)
        manifest.append({"installed": name, **record})
    yt_names = {("windows", "amd64"): "yt-dlp.exe", ("windows", "arm64"): "yt-dlp_arm64.exe", ("linux", "amd64"): "yt-dlp_linux", ("linux", "arm64"): "yt-dlp_linux_aarch64", ("darwin", "amd64"): "yt-dlp_macos", ("darwin", "arm64"): "yt-dlp_macos"}
    data, record = asset(release("yt-dlp/yt-dlp"), yt_names[(goos, arch)])
    save("yt-dlp" + ext, data, record)
    target_arch = "x86_64" if arch == "amd64" else "aarch64"
    triple = {"windows": "pc-windows-msvc", "darwin": "apple-darwin", "linux": "unknown-linux-gnu"}[goos]
    data, record = asset(release("denoland/deno", "v2.9.8"), f"deno-{target_arch}-{triple}.zip")
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        save("deno" + ext, archive.read("deno" + ext), record)
    if (goos, arch) == ("windows", "arm64"):
        data, record = asset(release("yt-dlp/FFmpeg-Builds", "latest"), "ffmpeg-master-latest-winarm64-gpl.zip")
        with zipfile.ZipFile(io.BytesIO(data)) as archive:
            for name in ("ffmpeg.exe", "ffprobe.exe"):
                path = next(n for n in archive.namelist() if pathlib.PurePosixPath(n).name == name)
                save(name, archive.read(path), record)
            for name in archive.namelist():
                if pathlib.PurePosixPath(name).name.startswith(("LICENSE", "COPYING")):
                    (output / pathlib.PurePosixPath(name).name).write_bytes(archive.read(name))
    else:
        info = release("eugeneware/ffmpeg-static", "b6.1.1")
        platform = "win32" if goos == "windows" else goos
        ffarch = "x64" if arch == "amd64" else "arm64"
        for tool in ("ffmpeg", "ffprobe"):
            data, record = asset(info, f"{tool}-{platform}-{ffarch}")
            save(tool + ext, data, record)
        for suffix in ("LICENSE", "README"):
            data, _ = asset(info, f"{platform}-{ffarch}.{suffix}")
            (output / f"FFmpeg.{suffix}").write_bytes(data)
    # Notices for the applications and the libraries bundled in yt-dlp's standalone builds.
    for name, url in {
        "yt-dlp.LICENSE": "https://raw.githubusercontent.com/yt-dlp/yt-dlp/master/LICENSE",
        "yt-dlp.THIRD_PARTY_LICENSES.txt": "https://raw.githubusercontent.com/yt-dlp/yt-dlp/master/THIRD_PARTY_LICENSES.txt",
        "Deno.LICENSE": "https://raw.githubusercontent.com/denoland/deno/v2.9.8/LICENSE.md",
    }.items():
        (output / name).write_bytes(get(url))
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(f"Verified tools prepared for {goos}/{arch}: {output}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--os", required=True, choices=["windows", "darwin", "linux"])
    parser.add_argument("--arch", required=True, choices=["amd64", "arm64"])
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    prepare(args.os, args.arch, args.output)
