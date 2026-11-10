//go:build desktop

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/server"
	"github.com/VaDKustiK/ytaudio/packaging"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var version = "dev"

func main() {
	versionFlag := flag.Bool("version", false, "print the application version")
	flag.Parse()
	if *versionFlag {
		fmt.Println(version)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v", err)
	}
	app := server.New(cfg, "http://wails.localhost/", version)
	var appContext atomic.Value
	defer app.Close()
	if err := wails.Run(&options.App{
		Title:            "ytaudio",
		BackgroundColour: &options.RGBA{R: 18, G: 22, B: 30, A: 255},
		Linux:            &linux.Options{Icon: packaging.Icon, ProgramName: "ytaudio", WebviewGpuPolicy: linux.WebviewGpuPolicyNever},
		Width:            1100,
		Height:           800,
		MinWidth:         640,
		MinHeight:        600,
		OnStartup: func(ctx context.Context) {
			appContext.Store(ctx)
			app.SetDesktopActions(func() (string, error) {
				return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{Title: "Choose your download folder", CanCreateDirectories: true})
			}, revealFolder, func(url string) { wailsruntime.BrowserOpenURL(ctx, url) })
		},
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "3370c2ec-e76b-4e44-9756-b9a3cb0c39dd", OnSecondInstanceLaunch: func(options.SecondInstanceData) {
			if value := appContext.Load(); value != nil {
				ctx := value.(context.Context)
				wailsruntime.WindowUnminimise(ctx)
				wailsruntime.WindowShow(ctx)
			}
		}},
		OnShutdown:  func(context.Context) { app.Close() },
		AssetServer: &assetserver.Options{Handler: app.DesktopHandler()},
	}); err != nil {
		log.Fatal(err)
	}
}
