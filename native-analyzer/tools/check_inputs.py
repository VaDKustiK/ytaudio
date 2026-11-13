#!/usr/bin/env python3
"""Native input safety smoke tests. Only stdlib; never downloads audio."""
import argparse,array,json,math,pathlib,random,shutil,subprocess,tempfile
p=argparse.ArgumentParser();p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--models',type=pathlib.Path,required=True);a=p.parse_args()
with tempfile.TemporaryDirectory() as directory:
    directory=pathlib.Path(directory)/'Music & звук';directory.mkdir()
    pcm=directory/'音楽.f32'
    unicode_models=directory/'模型';shutil.copytree(a.models.resolve(),unicode_models)
    for kind in ('silence','noise','anti-phase','clipped','mixed','sliding','nonfinite','short','truncated','oversized'):
        data=array.array('f')
        rng=random.Random(42)
        for i in range(44100*(8 if kind=='sliding' else 4)):
            x=0 if kind=='silence' else .3*math.sin(2*math.pi*220*i/44100)
            if kind=='noise':x=.1*rng.gauss(0,1)
            if kind=='sliding':
                t=i/44100
                x=.5*math.tanh(3*math.sin(2*math.pi*(70*t+15/math.pi*math.sin(2*math.pi*t))))+sum(.03*math.sin(2*math.pi*f*t) for f in (130.8128,164.8138,195.9977,261.6256,329.6276,391.9954))
            if kind=='mixed':x=sum(.04*math.sin(2*math.pi*f*2**(offset/1200)*i/44100) for f in (130.8128,164.8138,195.9977,261.6256,329.6276,391.9954) for offset in (-15,15))
            if kind=='clipped':x*=1e30
            data.extend([x,-x if kind=='anti-phase' else x])
        if kind=='nonfinite':data[100]=float('nan')
        if kind=='short':data=data[:100]
        raw=data.tobytes()
        if kind=='truncated':raw=raw[:-1]
        pcm.write_bytes(raw)
        if kind=='oversized':
            with pcm.open('r+b') as file:file.truncate(44100*2*4*1800+8)
        run=subprocess.run([str(a.binary.resolve()),'--pcm',str(pcm),'--models',str(unicode_models),'--threads','2'],capture_output=True,text=True)
        if kind in ('nonfinite','short','truncated','oversized'):assert run.returncode!=0,kind
        else:
            assert run.returncode==0,(kind,run.stderr)
            result=json.loads(run.stdout)
            assert result['schema_version']==1
            if kind in ('silence','noise'):assert result['key']['tonic'] is None and result['tuning']['reference_hz'] is None,result
            elif kind=='mixed':assert result['tuning']['status']=='mixed' and result['tuning']['reference_hz'] is None and result['key']['status'] in ('uncertain','unavailable'),result
            elif kind=='sliding':assert result['tuning']['reference_hz'] is None or abs(result['tuning']['reference_hz']-440)<1,result
            else:assert result['tuning']['reference_hz'] is not None and result['key']['tonic'] is None,(kind,result)
        print(f'{kind}: passed')
