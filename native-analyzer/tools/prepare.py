#!/usr/bin/env python3
"""Fetch only hash-pinned artifacts into ignored build storage. No runtime Python."""
import argparse, hashlib, http.client, json, os, pathlib, platform, shutil, sys, tarfile, tempfile, time, urllib.error, urllib.request, zipfile
ROOT=pathlib.Path(__file__).resolve().parents[2]
LOCK=json.loads((ROOT/'native-analyzer/sources.lock.json').read_text())
ORT_VERSION = '1.23.2'
ORT_PACKAGE = f'microsoft.ml.onnxruntime.{ORT_VERSION}.nupkg'
ORT_TARGETS = {
    'linux-amd64': 'linux-x64',
    'linux-arm64': 'linux-arm64',
    'windows-amd64': 'win-x64',
    'windows-arm64': 'win-arm64',
    'darwin-arm64': 'osx-arm64',
}

def prepare_key_model(models):
    # Exporting on each host is not guaranteed to produce identical ONNX bytes.
    # Ship the reviewed artifact; never learn a new trusted hash from build output.
    source=ROOT/'native-analyzer/models/keynet.onnx'
    expected=json.loads((ROOT/'native-analyzer/models.lock.json').read_text())['keynet.onnx']
    if hashlib.sha256(source.read_bytes()).hexdigest()!=expected:
        raise ValueError('Canonical key model hash mismatch')
    models.mkdir(parents=True,exist_ok=True)
    shutil.copy2(source,models/'keynet.onnx')

def download_verified(url, entry, path):
    request=urllib.request.Request(url,headers={'User-Agent':'ytaudio-analyzer-build/1'})
    with tempfile.NamedTemporaryFile(dir=path.parent,delete=False) as output:
        temp=pathlib.Path(output.name)
        try:
            with urllib.request.urlopen(request,timeout=180) as response:
                digest=hashlib.sha256();total=0
                while chunk:=response.read(1024*1024):
                    total+=len(chunk)
                    if total>entry['size']: raise ValueError('Unexpected artifact size')
                    digest.update(chunk);output.write(chunk)
            if total!=entry['size']: raise http.client.IncompleteRead(b'',entry['size']-total)
            if digest.hexdigest()!=entry['sha256']: raise ValueError(f'Integrity failure: {path.name}')
            output.close();temp.replace(path)
        finally:
            # Windows cannot remove an open temporary file after a failed read.
            output.close();temp.unlink(missing_ok=True)

def fetch(name, cache):
    entry=LOCK[name]; path=cache/name
    if path.exists() and path.stat().st_size==entry['size'] and hashlib.sha256(path.read_bytes()).hexdigest()==entry['sha256']: return path
    path.parent.mkdir(parents=True,exist_ok=True)
    # Mirrors are pinned in the lock file; every source must supply the exact
    # same bytes. Never accept a different checkpoint or skip a reference gate.
    pending=list(dict.fromkeys([entry['url'],*entry.get('mirrors',[])]))
    last_error=None
    for attempt in range(3):
        retry=[]
        for url in pending:
            try:
                download_verified(url,entry,path)
                return path
            except urllib.error.HTTPError as error:
                last_error=error
                if error.code in (408,429) or 500<=error.code<600: retry.append(url)
                print(f'{name}: download source returned HTTP {error.code}; trying remaining sources.',file=sys.stderr,flush=True)
                error.close()
            except (urllib.error.URLError,TimeoutError,ConnectionError,http.client.IncompleteRead) as error:
                last_error=error;retry.append(url)
                print(f'{name}: download interrupted; trying remaining sources.',file=sys.stderr,flush=True)
        pending=retry
        if not pending or attempt==2: break
        delay=2**attempt
        print(f'{name}: retrying download in {delay}s (attempt {attempt+2}/3).',file=sys.stderr,flush=True)
        time.sleep(delay)
    raise RuntimeError(f'Could not download {name} from its pinned sources: {last_error}. Retry when the provider is available, or place the exact hash-verified artifact in {path}.') from last_error

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

