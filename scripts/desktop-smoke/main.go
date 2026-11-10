//go:build desktop

// Native WebView integration smoke test. Run on a desktop with a display:
// go run -tags 'desktop production webkit2_41' ./scripts/desktop-smoke
// Use isolated YTAUDIO_CONFIG_DIR/YTAUDIO_CACHE_DIR for test runs.
package main

import (
	"context"
	"fmt"
	"os"
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
	err := wails.Run(&options.App{Title: "ytaudio · native smoke test", Width: 1100, Height: 800, AssetServer: &assetserver.Options{Handler: app.DesktopHandler()},
		OnStartup: func(ctx context.Context) {
			wruntime.EventsOn(ctx, "ytaudio:smoke", func(data ...interface{}) {
				fmt.Printf("Native smoke result: %v\n", data)
				if len(data) > 0 && data[0] == "ok" {
					passed.Store(true)
				}
				wruntime.Quit(ctx)
			})
			go func() { time.Sleep(20 * time.Second); wruntime.Quit(ctx) }()
		},
		OnDomReady: func(ctx context.Context) {
			wruntime.WindowExecJS(ctx, `(async()=>{try{
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
