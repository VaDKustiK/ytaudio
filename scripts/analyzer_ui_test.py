#!/usr/bin/env python3
"""Real local FFmpeg/native analysis with browser and mocked native-picker/drop APIs.
No audio downloads or release checks; native OS dialog rendering is not tested.
"""
import argparse,array,base64,json,math,os,pathlib,socket,subprocess,tempfile,time,urllib.request,wave
from playwright.sync_api import sync_playwright,expect

def test(binary,component):
    with tempfile.TemporaryDirectory(prefix='ytaudio-analyzer-ui-') as root:
        root=pathlib.Path(root);folder=root/'audio';folder.mkdir();audio=folder/'Music & звук.wav'
        with wave.open(str(audio),'wb') as f:
            f.setnchannels(1);f.setsampwidth(2);f.setframerate(44100)
            data=array.array('h',(int(sum(2500*math.sin(2*math.pi*freq*i/44100) for freq in (130.8128,164.8138,195.9977,261.6256,329.6276,391.9954))) for i in range(44100*6)))
            f.writeframes(data.tobytes())
        second=folder/'Another beat.flac';subprocess.run(['ffmpeg','-nostdin','-v','error','-i',str(audio),str(second)],check=True)
        sock=socket.socket();sock.bind(('127.0.0.1',0));port=sock.getsockname()[1];sock.close()
        cfg=root/'config/ytaudio';cfg.mkdir(parents=True);(cfg/'config.json').write_text(json.dumps({'output_folder':str(folder),'format':'mp3','proxy':'','daily_cap':30,'port':port,'analyzer_performance':'balanced'}))
        cache=root/'cache/ytaudio/bin';cache.mkdir(parents=True);(cache/'.last-update-check').write_text('2099-01-01T00:00:00Z')
        env=dict(os.environ,YTAUDIO_AUTO_SETUP='0',YTAUDIO_CONFIG_DIR=str(root/'config'),YTAUDIO_CACHE_DIR=str(root/'cache'),YTAUDIO_ANALYZER_DIR=str(component.resolve()))
        process=subprocess.Popen([str(binary.resolve()),'--no-browser'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        try:
            url=f'http://127.0.0.1:{port}'
            for _ in range(100):
                try:urllib.request.urlopen(url+'/api/health',timeout=1);break
                except Exception:time.sleep(.1)
            else:raise AssertionError('app did not start')
            with sync_playwright() as pw:
                browser=pw.chromium.launch(headless=True);context=browser.new_context(viewport={'width':1200,'height':950});page=context.new_page();errors=[];page.on('pageerror',lambda error:errors.append(str(error)))
                page.add_init_script('window.runtime={OnFileDrop: cb=>window.testFileDrop=cb,ClipboardSetText:async text=>{window.copiedAnalysis=text;}}')
                page.goto(url);page.locator('[data-page=analyzer]').click()
                registered=page.evaluate('''async path=>{const r=await fetch('/api/analysis/select',{method:'POST',headers:{'Content-Type':'application/json','X-Ytaudio-Token':document.querySelector('meta[name=ytaudio-token]').content},body:JSON.stringify({path})});return r.json();}''',str(audio))
                page.route('**/api/analysis/pick',lambda route:route.fulfill(status=200,content_type='application/json',body=json.dumps(registered)))
                page.locator('#choose-audio').click();expect(page.locator('#audio-name')).to_have_text(audio.name)
                expect(page.locator('#analyze-audio')).to_be_enabled();page.locator('#analyze-audio').click()
                page.locator('[data-page=settings]').click();expect(page.locator('#page-settings')).to_be_visible();page.locator('[data-page=analyzer]').click()
                expect(page.locator('#analysis-results')).to_be_visible(timeout=30000);expect(page.locator('#analysis-tuning')).to_have_text('440 Hz (0 cents)')
                page.locator('#copy-analysis').click();assert 'Tuning: 440 Hz (0 cents)' in page.evaluate('window.copiedAnalysis')
                # Warnings retain the faster interpretation in the main result
                # and copied text, without showing a second half-time number.
                presentation=page.evaluate('''()=>{
                    const state={...analysisState,result:{...analysisState.result,tempo:{bpm:65,alternate_bpm:130,confidence:.82,variable:true}}};
                    renderAnalysis(state);
                    return {tempo:document.querySelector('#analysis-tempo').textContent,note:document.querySelector('#analysis-tempo-alt').textContent,copy:analysisCopy};
                }''')
                assert presentation['tempo']=='130 BPM' and presentation['note']=='Tempo varies' and 'Tempo: 130 BPM (tempo varies)' in presentation['copy'],presentation
                # Round presentation and copied BPM, keeping raw measurements
                # intact for the detector/cache and handling both sides of .5.
                for bpm,alternate,expected in ((125.1,62.55,'125 BPM'),(125.5,62.75,'126 BPM'),(125.9,62.95,'126 BPM'),(62.5,125,'125 BPM'),(62.55,125.1,'125 BPM'),(125.1,None,'125 BPM'),(None,None,'Unknown')):
                    presentation=page.evaluate('''([bpm,alternate])=>{
                        const tempo={bpm,alternate_bpm:alternate,confidence:.82,variable:false};
                        renderAnalysis({...analysisState,result:{...analysisState.result,tempo}});
                        return {tempo:document.querySelector('#analysis-tempo').textContent,note:document.querySelector('#analysis-tempo-alt').textContent,copy:analysisCopy,raw:analysisState.result.tempo.bpm};
                    }''',[bpm,alternate])
                    assert presentation['tempo']==expected and presentation['note']=='' and f'Tempo: {expected}\n' in presentation['copy'] and presentation['raw']==bpm,presentation
                presentation=page.evaluate('''()=>{
                    renderAnalysis({...analysisState,result:{...analysisState.result,tempo:{bpm:62.5,alternate_bpm:125,confidence:.6,variable:false}}});
                    return {tempo:document.querySelector('#analysis-tempo').textContent,note:document.querySelector('#analysis-tempo-alt').textContent,copy:analysisCopy};
                }''')
                assert presentation['tempo']=='125 BPM' and presentation['note']=='Uncertain tempo' and 'Tempo: 125 BPM (uncertain)' in presentation['copy'],presentation
                # Selecting the same path starts a new selection; polling must
                # not resurrect a result from an older job before Analyze.
                page.locator('#choose-audio').click();expect(page.locator('#analysis-results')).to_be_hidden();page.wait_for_timeout(1800);expect(page.locator('#analysis-results')).to_be_hidden()
                page.locator('#analyze-audio').click();expect(page.locator('#analysis-results')).to_be_visible(timeout=30000)
                page.evaluate('path=>window.testFileDrop(500,300,[path])',str(folder/'missing.wav'));expect(page.locator('#analysis-message')).to_contain_text('missing');page.wait_for_timeout(1800);expect(page.locator('#analysis-message')).to_contain_text('missing')
                # Native drop passes the original path; no upload/copy is involved.
                page.evaluate('path=>window.testFileDrop(500,300,[path])',str(second));expect(page.locator('#audio-name')).to_have_text(second.name);expect(page.locator('#analysis-results')).to_be_hidden()
                page.locator('#analyze-audio').click();expect(page.locator('#analysis-results')).to_be_visible(timeout=30000)
                # A completed download's actual registered file opens and starts analysis.
                page.route('**/api/jobs',lambda route:route.fulfill(status=200,content_type='application/json',body=json.dumps([{'id':'saved','format':'mp3','url':'https://www.youtube.com/watch?v=fixture','state':'complete','message':'Audio saved','progress':{'file':audio.name,'percent':100,'stage':'saved'},'files':[registered]}])))
                page.locator('[data-page=downloader]').click();page.get_by_role('button',name='BPM and key detection',exact=True).click(timeout=10000)
                expect(page.locator('#page-analyzer')).to_be_visible();expect(page.locator('#audio-name')).to_have_text(audio.name)
                expect(page.locator('#analysis-results')).to_be_visible(timeout=30000)
                # First-time setup preserves the same automatic handoff too.
                setup_pending=[True]
                def analyzer_state(route):
                    if setup_pending[0] and route.request.method=='GET':route.fulfill(status=200,content_type='application/json',body=json.dumps({'ready':False,'status':'idle','installing':False}))
                    else:route.continue_()
                def install_component(route):
                    setup_pending[0]=False
                    route.fulfill(status=200,content_type='application/json',body=json.dumps({'ready':True,'status':'idle','installing':False}))
                page.route('**/api/analysis',analyzer_state);page.route('**/api/analysis/install',install_component)
                expect(page.locator('#install-analyzer')).to_be_visible()
                page.locator('[data-page=downloader]').click();page.get_by_role('button',name='BPM and key detection',exact=True).click(timeout=10000);expect(page.locator('#page-analyzer')).to_be_visible();expect(page.locator('#audio-name')).to_have_text(audio.name);page.locator('#install-analyzer').click();expect(page.locator('#analysis-results')).to_be_visible(timeout=30000)
                page.locator('[data-page=settings]').click();page.locator('#analyzer-performance').select_option('low');page.locator('#save-settings').click();expect(page.locator('#settings-message')).to_have_text('Preferences saved.')
                assert json.loads((cfg/'config.json').read_text())['analyzer_performance']=='low'
                # Browser mode supports the actual file-input/drag-upload route too.
                browser_page=browser.new_page();browser_page.goto(url);browser_page.locator('[data-page=analyzer]').click();browser_page.locator('#audio-file').set_input_files(str(audio));expect(browser_page.locator('#audio-name')).to_have_text(audio.name);browser_page.locator('#analyze-audio').click();expect(browser_page.locator('#analysis-results')).to_be_visible(timeout=30000)
                # Exercise DOM drag and drop in the browser fallback, not just the input.
                encoded=base64.b64encode(audio.read_bytes()).decode()
                browser_page.evaluate("""data=>{const bytes=Uint8Array.from(atob(data.audio),c=>c.charCodeAt(0));const transfer=new DataTransfer();transfer.items.add(new File([bytes],data.name,{type:'audio/wav'}));document.querySelector('#audio-drop').dispatchEvent(new DragEvent('drop',{dataTransfer:transfer,bubbles:true,cancelable:true}));}""",{'audio':encoded,'name':'Dropped audio.wav'})
                expect(browser_page.locator('#audio-name')).to_have_text('Dropped audio.wav');browser_page.locator('#analyze-audio').click();expect(browser_page.locator('#analysis-results')).to_be_visible(timeout=30000)
                page.locator('[data-page=analyzer]').click();page.screenshot(path='/tmp/ytaudio-analyzer.png',full_page=True)
                assert not errors,errors
                browser.close()
            print('Analyzer UI: picker, native drop callback, real analysis, copy, download handoff, settings, browser upload passed')
        finally:process.terminate();process.wait(timeout=20)
if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--binary',type=pathlib.Path,required=True);p.add_argument('--component',type=pathlib.Path,required=True);a=p.parse_args();test(a.binary,a.component)
