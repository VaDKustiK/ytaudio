package server

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/update"
)

type appInstallStatus struct {
	Running  bool   `json:"running"`
	Percent  int    `json:"percent"`
	Message  string `json:"message,omitempty"`
	Error    bool   `json:"error,omitempty"`
	Reopened bool   `json:"reopened,omitempty"`
}

func (s *Server) SetAppUpdater(target update.Target, result string, quit func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateTarget, s.updateQuit = target, quit
	switch result {
	case "installed":
		s.appInstall = appInstallStatus{Message: "Updated to " + s.version + ".", Reopened: true}
	case "failed":
		s.appInstall = appInstallStatus{Message: "The update could not be installed. Try again or use the release page.", Error: true, Reopened: true}
	}
}

func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET", "POST") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == "GET" {
		writeJSON(w, s.appInstall)
		return
	}
	if s.closed || s.busyLocked() {
		http.Error(w, "Wait for downloads or setup to finish before updating.", http.StatusConflict)
		return
	}
	if s.analysis != nil {
		state := s.analysis.Snapshot()
		if state.Installing || state.Status == "queued" || state.Status == "running" {
			http.Error(w, "Wait for the analyzer to finish before updating.", http.StatusConflict)
			return
		}
	}
	target, quit, info, proxy := s.updateTarget, s.updateQuit, s.releases, s.cfg.Proxy
	if _, available := target.Asset(info); quit == nil || !available {
		http.Error(w, "In-app installation is unavailable for this release and installation type.", http.StatusConflict)
		return
	}
	s.appInstall = appInstallStatus{Running: true, Message: "Downloading " + info.Latest + "…"}
	s.wg.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
		defer cancel()
		err := target.Stage(ctx, info, proxy, func(percent int) {
			s.mu.Lock()
			s.appInstall.Percent = percent
			s.mu.Unlock()
		})
		s.mu.Lock()
		if err == nil {
			s.appInstall.Percent = 100
			s.appInstall.Message = "Installing update and reopening ytaudio…"
		} else {
			log.Printf("app update: %v", err)
			s.appInstall = appInstallStatus{Message: "Could not prepare the update. Check your connection and try again.", Error: true}
		}
		s.mu.Unlock()
		s.wg.Done()
		if err == nil {
			quit()
		}
	}()
	writeJSON(w, s.appInstall)
}
