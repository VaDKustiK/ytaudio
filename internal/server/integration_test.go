package server

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/VaDKustiK/ytaudio/packaging"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/deps"
	"github.com/VaDKustiK/ytaudio/internal/queue"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	t.Setenv("YTAUDIO_CACHE_DIR", root)
	t.Setenv("YTAUDIO_CONFIG_DIR", root)
	t.Setenv("APPDATA", root)
	cfg := config.Defaults()
	cfg.OutputFolder = t.TempDir()
	manager, err := deps.New("")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, appURL: "http://127.0.0.1:8750/", token: "token", deps: manager, jobs: make(map[string]*Job), queue: queue.New(30), ctx: ctx, cancel: cancel}
	t.Cleanup(s.Close)
	return s
}
func request(s *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8750"+path, strings.NewReader(body))
	r.Header.Set("X-Ytaudio-Token", "token")
	r.Header.Set("Origin", "http://127.0.0.1:8750")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}
func TestMutatingEndpointsRejectGet(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/api/setup", "/api/check-updates", "/api/download", "/api/pick-folder", "/api/open-folder", "/api/open-release"} {
		w := request(s, "GET", path, "")
		if w.Code != 405 {
			t.Errorf("%s=%d", path, w.Code)
		}
	}
}
func TestSettingsSaveAndValidation(t *testing.T) {
	s := testServer(t)
	cfg := s.cfg
	cfg.Format = "ogg"
	body, _ := json.Marshal(cfg)
	w := request(s, "PUT", "/api/settings", string(body))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	got, err := config.Load()
	if err != nil || got.Format != "ogg" {
		t.Fatalf("%#v %v", got, err)
	}
	cfg.Proxy = "ftp://bad"
	body, _ = json.Marshal(cfg)
	if request(s, "PUT", "/api/settings", string(body)).Code != 400 {
		t.Fatal("invalid proxy accepted")
	}
	s.mu.Lock()
	s.jobs["1"] = &Job{State: "queued"}
	s.mu.Unlock()
	if request(s, "PUT", "/api/settings", string(body)).Code != 409 {
		t.Fatal("changed settings with queued work")
	}
}
func TestNoHistoryAndEmptyJobs(t *testing.T) {
	s := testServer(t)
	if request(s, "GET", "/api/history", "").Code != 404 {
		t.Fatal("history endpoint remains")
	}
	w := request(s, "GET", "/api/jobs", "")
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal(w.Body.String())
	}
}
func TestDesktopMutationsRequireToken(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest("POST", "http://wails.localhost/api/setup", nil)
	w := httptest.NewRecorder()
	s.DesktopHandler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestDiagnosticsRedactsPathsAndProxy(t *testing.T) {
	s := testServer(t)
	s.cfg.Proxy = "http://alice:secret@localhost:8888"
	s.lastLog = "output " + s.cfg.OutputFolder + " proxy " + s.cfg.Proxy
	w := request(s, "GET", "/api/diagnostics", "")
	if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), s.cfg.OutputFolder) {
		t.Fatal(w.Body.String())
	}
}
func TestRejectExternalReleaseURLs(t *testing.T) {
	s := testServer(t)
	for _, url := range []string{"javascript:bad", "https://github.com.evil/VaDKustiK/ytaudio/releases/tag/v1", "https://github.com/other/repo/releases/tag/v1"} {
		body, _ := json.Marshal(map[string]string{"url": url})
		if request(s, http.MethodPost, "/api/open-release", string(body)).Code != 400 {
			t.Fatal(url)
		}
	}
}
func TestOversizedAndUnknownFieldsRejected(t *testing.T) {
	s := testServer(t)
	for _, body := range []string{`{"url":"https://youtu.be/abc","flags":"--exec"}`, strings.Repeat("x", 20000)} {
		if request(s, "POST", "/api/download", body).Code != 400 {
			t.Fatal("bad request accepted")
		}
	}
}

func TestSharedArtworkServed(t *testing.T) {
	s := testServer(t)
	response := request(s, "GET", "/static/icon.png", "")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), packaging.Icon) {
		t.Fatal("UI must serve the shared application PNG")
	}
}

func TestFirstLaunchAutomaticallyAttemptsSetupAndCanRetry(t *testing.T) {
	t.Setenv("YTAUDIO_CACHE_DIR", t.TempDir())
	t.Setenv("YTAUDIO_CONFIG_DIR", t.TempDir())
	t.Setenv("YTAUDIO_AUTO_SETUP", "1")
	// Ensure that no system tool can satisfy the first-launch check.
	t.Setenv("PATH", t.TempDir())
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline fixture", http.StatusServiceUnavailable)
	}))
	defer proxy.Close()
	cfg := config.Defaults()
	cfg.Proxy = proxy.URL
	s := New(cfg, "http://127.0.0.1:8750/", "dev")
	defer s.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.RLock()
		state := s.setup
		s.mu.RUnlock()
		if !state.Running {
			if state.Error == "" || state.Message != "Setup needs attention" {
				t.Fatalf("missing actionable setup error: %+v", state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic setup did not finish after network failure")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r := httptest.NewRequest("POST", "/api/setup", nil)
	w := httptest.NewRecorder()
	s.setupTools(w, r, false)
	if w.Code != http.StatusOK {
		t.Fatalf("repair unavailable after automatic setup failure: %s", w.Body.String())
	}
}
