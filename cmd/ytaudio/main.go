//go:build !desktop

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/server"
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

	listener, port, err := listen(cfg.Port)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	appURL := fmt.Sprintf("http://127.0.0.1:%d/", port)
	srv := server.New(cfg, appURL)
	defer srv.Close()
	httpServer := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}

	log.Printf("ytaudio %s listening at %s", version, appURL)
	openBrowser(appURL)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func listen(preferredPort int) (net.Listener, int, error) {
	if preferredPort < 1 || preferredPort > 65535 {
		preferredPort = config.DefaultPort
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err == nil {
		return listener, listener.Addr().(*net.TCPAddr).Port, nil
	}

	listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf("listen on localhost: %w", err)
	}
	return listener, listener.Addr().(*net.TCPAddr).Port, nil
}

func openBrowser(url string) {
	var command string
	var args []string
	switch runtime.GOOS {
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		command, args = "open", []string{url}
	default:
		command, args = "xdg-open", []string{url}
	}
	if err := startProcess(command, args); err != nil {
		log.Printf("open %s in your browser", url)
	}
}
