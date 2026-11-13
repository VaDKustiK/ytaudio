#!/usr/bin/env python3
"""Fetch only hash-pinned artifacts into ignored build storage. No runtime Python."""
import argparse, hashlib, json, os, pathlib, platform, shutil, tarfile, tempfile, urllib.request, zipfile
ROOT=pathlib.Path(__file__).resolve().parents[2]
LOCK=json.loads((ROOT/'native-analyzer/sources.lock.json').read_text())

def fetch(name, cache):
    entry=LOCK[name]; path=cache/name
    if path.exists() and hashlib.sha256(path.read_bytes()).hexdigest()==entry['sha256']: return path
    path.parent.mkdir(parents=True,exist_ok=True)
    request=urllib.request.Request(entry['url'],headers={'User-Agent':'ytaudio-analyzer-build/1'})
    with tempfile.NamedTemporaryFile(dir=cache,delete=False) as output:
        temp=pathlib.Path(output.name)
        try:
            with urllib.request.urlopen(request,timeout=180) as response:
                digest=hashlib.sha256();total=0
                while chunk:=response.read(1024*1024):
                    total+=len(chunk)
                    if total>entry['size']: raise ValueError('Unexpected artifact size')
                    digest.update(chunk);output.write(chunk)
            if total!=entry['size'] or digest.hexdigest()!=entry['sha256']: raise ValueError(f'Integrity failure: {name}')
            output.close();temp.replace(path)
        finally: temp.unlink(missing_ok=True)
    return path

def extract(archive, target):
    # A fresh extraction avoids stale libraries, symlink aliases and mixed SDKs.
    if target.exists():shutil.rmtree(target)
    target.mkdir(parents=True,exist_ok=True)
    # Extract regular files only, stripping the archive's single root directory.
    if zipfile.is_zipfile(archive):
        with zipfile.ZipFile(archive) as source:
            for item in source.infolist():
                if item.is_dir():continue
                copy_member(item.filename,source.read(item),target)
    else:
        with tarfile.open(archive) as source:
            for item in source:
                if item.isfile():copy_member(item.name,source.extractfile(item).read(),target)

def copy_member(name, data, target):
    path=pathlib.PurePosixPath(name)
    if path.is_absolute() or '..' in path.parts or '\\' in name: raise ValueError('Unsafe archive path')
    if len(path.parts)<2:return
    destination=target.joinpath(*path.parts[1:]);destination.parent.mkdir(parents=True,exist_ok=True);destination.write_bytes(data)

def prepare(target,cache):
    deps=ROOT/'build/analyzer/deps';deps.mkdir(parents=True,exist_ok=True)
    for artifact,directory in [('beat-source.tar.gz','beat'),('key-source.tar.gz','key'),('pocketfft.tar.gz','pocketfft'),('r8brain.tar.gz','r8brain')]:
        extract(fetch(artifact,cache),deps/directory)
    (deps/'nlohmann').mkdir(exist_ok=True);shutil.copy2(fetch('json.hpp',cache),deps/'nlohmann/json.hpp')
    sdk={'linux-amd64':'linux-x64','linux-arm64':'linux-aarch64','windows-amd64':'win-x64','windows-arm64':'win-arm64','darwin-arm64':'osx-arm64'}[target]
    extension='zip' if sdk.startswith('win') else 'tgz'
    extract(fetch(f'onnxruntime-{sdk}-1.23.2.{extension}',cache),deps/'ort')
    # Tar SDKs use symlinks for their canonical SONAME. They were intentionally
    # not extracted; reconstruct only this known library alias from verified files.
    if not sdk.startswith('win'):
        lib=deps/'ort/lib';pattern='libonnxruntime*.dylib' if sdk.startswith('osx') else 'libonnxruntime.so.*'
        real=next(p for p in lib.glob(pattern) if p.is_file())
        name='libonnxruntime.dylib' if sdk.startswith('osx') else 'libonnxruntime.so'
        shutil.copy2(real,lib/name)
        if not sdk.startswith('osx'):shutil.copy2(real,lib/'libonnxruntime.so.1')
    models=ROOT/'build/analyzer/models';models.mkdir(parents=True,exist_ok=True)
    source=deps/'beat/onnx/beat_this.onnx'
    if hashlib.sha256(source.read_bytes()).hexdigest()!=LOCK['beat_this.onnx']['sha256']:raise ValueError('Upstream beat model hash mismatch')
    shutil.copy2(source,models/'beat_this.onnx')
    print(deps)

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--target',choices=['linux-amd64','linux-arm64','windows-amd64','windows-arm64','darwin-arm64']);p.add_argument('--cache',type=pathlib.Path,default=ROOT/'build/analyzer/downloads');p.add_argument('--reference',action='store_true')
    a=p.parse_args();target=a.target or {'Linux':'linux','Windows':'windows','Darwin':'darwin'}[platform.system()]+'-'+('arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64')
    prepare(target,a.cache)
    if a.reference:
        extract(fetch('beat-reference.tar.gz',a.cache),ROOT/'build/analyzer/reference')
        shutil.copy2(fetch('final0.ckpt',a.cache),ROOT/'build/analyzer/final0.ckpt')