def extract_ort(archive, target, rid):
    """Stage a C++ SDK from the native files in Microsoft's NuGet package."""
    prefix = f'runtimes/{rid}/native'
    if rid.startswith('win-'):
        names = ('onnxruntime.dll', 'onnxruntime.lib', 'onnxruntime_providers_shared.dll')
        libraries = {name: name for name in names}
    elif rid.startswith('osx-'):
        libraries = {'libonnxruntime.dylib': f'libonnxruntime.{ORT_VERSION}.dylib'}
    else:
        libraries = {'libonnxruntime.so': 'libonnxruntime.so.1'}

    with tempfile.TemporaryDirectory(dir=target.parent, prefix='.ort-') as staging:
        sdk = pathlib.Path(staging) / 'sdk'
        (sdk / 'include').mkdir(parents=True)
        (sdk / 'lib').mkdir()
        with zipfile.ZipFile(archive) as source:
            members = {f'{prefix}/{name}': f'lib/{dest}' for name, dest in libraries.items()}
            members.update({name: name for name in ('LICENSE', 'ThirdPartyNotices.txt')})
            for name in source.namelist():
                parent, _, filename = name.rpartition('/')
                if parent == 'build/native/include' and filename.endswith('.h'):
                    members[name] = f'include/{filename}'
            if 'build/native/include/onnxruntime_cxx_api.h' not in members:
                raise ValueError('ONNX Runtime C++ headers are missing')
            for member, relative in members.items():
                if '\\' in relative or ':' in relative:
                    raise ValueError('Unsafe ONNX Runtime header name')
                with source.open(member) as data, (sdk / relative).open('wb') as output:
                    shutil.copyfileobj(data, output)

        # CMake links the unversioned name, while the OS loader uses the
        # versioned library name shipped in the runtime archive.
        for name, destination in libraries.items():
            if name != destination:
                shutil.copy2(sdk / 'lib' / destination, sdk / 'lib' / name)
        if target.exists():
            shutil.rmtree(target)
        sdk.rename(target)

def prepare(target,cache):
    deps=ROOT/'build/analyzer/deps';deps.mkdir(parents=True,exist_ok=True)
    for artifact,directory in [('beat-source.tar.gz','beat'),('key-source.tar.gz','key'),('pocketfft.tar.gz','pocketfft'),('r8brain.tar.gz','r8brain')]:
        extract(fetch(artifact,cache),deps/directory)
    (deps/'nlohmann').mkdir(exist_ok=True);shutil.copy2(fetch('json.hpp',cache),deps/'nlohmann/json.hpp')
    extract_ort(fetch(ORT_PACKAGE,cache),deps/'ort',ORT_TARGETS[target])
    models=ROOT/'build/analyzer/models';models.mkdir(parents=True,exist_ok=True)
    source=deps/'beat/onnx/beat_this.onnx'
    if hashlib.sha256(source.read_bytes()).hexdigest()!=LOCK['beat_this.onnx']['sha256']:raise ValueError('Upstream beat model hash mismatch')
    shutil.copy2(source,models/'beat_this.onnx')
    prepare_key_model(models)
    print(deps)

if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--target',choices=['linux-amd64','linux-arm64','windows-amd64','windows-arm64','darwin-arm64']);p.add_argument('--cache',type=pathlib.Path,default=ROOT/'build/analyzer/downloads');p.add_argument('--reference',action='store_true')
    a=p.parse_args();target=a.target or {'Linux':'linux','Windows':'windows','Darwin':'darwin'}[platform.system()]+'-'+('arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64')
    prepare(target,a.cache)
    if a.reference:
        extract(fetch('beat-reference.tar.gz',a.cache),ROOT/'build/analyzer/reference')
        shutil.copy2(fetch('final0.ckpt',a.cache),ROOT/'build/analyzer/final0.ckpt')
