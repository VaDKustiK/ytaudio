#!/usr/bin/env python3
"""Build desktop distribution assets on the target OS. Go, platform packaging
utilities, and Pillow are required on the build machine. Companion tools are
never embedded or included in packages."""
from prepare_icons import prepare_icons
import argparse, json, os, pathlib, plistlib, shutil, subprocess, sys, tarfile, time, zipfile

ROOT = pathlib.Path(__file__).resolve().parent.parent

def run(args, **kwargs):
    subprocess.run([str(a) for a in args], check=True, cwd=ROOT, **kwargs)


def package(goos, arch, version):
    version = version.removeprefix("v")
    if not version or any(c not in "0123456789.-abcdefghijklmnopqrstuvwxyz" for c in version):
        raise ValueError("Invalid version")
    dist = ROOT / "dist"; dist.mkdir(exist_ok=True)
    name = f"ytaudio-{version}-{goos}-{arch}"
    stage = dist / name
    if stage.exists():
        shutil.rmtree(stage)  # Never carry old bundled binaries/notices into a rebuild.
    stage.mkdir()
    binary = stage / ("ytaudio.exe" if goos == "windows" else "ytaudio")
    icons = prepare_icons(dist / "icons")
    env = dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED="0" if goos == "windows" else "1")
    tags = "desktop,production" + (",webkit2_41" if goos == "linux" else "")
    flags = f"-s -w -X main.version=v{version}" + (" -H windowsgui" if goos == "windows" else "")
    resource = ROOT / f"cmd/ytaudio/resource_windows_{arch}.syso"
    try:
        if goos == "windows":
            run(["goversioninfo", "-skip-versioninfo", "-icon", icons / "icon.ico", "-64", f"-arm={'true' if arch == 'arm64' else 'false'}", "-o", resource])
        run(["go", "build", "-trimpath", "-tags", tags, "-ldflags", flags, "-o", binary, "./cmd/ytaudio"], env=env)
    finally:
        if goos == "windows":
            resource.unlink(missing_ok=True)
    binary.chmod(0o755)
    for file in ("README.md", "LICENSE", "THIRD_PARTY_NOTICES.md"):
        shutil.copy2(ROOT / file, stage / file)
    notices = stage / "third-party"; notices.mkdir(exist_ok=True)
    shutil.copy2(ROOT / "THIRD_PARTY_NOTICES.md", notices / "THIRD_PARTY_NOTICES.md")
    # Include license texts of Go modules (including transitive dependencies).
    modules = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    while modules.strip():
        module, consumed = decoder.raw_decode(modules.lstrip())
        modules = modules.lstrip()[consumed:]
        if module.get("Main") or not module.get("Dir"):
            continue
        source = pathlib.Path(module["Dir"])
        destination = notices / (module["Path"].replace("/", "_") + "@" + module["Version"])
        for license_file in source.iterdir():
            if license_file.is_file() and license_file.name.upper().startswith(("LICENSE", "COPYING", "NOTICE", "AUTHORS")):
                destination.mkdir(exist_ok=True)
                shutil.copy2(license_file, destination / license_file.name)
    if goos == "windows":
        with zipfile.ZipFile(dist / f"{name}.zip", "w", zipfile.ZIP_DEFLATED) as z:
            for file in stage.rglob("*"):
                if file.is_file(): z.write(file, file.relative_to(stage.parent))
        # Per-user NSIS installer, no administrator access needed.
        source = ROOT / "packaging/installer.nsi"
        run(["makensis", f"-DVERSION={version}", f"-DARCH={arch}", f"-DICON={icons / 'icon.ico'}", f"-DSTAGE={stage}", f"-DOUTFILE={dist / (name + '-setup.exe')}", source])
    elif goos == "darwin":
        app = stage / "ytaudio.app"; contents = app / "Contents"; (contents / "MacOS").mkdir(parents=True, exist_ok=True);(contents / "Resources").mkdir(exist_ok=True)
        shutil.move(binary, contents / "MacOS/ytaudio")
        (contents / "Info.plist").write_bytes(plistlib.dumps({"CFBundleName":"ytaudio", "CFBundleDisplayName":"ytaudio", "CFBundleIdentifier":"io.github.vadkustik.ytaudio", "CFBundleExecutable":"ytaudio", "CFBundlePackageType":"APPL", "CFBundleShortVersionString":version,"CFBundleVersion":version,"NSHighResolutionCapable":True,"LSMinimumSystemVersion":"11.0","CFBundleIconFile":"icon.icns"}))
        shutil.copytree(notices, contents / "Resources/third-party", dirs_exist_ok=True)
        shutil.copy2(stage / "LICENSE", contents / "Resources/LICENSE")
        shutil.copy2(icons / "icon.icns", contents / "Resources/icon.icns")
        identity=os.environ.get("APPLE_SIGN_IDENTITY","-")
        run(["codesign","--force","--deep","--sign",identity,app])
        dmgroot=dist / f"{name}-dmg"
        if dmgroot.exists(): shutil.rmtree(dmgroot)
        dmgroot.mkdir();shutil.copytree(app,dmgroot / app.name)
        link=dmgroot / "Applications"
        if not link.exists():link.symlink_to("/Applications")
        run(["hdiutil","create","-volname","ytaudio","-srcfolder",dmgroot,"-ov","-format","UDZO",dist / f"{name}.dmg"])
        run(["ditto","-c","-k","--keepParent",app,dist / f"{name}.zip"])
    else:
        with tarfile.open(dist / f"{name}.tar.gz", "w:gz") as tar:
            tar.add(stage,arcname=name)
        # nFPM produces both native formats; packages declare the WebView runtime.
        common={"name":"ytaudio","arch":arch,"platform":"linux","version":version,"maintainer":"ytaudio maintainers","description":"Simple, local YouTube audio downloads","homepage":"https://github.com/VaDKustiK/ytaudio","license":"MIT","contents":[{"src":str(binary),"dst":"/usr/bin/ytaudio"},{"src":str(ROOT / "packaging/ytaudio.desktop"),"dst":"/usr/share/applications/ytaudio.desktop"},{"src":str(icons / "ytaudio.png"),"dst":"/usr/share/icons/hicolor/512x512/apps/ytaudio.png"},{"src":str(stage / "LICENSE"),"dst":"/usr/share/licenses/ytaudio/LICENSE"},{"src":str(notices),"dst":"/usr/share/doc/ytaudio/third-party","type":"tree"}]}
        for kind,depends in (("deb",["libgtk-3-0 | libgtk-3-0t64","libwebkit2gtk-4.1-0"]),("rpm",["libgtk-3.so.0()(64bit)","libwebkit2gtk-4.1.so.0()(64bit)"])):
            cfg={**common,"depends":depends};path=dist / f"nfpm-{arch}-{kind}.json";path.write_text(json.dumps(cfg));run(["nfpm","package","--config",path,"--packager",kind,"--target",dist / f"{name}.{kind}"])
        archroot=dist / f"arch-{arch}"
        if archroot.exists(): shutil.rmtree(archroot)
        (archroot / "usr/bin").mkdir(parents=True,exist_ok=True)
        shutil.copy2(binary,archroot / "usr/bin/ytaudio")
        for src,dst in [(ROOT / "packaging/ytaudio.desktop",archroot / "usr/share/applications/ytaudio.desktop"),(icons / "ytaudio.png",archroot / "usr/share/icons/hicolor/512x512/apps/ytaudio.png")]:
            dst.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(src,dst)
        (archroot / "usr/share/licenses/ytaudio").mkdir(parents=True, exist_ok=True)
        shutil.copy2(stage / "LICENSE", archroot / "usr/share/licenses/ytaudio/LICENSE")
        shutil.copytree(notices,archroot / "usr/share/doc/ytaudio/third-party",dirs_exist_ok=True)
        (archroot / ".PKGINFO").write_text(f"pkgname = ytaudio\npkgbase = ytaudio\npkgver = {version}-1\npkgdesc = Simple local YouTube audio downloader\nurl = https://github.com/VaDKustiK/ytaudio\nbuilddate = {int(time.time())}\npackager = ytaudio maintainers\nsize = {binary.stat().st_size}\narch = {'x86_64' if arch=='amd64' else 'aarch64'}\nlicense = MIT\ndepend = gtk3\ndepend = webkit2gtk-4.1\n")
        run(["tar","--owner=0","--group=0","--numeric-owner","--mode=u+rwX,go+rX,go-w","--zstd","-cf",dist / f"{name}.pkg.tar.zst","-C",archroot,".PKGINFO","usr"])
    print(f"Built {name} in {dist}")

if __name__ == "__main__":
    p=argparse.ArgumentParser();p.add_argument("--os",required=True,choices=["windows","darwin","linux"]);p.add_argument("--arch",required=True,choices=["amd64","arm64"]);p.add_argument("--version",required=True);a=p.parse_args();package(a.os,a.arch,a.version)
