//go:build desktop

package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/server"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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
	app := server.New(cfg, "http://wails.localhost/")
	defer app.Close()
	if err := wails.Run(&options.App{
		Title:       "ytaudio",
		Width:       1100,
		Height:      800,
		MinWidth:    760,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Handler: app.Handler()},
	}); err != nil {
		log.Fatal(err)
	}
}
