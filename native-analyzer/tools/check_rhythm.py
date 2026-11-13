#!/usr/bin/env python3
"""Milestone 2 gate: actual C++ features/logits vs official final0, offline."""
import argparse, hashlib, json, pathlib, subprocess, sys, tempfile, time
import numpy as np
import torch
import soxr

def main():
    p=argparse.ArgumentParser();p.add_argument('--reference',type=pathlib.Path,required=True);p.add_argument('--checkpoint',type=pathlib.Path,required=True);p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--models',type=pathlib.Path,required=True);p.add_argument('--report',type=pathlib.Path,required=True)
    a=p.parse_args();sys.path.insert(0,str(a.reference.resolve()))
    from beat_this.inference import load_model,split_predict_aggregate
    from beat_this.preprocessing import LogMelSpect
    from beat_this.model.postprocessor import Postprocessor
    torch.set_num_threads(2)
    model=load_model(str(a.checkpoint)).eval();mel=LogMelSpect();post=Postprocessor()
    reports=[]
    for seconds in (2,29.6,30,30.3,64):
        sample_rate=44100;t=np.arange(int(seconds*sample_rate))/sample_rate
        rng=np.random.default_rng(42)
        audio=(0.2*np.sin(2*np.pi*220*t)+0.12*np.sin(2*np.pi*330*t)+0.08*np.sin(2*np.pi*440*t)).astype(np.float32)
        # Synthetic pitched rhythm with noise bursts; parity fixture, not a genre benchmark.
        for index,beat in enumerate(np.arange(0,seconds,60/140)):
            start=int(beat*sample_rate);n=min(5000,len(t)-start);local=np.arange(n)/sample_rate
            audio[start:start+n]+=(0.25*np.sin(2*np.pi*70*local)*np.exp(-local*35)+0.12*rng.standard_normal(n)*np.exp(-local*80)).astype(np.float32)
        with tempfile.TemporaryDirectory() as directory:
            directory=pathlib.Path(directory);pcm=directory/'audio.f32';np.column_stack([audio,audio]).astype('<f4').tofile(pcm)
            begin=time.monotonic()
            run=subprocess.run([str(a.binary.resolve()),'--pcm',str(pcm),'--models',str(a.models.resolve()),'--threads','2','--dump',str(directory)],capture_output=True,text=True,check=True)
            duration=time.monotonic()-begin
            output=json.loads(run.stdout)
            decoded=np.fromfile(directory/'rhythm-audio.f32',dtype='<f4')
            native_mel=np.fromfile(directory/'mel.f32',dtype='<f4').reshape(-1,128)
            with torch.inference_mode():
                reference_mel=mel(torch.from_numpy(decoded.copy()))
                native_spect=torch.from_numpy(native_mel.copy())
                # Separate tensor-inference parity from FFT/filterbank differences.
                expected=split_predict_aggregate(native_spect,1500,6,'keep_first',model)
            mel_error=float(np.max(np.abs(native_mel-reference_mel.numpy())))
            reference_audio=soxr.resample(audio,44100,22050)
            resample_rms=float(np.sqrt(np.mean((decoded-reference_audio)**2)))
            with torch.inference_mode():
                full_mel=mel(torch.from_numpy(reference_audio.copy()))
                full_expected=split_predict_aggregate(full_mel,1500,6,'keep_first',model)
            full_beats,full_downbeats=post(full_expected['beat'],full_expected['downbeat'])
            item={'resampling_rms':resample_rms,'full_pipeline_mel_rms':float(np.sqrt(np.mean((native_mel-full_mel.numpy())**2))),'seconds':seconds,'native_seconds':duration,'mel_max_abs':mel_error,'tempo':output['tempo']}
            assert mel_error<3e-3, item
            for channel in ('beat','downbeat'):
                actual=np.fromfile(directory/f'{channel}-logits.f32',dtype='<f4')
                target=expected[channel].numpy()
                error=float(np.max(np.abs(actual-target)))
                item[f'{channel}_logits_max_abs']=error
                # ONNX/PyTorch reductions differ slightly; no prediction shift is allowed.
                assert error<3e-3,item
            reference_beats,reference_downbeats=post(expected['beat'],expected['downbeat'])
            native_beats=np.fromfile(directory/'beats.f32',dtype='<f4')
            assert len(reference_beats)==len(native_beats),item
            assert not len(native_beats) or np.max(np.abs(reference_beats-native_beats))<1e-5,item
            native_downbeats=np.fromfile(directory/'downbeats.f32',dtype='<f4')
            assert len(native_downbeats)==len(reference_downbeats),item
            assert not len(native_downbeats) or np.max(np.abs(native_downbeats-reference_downbeats))<1e-5,item
            # r8brain and the official soxr HQ have different transition bands.
            # Report this difference; require matching complete beat/downbeat
            # counts and positions, rather than pretending the waveforms are identical.
            assert resample_rms<0.003,item
            item['full_pipeline_logit_rms']=float(np.sqrt(np.mean((np.fromfile(directory/'beat-logits.f32',dtype='<f4')-full_expected['beat'].numpy())**2)))
            assert len(full_beats)==len(native_beats) and len(full_downbeats)==len(native_downbeats),item
            item['full_pipeline_beat_time_max_abs']=float(np.max(np.abs(full_beats-native_beats))) if len(full_beats) else 0
            item['full_pipeline_downbeat_time_max_abs']=float(np.max(np.abs(full_downbeats-native_downbeats))) if len(full_downbeats) else 0
            assert item['full_pipeline_beat_time_max_abs']<=0.02001 and item['full_pipeline_downbeat_time_max_abs']<=0.02001,item
            if seconds>=29:assert abs(output['tempo']['bpm']-140)<1,item
            reports.append(item);print(json.dumps(item),flush=True)
    a.report.parent.mkdir(parents=True,exist_ok=True);a.report.write_text(json.dumps({'fixtures':'synthetic parity only','results':reports},indent=2)+'\n')
if __name__=='__main__':main()
