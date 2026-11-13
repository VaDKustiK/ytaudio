#!/usr/bin/env python3
"""Deterministic 24-key generated-fixture evaluation, not real-world accuracy."""
import argparse,hashlib,json,pathlib,subprocess,tempfile
import numpy as np
p=argparse.ArgumentParser();p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--models',type=pathlib.Path,required=True);p.add_argument('--report',type=pathlib.Path,required=True);a=p.parse_args()
sr=44100;seconds=16;names=['C','C#','D','Eb','E','F','F#','G','Ab','A','Bb','B'];records=[]
with tempfile.TemporaryDirectory() as directory:
    directory=pathlib.Path(directory)
    for minor in (False,True):
        for tonic in range(12):
            bpm=84 if minor else 140;offset=-31.76665 if minor else 0
            audio=np.zeros(sr*seconds,np.float32)
            chords=[(0,3,7),(8,12,15),(5,8,12),(7,10,14)] if minor else [(0,4,7),(9,12,16),(5,9,12),(7,11,14)]
            for bar,chord in enumerate(chords):
                begin=bar*sr*4;local=np.arange(sr*4)/sr;envelope=np.minimum(local/.02,1)*np.exp(-local*.2)
                for step in chord:
                    f=440*2**((48+tonic+step-69)/12+offset/1200)
                    for harmonic in (1,2,3):audio[begin:begin+sr*4]+=(.05/harmonic*np.sin(2*np.pi*f*harmonic*local)*envelope).astype(np.float32)
            rng=np.random.default_rng(123)
            for index,start in enumerate(np.arange(0,seconds,60/bpm/2)):
                begin=int(start*sr);n=min(4000,len(audio)-begin);local=np.arange(n)/sr
                drum=.13*np.sin(2*np.pi*65*local)*np.exp(-local*35) if index%4==0 else .03*rng.standard_normal(n)*np.exp(-local*70)
                audio[begin:begin+n]+=drum.astype(np.float32)
            pcm=directory/'fixture.f32';np.column_stack([audio,audio]).astype('<f4').tofile(pcm)
            result=json.loads(subprocess.run([str(a.binary.resolve()),'--pcm',str(pcm),'--models',str(a.models.resolve()),'--threads','2'],check=True,capture_output=True,text=True).stdout)
            expected_scale='minor' if minor else 'major';key=result['key'];detected=result['tempo']['bpm'];tuning=result['tuning']['offset_cents']
            reference_hz=440*2**(offset/1200)
            record={'tonic':names[tonic],'scale':expected_scale,'bpm':bpm,'offset_cents':offset,'result':result,'exact_key':key['tonic']==names[tonic] and key['scale']==expected_scale,'tempo_equivalent':detected is not None and min(abs(detected-bpm),abs(detected*2-bpm),abs(detected/2-bpm))<=1,'tuning_error_cents':None if tuning is None else abs((tuning-offset+50)%100-50),'tuning_error_hz':None if result['tuning']['reference_hz'] is None else abs(result['tuning']['reference_hz']-reference_hz)}
            expected_notes={(tonic+n)%12 for n in ((0,2,3,5,7,8,10) if minor else (0,2,4,5,7,9,11))}
            detected_notes=({(names.index(key['tonic'])+n)%12 for n in ((0,2,3,5,7,8,10) if key['scale']=='minor' else (0,2,4,5,7,9,11))} if key['tonic'] in names else set())
            record['usable_scale_notes']=expected_notes==detected_notes
            record['tempo_exact']=detected is not None and abs(detected-bpm)<=1
            records.append(record)
    summary={'fixture_type':'generated chords and percussion; not independently labelled real-world beats','binary_sha256':hashlib.sha256(a.binary.read_bytes()).hexdigest(),'count':len(records),'usable_scale_notes_correct':sum(r['usable_scale_notes'] for r in records),'tempo_exact_correct':sum(r['tempo_exact'] for r in records),'unflagged_key_errors':sum(not r['exact_key'] and r['result']['key']['status']=='estimated' for r in records),'tuning_available':sum(r['tuning_error_cents'] is not None for r in records),'max_tuning_error_cents':max((r['tuning_error_cents'] for r in records if r['tuning_error_cents'] is not None),default=None),'exact_key_correct':sum(r['exact_key'] for r in records),'tempo_equivalent_correct':sum(r['tempo_equivalent'] for r in records),'key_uncertain':sum(r['result']['key']['status']!='estimated' for r in records),'max_tuning_error_hz':max(r['tuning_error_hz'] or 0 for r in records),'records':records}
    a.report.parent.mkdir(parents=True,exist_ok=True);a.report.write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps({k:v for k,v in summary.items() if k!='records'}),flush=True)
