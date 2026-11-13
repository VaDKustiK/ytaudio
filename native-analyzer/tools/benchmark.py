#!/usr/bin/env python3
"""Controlled generated music only; never label these as real-world beat results."""
import argparse,hashlib,json,math,os,pathlib,socket,subprocess,sys,tempfile,time,urllib.request
import numpy as np
p=argparse.ArgumentParser();p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--models',type=pathlib.Path,required=True);p.add_argument('--seconds',type=int,default=180);p.add_argument('--report',type=pathlib.Path,required=True);p.add_argument('--app-binary',type=pathlib.Path)
a=p.parse_args();sr=44100;t=np.arange(sr*a.seconds)/sr;audio=np.zeros(t.shape,dtype=np.float32);rng=np.random.default_rng(42)
# C major, I-vi-IV-V with independent bass, upper harmonics and syncopated drums.
progression=[(48,52,55),(45,48,52),(41,45,48),(43,47,50)];beat_seconds=60/140
for start in np.arange(0,a.seconds,beat_seconds*8):
    begin=int(start*sr);end=min(len(audio),int((start+beat_seconds*8)*sr));local=t[begin:end]-start
    for note in progression[int(start/(beat_seconds*8))%4]:
        frequency=440*2**((note-69)/12)*432/440
        envelope=np.minimum(local/.03,1)*np.exp(-local*.25)
        for harmonic in (1,2,3):audio[begin:end]+=(.05/harmonic*np.sin(2*np.pi*frequency*harmonic*local)*envelope).astype(np.float32)
for i,start in enumerate(np.arange(0,a.seconds,beat_seconds/2)):
    begin=int(start*sr);n=min(int(sr*.15),len(audio)-begin);local=np.arange(n)/sr
    hat=.02*rng.standard_normal(n)*np.exp(-local*100)
    kick=.18*np.sin(2*np.pi*(60*local+1.8*(1-np.exp(-local*30))))*np.exp(-local*30) if i%4==0 else 0
    snare=.07*rng.standard_normal(n)*np.exp(-local*35) if i%8==4 else 0
    audio[begin:begin+n]+=(hat+kick+snare).astype(np.float32)
wrapper='''import json,resource,subprocess,sys,time
start=time.monotonic();p=subprocess.run(sys.argv[1:],capture_output=True,text=True,check=True);r=resource.getrusage(resource.RUSAGE_CHILDREN);print(json.dumps({'elapsed_seconds':time.monotonic()-start,'peak_rss_kib':r.ru_maxrss,'cpu_seconds':r.ru_utime+r.ru_stime,'result':json.loads(p.stdout)}))'''
records=[]
with tempfile.TemporaryDirectory() as directory:
    pcm=pathlib.Path(directory)/'fixture.f32';np.column_stack([audio,audio]).astype('<f4').tofile(pcm)
    for mode,threads in [('low',2),('balanced',4),('maximum',min(16,max(1,( __import__('os').cpu_count() or 1)-1)))]:
        run=subprocess.run([sys.executable,'-c',wrapper,str(a.binary.resolve()),'--pcm',str(pcm),'--models',str(a.models.resolve()),'--threads',str(threads)],capture_output=True,text=True,check=True)
        record=json.loads(run.stdout);record.update(mode=mode,threads=threads);records.append(record);print(json.dumps(record),flush=True)
report={'fixture':'Generated C major, 140 BPM, 432 Hz; not real-world accuracy data','duration_seconds':a.seconds,'platform':sys.platform,'binary_sha256':hashlib.sha256(a.binary.read_bytes()).hexdigest(),'records':records}
if a.app_binary:
    if sys.platform!='linux':raise RuntimeError('App idle measurement currently uses Linux /proc')
    # Compare the browser backend before and after creating the optional manager.
    # This does not measure WebView/Chromium renderer memory or DAW responsiveness.
    with tempfile.TemporaryDirectory(prefix='ytaudio-idle-') as directory:
        root=pathlib.Path(directory);folder=root/'audio';folder.mkdir()
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
        config=root/'config/ytaudio';config.mkdir(parents=True)
        (config/'config.json').write_text(json.dumps({'output_folder':str(folder),'format':'mp3','daily_cap':30,'port':port,'analyzer_performance':'balanced'}))
        cache=root/'cache/ytaudio/bin';cache.mkdir(parents=True);(cache/'.last-update-check').write_text('2099-01-01T00:00:00Z')
        env=dict(os.environ,YTAUDIO_AUTO_SETUP='0',YTAUDIO_CONFIG_DIR=str(root/'config'),YTAUDIO_CACHE_DIR=str(root/'cache'),YTAUDIO_ANALYZER_DIR=str(a.binary.resolve().parent))
        process=subprocess.Popen([str(a.app_binary.resolve()),'--no-browser'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        try:
            url=f'http://127.0.0.1:{port}'
            for _ in range(100):
                try:urllib.request.urlopen(url+'/api/health',timeout=1);break
                except Exception:time.sleep(.1)
            else:raise AssertionError('App did not start for idle measurement')
            def counters():
                fields=(pathlib.Path('/proc')/str(process.pid)/'stat').read_text().rsplit(')',1)[1].split()
                return (int(fields[11])+int(fields[12]))/os.sysconf('SC_CLK_TCK'),int(fields[21])*os.sysconf('SC_PAGE_SIZE')/1024
            def sample():
                before,_=counters();start=time.monotonic();time.sleep(5);after,rss=counters();elapsed=time.monotonic()-start
                children=(pathlib.Path('/proc')/str(process.pid)/'task'/str(process.pid)/'children').read_text().split()
                return {'sample_seconds':elapsed,'cpu_percent':100*(after-before)/elapsed,'rss_kib':rss,'child_processes':len(children)}
            time.sleep(2);before=sample()
            with urllib.request.urlopen(url+'/api/analysis',timeout=5) as response:state=json.load(response)
            assert state['ready'] and state['status']=='idle',state
            after=sample();assert after['child_processes']==0,after
            report['idle_browser_backend']={'before_analyzer':before,'after_analyzer':after,'rss_delta_kib':after['rss_kib']-before['rss_kib'],'note':'Linux browser backend only; excludes GUI renderer'}
            print(json.dumps(report['idle_browser_backend']),flush=True)
        finally:process.terminate();process.wait(timeout=20)
a.report.parent.mkdir(parents=True,exist_ok=True);a.report.write_text(json.dumps(report,indent=2)+'\n')
