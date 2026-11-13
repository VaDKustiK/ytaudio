package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzerSelectAndState(t *testing.T) {
	s := testServer(t)
	path := filepath.Join(t.TempDir(), "music.wav")
	_ = os.WriteFile(path, []byte("audio"), 0600)
	body, _ := json.Marshal(map[string]string{"path": path})
	w := request(s, "POST", "/api/analysis/select", string(body))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request(s, "GET", "/api/analysis", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w = request(s, "GET", "/api/analysis/select", ""); w.Code != 405 {
		t.Fatal("select should require POST")
	}
	if w = request(s, "POST", "/api/analysis", `{"file_id":"missing"}`); w.Code != 409 {
		t.Fatal("missing analyzer accepted")
	}
}

func TestAnalyzerPerformanceCanChangeDuringDownload(t *testing.T) {
	s := testServer(t)
	s.mu.Lock()
	s.jobs["download"] = &Job{State: "running"}
	manager := s.deps
	cfg := s.cfg
	cfg.AnalyzerPerformance = "low"
	s.mu.Unlock()
	body, _ := json.Marshal(cfg)
	w := request(s, "PUT", "/api/settings", string(body))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.AnalyzerPerformance != "low" || s.deps != manager {
		t.Fatal("performance setting changed the active download manager")
	}
}
