#!/usr/bin/env python3
"""Build/package the native component separately from the main app installer."""
import argparse,hashlib,json,math,pathlib,platform,shutil,struct,subprocess,sys,tempfile,zipfile
from prepare import ROOT,prepare
METADATA=json.loads((ROOT/"packaging/analyzer.json").read_text())
ENGINE_VERSION=METADATA["engine_version"]
MODELS_VERSION=METADATA["models_version"]

def run(args):subprocess.run([str(x) for x in args],check=True,cwd=ROOT)
def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def manifest(directory,engine=False):
    files={str(p.relative_to(directory)).replace('\\','/'):digest(p) for p in sorted(directory.rglob('*')) if p.is_file() and p.name!='manifest.json'}
    data={'version':'1','models_version':MODELS_VERSION,'files':files}
    if engine:data['engine_version']=ENGINE_VERSION
    (directory/'manifest.json').write_text(json.dumps(data,indent=2)+'\n')
def archive(directory,destination):
    with zipfile.ZipFile(destination,'w',zipfile.ZIP_DEFLATED,compresslevel=6) as z:
        for file in sorted(directory.rglob('*')):
            if file.is_file():z.write(file,str(file.relative_to(directory)).replace('\\','/'))
def package(goos,arch,skip_prepare,skip_export,python,models_only=False):
    target=f'{goos}-{arch}';deps=ROOT/'build/analyzer/deps';models=ROOT/'build/analyzer/models';build=ROOT/'build/analyzer/cmake'
    if not skip_prepare:prepare(target,ROOT/'build/analyzer/downloads')
    if not skip_export:run([python,ROOT/'native-analyzer/tools/export_key.py','--source',deps/'key','--output',models/'keynet.onnx'])
    expected=json.loads((ROOT/'native-analyzer/models.lock.json').read_text())
    if expected['version']!=MODELS_VERSION:raise ValueError('Model version metadata mismatch')
    for name in ('beat_this.onnx','keynet.onnx'):
        if digest(models/name)!=expected[name]:raise ValueError(f'Model hash mismatch: {name}')
    notices=models/'licenses';notices.mkdir(exist_ok=True)
    shutil.copy2(deps/'beat/LICENSE',notices/'beat-this-cpp-MIT.txt')
    # The official project explicitly licenses final0 weights MIT.
    shutil.copy2(ROOT/'native-analyzer/licenses/beat-this.txt',notices/'beat-this-MIT.txt')
    shutil.copy2(deps/'key/LICENSE',notices/'MusicalKeyCNN-MIT.txt')
    shutil.copy2(ROOT/'native-analyzer/models.lock.json',models/'provenance.json')
    manifest(models)
    dist=ROOT/'dist';dist.mkdir(exist_ok=True);archive(models,dist/f'ytaudio-models-{MODELS_VERSION}.zip')
    if models_only:return
    flags=[]
    if goos=='windows':flags=['-A','ARM64' if arch=='arm64' else 'x64']
    if goos=='darwin':flags=['-DCMAKE_OSX_ARCHITECTURES=arm64','-DCMAKE_OSX_DEPLOYMENT_TARGET=13.4']
    run(['cmake','-S',ROOT/'native-analyzer','-B',build,'-DCMAKE_BUILD_TYPE=Release',*flags])
    run(['cmake','--build',build,'--config','Release','--parallel','2'])
    run(['ctest','--test-dir',build,'-C','Release','--output-on-failure'])
    install=ROOT/'build/analyzer/install'
    if install.exists():shutil.rmtree(install)
    run(['cmake','--install',build,'--config','Release','--prefix',install,*(['--strip'] if goos!='windows' else [])])
    library=deps/'ort/lib'
    patterns={'linux':['libonnxruntime.so.1'],'darwin':['libonnxruntime.*.dylib'],'windows':['onnxruntime.dll','onnxruntime_providers_shared.dll']}[goos]
    for pattern in patterns:
        for path in library.glob(pattern):shutil.copy2(path,install/path.name)
    if goos=='darwin':
        real=next(install.glob('libonnxruntime.*.dylib'))
        # Verified SDK LC_ID_DYLIB is @rpath/libonnxruntime.1.23.2.dylib.
        # The executable has @loader_path; no second 35 MB alias is needed.
        run(['codesign','--force','--sign','-',real])
        run(['codesign','--force','--sign','-',install/'ytaudio-analyzer'])
    if goos=='windows':
        # ONNX Runtime's native VC++ runtime dependencies must travel with it.
        import os
        vc=pathlib.Path(os.environ.get('VCToolsRedistDir',''))
        candidates=list(vc.glob(f'{arch if arch=="arm64" else "x64"}/Microsoft.VC*.CRT')) if str(vc)!='.' else []
        if not candidates:raise RuntimeError('Run from a Visual Studio developer environment with VCToolsRedistDir set')
        for path in candidates[-1].glob('*.dll'):shutil.copy2(path,install/path.name)
    notices=install/'licenses';notices.mkdir(exist_ok=True)
    for src,name in [(ROOT/'LICENSE','ytaudio-MIT.txt'),(deps/'beat/LICENSE','beat-this-cpp-MIT.txt'),(ROOT/'native-analyzer/licenses/beat-this.txt','beat-this-MIT.txt'),(deps/'pocketfft/LICENSE.md','PocketFFT-BSD-3-Clause.txt'),(deps/'r8brain/LICENSE','r8brain-MIT.txt'),(deps/'ort/LICENSE','ONNXRuntime-MIT.txt'),(deps/'ort/ThirdPartyNotices.txt','ONNXRuntime-third-party.txt'),(ROOT/'native-analyzer/licenses/librosa.txt','librosa-ISC.txt')]:shutil.copy2(src,notices/name)
    (notices/'nlohmann-json-MIT.txt').write_text('Copyright (c) 2013-2025 Niels Lohmann\n\n'+'Permission is hereby'+(ROOT/'LICENSE').read_text().split('Permission is hereby',1)[1])
    shutil.copy2(ROOT/'native-analyzer/sources.lock.json',install/'sources.lock.json')
    manifest(install,engine=True);archive(install,dist/f'ytaudio-analyzer-{ENGINE_VERSION}-{target}.zip')
    shutil.copytree(models,install/'models')
    binary=install/('ytaudio-analyzer.exe' if goos=='windows' else 'ytaudio-analyzer')
    with tempfile.TemporaryDirectory() as d:
        pcm=pathlib.Path(d)/'fixture.f32'
        with pcm.open('wb') as f:
            for i in range(44100*4):
                x=.1*math.sin(2*math.pi*220*i/44100);f.write(struct.pack('<ff',x,x))
        result=subprocess.run([str(binary),'--pcm',str(pcm),'--models',str(install/'models'),'--threads','2'],check=True,capture_output=True,text=True)
        parsed=json.loads(result.stdout)
        if (parsed['schema_version'],parsed['analyzer_version'],parsed['model_version'])!=(METADATA['schema_version'],ENGINE_VERSION,MODELS_VERSION):raise ValueError('Packaged analyzer smoke test failed')
    print(f'Packaged native runtime: {target}; model size: {sum(p.stat().st_size for p in models.rglob("*") if p.is_file())} bytes')
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--os',choices=['windows','darwin','linux'],default={'Linux':'linux','Darwin':'darwin','Windows':'windows'}[platform.system()]);p.add_argument('--arch',choices=['amd64','arm64'],default='arm64' if platform.machine().lower() in ('arm64','aarch64') else 'amd64');p.add_argument('--skip-prepare',action='store_true');p.add_argument('--skip-export',action='store_true');p.add_argument('--models-only',action='store_true');p.add_argument('--python',default=sys.executable)
    a=p.parse_args();package(a.os,a.arch,a.skip_prepare,a.skip_export,a.python,a.models_only)
