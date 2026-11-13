//go:build desktop

// Native WebView integration smoke test. Run on a desktop with a display:
// go run -tags 'desktop production webkit2_41' ./scripts/desktop-smoke
// Use isolated YTAUDIO_CONFIG_DIR/YTAUDIO_CACHE_DIR for test runs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/server"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	cfg := config.Defaults()
	app := server.New(cfg, "http://wails.localhost/", "dev")
	defer app.Close()
	var passed atomic.Bool
	audio, dropped := os.Getenv("YTAUDIO_SMOKE_AUDIO"), os.Getenv("YTAUDIO_SMOKE_DROP_AUDIO")
	if dropped == "" {
		dropped = audio
	}
	err := wails.Run(&options.App{Title: "ytaudio · native smoke test", Width: 1100, Height: 800, DragAndDrop: &options.DragAndDrop{EnableFileDrop: true}, AssetServer: &assetserver.Options{Handler: app.DesktopHandler()},
		OnStartup: func(ctx context.Context) {
			if audio != "" {
				app.SetAudioPicker(func() (string, error) { return audio, nil })
				wruntime.EventsOn(ctx, "ytaudio:smoke:drop", func(data ...interface{}) {
					if len(data) == 2 {
						wruntime.EventsEmit(ctx, "wails:file-drop", data[0], data[1], []string{dropped})
					}
				})
			}
			wruntime.EventsOn(ctx, "ytaudio:smoke", func(data ...interface{}) {
				fmt.Printf("Native smoke result: %v\n", data)
				if len(data) > 0 && data[0] == "ok" {
					passed.Store(true)
				}
				wruntime.Quit(ctx)
			})
			go func() { time.Sleep(45 * time.Second); wruntime.Quit(ctx) }()
		},
		OnDomReady: func(ctx context.Context) {
			metadata, _ := json.Marshal(map[string]any{"enabled": audio != "", "picked": filepath.Base(audio), "dropped": filepath.Base(dropped)})
			wruntime.WindowExecJS(ctx, "window.smokeAudioExpected="+string(metadata)+";\n"+`(async()=>{try{
 await new Promise(r=>setTimeout(r,1500));
 const logo=document.querySelector('.brand-logo');await logo.decode();if(!logo.naturalWidth)throw Error('application artwork');
 if(getComputedStyle(document.documentElement).colorScheme!=='dark')throw Error('dark mode');
 const previous=await window.runtime.ClipboardGetText();
 try {
 await window.runtime.ClipboardSetText('https://youtu.be/native-paste');
 document.querySelector('#paste-url').click();
 for(let i=0;i<30&&document.querySelector('#url').value!=='https://youtu.be/native-paste';i++)await new Promise(r=>setTimeout(r,50));
 if(document.querySelector('#url').value!=='https://youtu.be/native-paste')throw Error('native clipboard paste');
 }finally{await window.runtime.ClipboardSetText(previous);}
 const token=document.querySelector('meta[name="ytaudio-token"]').content;
 const health=await(await fetch('/api/health')).json();if(health.status!=='ok')throw Error('health');
 const settings=await(await fetch('/api/settings')).json();if(!settings.output_folder)throw Error('settings');
 if(!document.querySelector('#format-help').textContent)throw Error('app.js did not initialize');
 document.querySelector('[data-page="settings"]').click();if(document.querySelector('#page-settings').hidden)throw Error('navigation');
 const rejected=await fetch('/api/setup');if(rejected.status!==405)throw Error('GET setup accepted');
 const result=await fetch('/api/settings',{method:'PUT',headers:{'Content-Type':'application/json','X-Ytaudio-Token':token},body:JSON.stringify({...settings,proxy:'ftp://invalid'})});if(result.status!==400 && result.status!==409)throw Error('POST/body bridge: '+result.status);
 if(window.smokeAudioExpected.enabled){
 const waitFor=async(check,label)=>{const deadline=Date.now()+30000;while(!check()){if(Date.now()>deadline)throw Error(label);await new Promise(r=>setTimeout(r,50));}};
 document.querySelector('[data-page="analyzer"]').click();
 document.querySelector('#choose-audio').click();
 await waitFor(()=>document.querySelector('#audio-name').textContent===window.smokeAudioExpected.picked,'native picker bridge');
 await waitFor(()=>!document.querySelector('#analyze-audio').disabled,'analyzer readiness');
 document.querySelector('#analyze-audio').click();
 await waitFor(()=>!document.querySelector('#analysis-results').hidden,'native analysis results');
 if(document.querySelector('#analysis-tuning').textContent!=='440 Hz (0 cents)')throw Error('native tuning result');
 const previousResultsClipboard=await window.runtime.ClipboardGetText();
 try{document.querySelector('#copy-analysis').click();await waitFor(()=>document.querySelector('#analysis-message').textContent==='Copied','native result copy');if(!(await window.runtime.ClipboardGetText()).includes('Tuning: 440 Hz (0 cents)'))throw Error('native result clipboard');}
 finally{await window.runtime.ClipboardSetText(previousResultsClipboard);}
 const area=document.querySelector('#audio-drop').getBoundingClientRect();
 window.runtime.EventsEmit('ytaudio:smoke:drop',area.x+10,area.y+10);
 await waitFor(()=>document.querySelector('#audio-name').textContent===window.smokeAudioExpected.dropped,'native drop event bridge');
 if(window.smokeAudioExpected.picked!==window.smokeAudioExpected.dropped&&!document.querySelector('#analysis-results').hidden)throw Error('stale native results');
 }
 window.runtime.EventsEmit('ytaudio:smoke','ok');
 }catch(e){window.runtime.EventsEmit('ytaudio:smoke',String(e));}})()`)
		}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err != nil || !passed.Load() {
		os.Exit(1)
	}
}
