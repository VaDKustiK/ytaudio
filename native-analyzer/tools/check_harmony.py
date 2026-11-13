#!/usr/bin/env python3
"""Milestone 3 gate: full native CQT/KeyNet vs librosa and PyTorch."""
import argparse,json,pathlib,subprocess,sys,tempfile,time
import librosa,numpy as np,torch
p=argparse.ArgumentParser();p.add_argument('--source',type=pathlib.Path,required=True);p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--models',type=pathlib.Path,required=True);p.add_argument('--report',type=pathlib.Path,required=True)
a=p.parse_args();sys.path.insert(0,str(a.source.resolve()))
from model import KeyNet
model=KeyNet().eval();model.load_state_dict(torch.load(a.source/'checkpoints/keynet.pt',weights_only=True,map_location='cpu'));torch.set_num_threads(2)
records=[]

def aggregate(cqt):
    scores=[]
    with torch.inference_mode():
        for start in range(0,cqt.shape[1],100):
            count=min(100,cqt.shape[1]-start);weight=count
            if count<8:start=cqt.shape[1]-8;count=8
            value=model(torch.tensor(cqt[:,start:start+count])[None,None]).numpy()[0]
            scores.append(value*weight)
    return sum(scores)/cqt.shape[1]

for seconds,offset in ((2,0),(20.4,0),(24,-31.76665),(24,23),(24,49),(60.4,0)):
    sr=44100;t=np.arange(int(sr*seconds))/sr
    audio=np.zeros(t.shape,dtype=np.float32)
    for f in (130.8128,164.8138,195.9977,261.6256,329.6276,391.9954):
        audio+=(0.09*np.sin(2*np.pi*f*2**(offset/1200)*t)).astype(np.float32)
    # Alternate envelope/sections and add quiet percussion to test more than a sine.
    audio*=(0.75+0.25*np.cos(2*np.pi*0.3*t)).astype(np.float32)
    rng=np.random.default_rng(314)
    for beat in np.arange(0,seconds,60/140):
        start=int(beat*sr);n=min(2400,len(t)-start);audio[start:start+n]+=(0.05*rng.standard_normal(n)*np.exp(-np.arange(n)/500)).astype(np.float32)
    with tempfile.TemporaryDirectory() as d:
        d=pathlib.Path(d);pcm=d/'audio.f32';np.column_stack([audio,audio]).astype('<f4').tofile(pcm)
        begin=time.monotonic();run=subprocess.run([str(a.binary.resolve()),'--pcm',str(pcm),'--models',str(a.models.resolve()),'--threads','2','--dump',str(d)],check=True,capture_output=True,text=True)
        elapsed=time.monotonic()-begin;result=json.loads(run.stdout)
        detected=result['tuning']['offset_cents']
        if seconds>=3:assert detected is not None and abs(np.remainder(detected-offset+50,100)-50)<2,(offset,result)
        else:assert result["tuning"]["status"]=="unknown",result
        corrected=detected if result['key'].get('tuning_normalized') else 0
        expected=np.log1p(np.abs(librosa.cqt(audio,sr=sr,hop_length=8820,n_bins=105,bins_per_octave=24,fmin=65,tuning=corrected*24/1200)))[:,:-2]
        native=np.fromfile(d/'cqt.f32',dtype='<f4').reshape(105,-1)
        delta=np.abs(expected-native)
        record={'offset':offset,'detected_cents':detected,'seconds':elapsed,'cqt_max_abs':float(delta.max()),'cqt_mean_abs':float(delta.mean()),'result':result}
        assert delta.max()<0.03 and delta.mean()<0.002,record
        target=aggregate(expected);actual=np.fromfile(d/'key-logits.f32',dtype='<f4')
        record['key_logits_max_abs']=float(np.max(np.abs(target-actual)))
        record['key_prediction_equal']=bool(target.argmax()==actual.argmax())
        assert record['key_logits_max_abs']<0.03 and record['key_prediction_equal'],record
        # Compare grid correction with leaving the same detuned audio untouched.
        # This records evidence; it does not assume correction always helps.
        raw=aggregate(np.log1p(np.abs(librosa.cqt(audio,sr=sr,hop_length=8820,n_bins=105,bins_per_octave=24,fmin=65,tuning=0)))[:,:-2]) if corrected else target
        record['normalized_class']=int(target.argmax());record['unnormalized_class']=int(raw.argmax())
        record['normalized_correct']=bool(target.argmax()==19);record['unnormalized_correct']=bool(raw.argmax()==19)
        print(json.dumps(record),flush=True)
        records.append(record)
a.report.parent.mkdir(parents=True,exist_ok=True);a.report.write_text(json.dumps({'fixtures':'controlled synthetic only','results':records},indent=2)+'\n')
