package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/deps"
	"github.com/VaDKustiK/ytaudio/internal/queue"
	"github.com/VaDKustiK/ytaudio/internal/update"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
	"github.com/VaDKustiK/ytaudio/internal/ytdlp"
	"github.com/VaDKustiK/ytaudio/packaging"
)

//go:embed web/*
var webFiles embed.FS
var indexTemplate = template.Must(template.New("index").ParseFS(webFiles, "web/index.html"))

type SetupState struct {
	Running bool    `json:"running"`
	Message string  `json:"message"`
	Percent float64 `json:"percent"`
	Error   string  `json:"error,omitempty"`
}
type Job struct {
	ID       string         `json:"id"`
	URL      string         `json:"url"`
	Format   ytdlp.Format   `json:"format"`
	State    string         `json:"state"`
	Message  string         `json:"message"`
	Progress ytdlp.Progress `json:"progress"`
	Created  time.Time      `json:"created"`
	cancel   context.CancelFunc
}
type Server struct {
	mu                     sync.RWMutex
	cfg                    config.Config
	appURL, token, version string
	deps                   *deps.Manager
	statuses               []deps.Status
	setup                  SetupState
	jobs                   map[string]*Job
	queue                  *queue.Queue
	releases               update.Info
	lastLog                string
	ctx                    context.Context
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	closeOnce              sync.Once
	closed                 bool
	pickFolder             func() (string, error)
	openFolder             func(string) error
	openURL                func(string)
}

func New(cfg config.Config, appURL, version string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := deps.New(cfg.Proxy)
	s := &Server{cfg: cfg, appURL: appURL, token: newToken(), version: version, deps: manager, jobs: make(map[string]*Job), queue: queue.New(cfg.DailyCap), ctx: ctx, cancel: cancel, setup: SetupState{Running: true, Message: "Checking download tools…"}, releases: update.Info{Current: version, Checking: true}}
	if err != nil {
		s.setup = SetupState{Error: "Unable to prepare download tools.", Message: "Setup needed"}
		s.lastLog = err.Error()
	}
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		if manager == nil {
			return
		}
		statuses := manager.Status(ctx)
		s.mu.Lock()
		s.statuses = statuses
		needsSetup := !s.readyLocked() && os.Getenv("YTAUDIO_AUTO_SETUP") != "0"
		s.setup.Running = needsSetup
		s.setup.Message = "Download tools checked"
		if needsSetup {
			s.setup.Message = "Preparing download tools for first use…"
		}
		s.mu.Unlock()
		if needsSetup {
			s.prepareTools(manager, false)
			return
		}
		checkCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		available, checkErr := manager.CheckDaily(checkCtx)
		stop()
		if checkErr != nil {
			return
		}
		s.mu.Lock()
		if !available || s.deps != manager || s.busyLocked() || ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		s.setup = SetupState{Running: true, Message: "Updating yt-dlp…"}
		s.mu.Unlock()
		updateCtx, stop := context.WithTimeout(ctx, 3*time.Minute)
		err := manager.Update(updateCtx)
		statuses = manager.Status(updateCtx)
		stop()
		s.mu.Lock()
		s.statuses = statuses
		s.setup = SetupState{Message: "Download tools checked"}
		if err != nil {
			s.lastLog = err.Error()
			s.setup.Error = "The automatic yt-dlp update could not finish. Your installed tools are still available."
		}
		s.mu.Unlock()
	}()
	go func() {
		defer s.wg.Done()
		info := update.CheckWithProxy(ctx, version, cfg.Proxy)
		s.mu.Lock()
		s.releases = info
		s.mu.Unlock()
	}()
	return s
}
func (s *Server) SetDesktopActions(pick func() (string, error), folder func(string) error, open func(string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pickFolder = pick
	s.openFolder = folder
	s.openURL = open
}
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		s.mu.Unlock()
		s.queue.Close()
		s.wg.Wait()
	})
}
func (s *Server) Handler() http.Handler { return securityMiddleware(s.appURL, s.token, s.routes()) }
func (s *Server) DesktopHandler() http.Handler {
	handler := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Ytaudio-Token") != s.token {
			http.Error(w, "Request was not authorized.", http.StatusForbidden)
			return
		}
		handler.ServeHTTP(w, r)
	})
}
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/static/icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		http.ServeContent(w, r, "icon.png", time.Time{}, bytes.NewReader(packaging.Icon))
	})
	mux.HandleFunc("/", s.index)
	files, _ := fs.Sub(webFiles, "web")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(files))))
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("/api/diagnostics", s.diagnostics)
	mux.HandleFunc("/api/settings", s.settings)
	mux.HandleFunc("/api/pick-folder", s.pick)
	mux.HandleFunc("/api/open-folder", s.reveal)
	mux.HandleFunc("/api/open-release", s.openRelease)
	mux.HandleFunc("/api/setup", func(w http.ResponseWriter, r *http.Request) { s.setupTools(w, r, false) })
	mux.HandleFunc("/api/check-updates", func(w http.ResponseWriter, r *http.Request) { s.setupTools(w, r, true) })
	mux.HandleFunc("/api/download", s.startDownload)
	mux.HandleFunc("/api/jobs", s.listJobs)
	mux.HandleFunc("/api/jobs/", s.jobHandler)
	mux.HandleFunc("/api/app-update", s.updates)
	return mux
}
func method(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
	return false
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		http.Error(w, "Invalid request.", http.StatusBadRequest)
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		http.Error(w, "Invalid request.", http.StatusBadRequest)
		return false
	}
	return true
}
func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !method(w, r, "GET") {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = indexTemplate.ExecuteTemplate(w, "index", struct{ Token string }{s.token})
}
func (s *Server) readyLocked() bool {
	if len(s.statuses) != 4 {
		return false
	}
	for _, d := range s.statuses {
		if !d.Ready {
			return false
		}
	}
	return true
}
func (s *Server) busyLocked() bool {
	if s.setup.Running {
		return true
	}
	for _, j := range s.jobs {
		if j.State == "queued" || j.State == "running" {
			return true
		}
	}
	return false
}
func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, struct {
		Version      string        `json:"version"`
		OS           string        `json:"os"`
		Arch         string        `json:"arch"`
		Setup        SetupState    `json:"setup"`
		Dependencies []deps.Status `json:"dependencies"`
		Ready        bool          `json:"ready"`
		LastLog      string        `json:"last_log"`
	}{s.version, runtime.GOOS, runtime.GOARCH, s.setup, s.statuses, s.readyLocked(), s.redact(s.lastLog)})
}
func (s *Server) redact(raw string) string {
	if s.cfg.OutputFolder != "" {
		raw = strings.ReplaceAll(raw, s.cfg.OutputFolder, "<output>")
	}
	if s.cfg.Proxy != "" {
		raw = strings.ReplaceAll(raw, s.cfg.Proxy, "<proxy>")
	}
	if home, err := os.UserHomeDir(); err == nil {
		raw = strings.ReplaceAll(raw, home, "<home>")
	}
	if len(raw) > 16000 {
		raw = raw[len(raw)-16000:]
	}
	return raw
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET", "PUT") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Method == "GET" {
		writeJSON(w, s.cfg)
		return
	}
	if s.busyLocked() {
		http.Error(w, "Wait for downloads or setup to finish before changing settings.", http.StatusConflict)
		return
	}
	var cfg config.Config
	if !decode(w, r, &cfg) {
		return
	}
	if err := cfg.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := ytdlp.OutputFolder(cfg.OutputFolder); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	manager, err := deps.New(cfg.Proxy)
	if err != nil {
		http.Error(w, "Unable to apply proxy settings.", http.StatusBadRequest)
		return
	}
	if err = cfg.Save(); err != nil {
		http.Error(w, "Unable to save settings.", http.StatusInternalServerError)
		return
	}
	s.cfg = cfg
	s.deps = manager
	s.queue.SetLimit(cfg.DailyCap)
	writeJSON(w, cfg)
}
func (s *Server) pick(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	s.mu.RLock()
	pick := s.pickFolder
	s.mu.RUnlock()
	if pick == nil {
		http.Error(w, "Enter the full folder path when using browser mode.", http.StatusConflict)
		return
	}
	folder, err := pick()
	if err != nil {
		http.Error(w, "Unable to open folder picker.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"folder": folder})
}
func (s *Server) reveal(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	s.mu.RLock()
	open, folder := s.openFolder, s.cfg.OutputFolder
	s.mu.RUnlock()
	if open == nil || open(folder) != nil {
		http.Error(w, "Unable to open the output folder.", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) openRelease(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var body struct {
		URL string `json:"url"`
	}
	if !decode(w, r, &body) {
		return
	}
	u, err := url.Parse(body.URL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/VaDKustiK/ytaudio/releases/") {
		http.Error(w, "Invalid release link.", http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	open := s.openURL
	s.mu.RUnlock()
	if open == nil {
		http.Error(w, "Open this release in your browser.", http.StatusConflict)
		return
	}
	open(body.URL)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) setupTools(w http.ResponseWriter, r *http.Request, updateOnly bool) {
	if !method(w, r, "POST") {
		return
	}
	s.mu.Lock()
	if s.closed || s.busyLocked() {
		s.mu.Unlock()
		http.Error(w, "Wait for the current operation to finish.", http.StatusConflict)
		return
	}
	manager := s.deps
	if manager == nil {
		s.mu.Unlock()
		http.Error(w, "Download tools are unavailable. Restart the app.", http.StatusInternalServerError)
		return
	}
	s.setup = SetupState{Running: true, Message: "Preparing download tools…"}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		s.prepareTools(manager, updateOnly)
	}()
	writeJSON(w, SetupState{Running: true, Message: "Preparing download tools…"})
}
func (s *Server) prepareTools(manager *deps.Manager, updateOnly bool) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Minute)
	defer cancel()
	var err error
	if updateOnly {
		err = manager.Update(ctx)
	} else {
		err = manager.Setup(ctx, func(p deps.Progress) {
			s.mu.Lock()
			s.setup.Message = p.Message
			s.setup.Percent = p.Percent
			s.mu.Unlock()
		})
	}
	statuses := manager.Status(ctx)
	s.mu.Lock()
	s.statuses = statuses
	s.setup.Running = false
	s.setup.Percent = 100
	s.setup.Message = "Download tools are ready"
	if err != nil {
		s.lastLog = err.Error()
		s.setup.Error = "Could not prepare the download tools. Check your connection or proxy, then try again."
		s.setup.Message = "Setup needs attention"
		s.setup.Percent = 0
	}
	s.mu.Unlock()
}
func (s *Server) updates(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET", "POST") {
		return
	}
	if r.Method == "POST" {
		s.mu.RLock()
		proxy := s.cfg.Proxy
		s.mu.RUnlock()
		info := update.CheckWithProxy(r.Context(), s.version, proxy)
		s.mu.Lock()
		s.releases = info
		s.mu.Unlock()
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, s.releases)
}
func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var request struct {
		URL             string       `json:"url"`
		Format          ytdlp.Format `json:"format"`
		ConfirmPlaylist bool         `json:"confirm_playlist"`
	}
	if !decode(w, r, &request) {
		return
	}
	normalized, err := urlcheck.Normalize(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, _ := url.Parse(normalized)
	playlist := u.Query().Get("list") != ""
	if playlist && !request.ConfirmPlaylist {
		http.Error(w, "Download this playlist? Up to 100 videos will be processed, one at a time.", http.StatusConflict)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.setup.Running || !s.readyLocked() {
		http.Error(w, "Set up the download tools in Info first.", http.StatusConflict)
		return
	}
	cfg, manager := s.cfg, s.deps
	if err = ytdlp.OutputFolder(cfg.OutputFolder); err != nil {
		http.Error(w, err.Error()+". Choose a folder in Settings.", http.StatusBadRequest)
		return
	}
	args, err := ytdlp.BuildArgsWithPlaylist(normalized, cfg.OutputFolder, request.Format, playlist)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if cfg.Proxy != "" {
		args = append([]string{"--proxy", cfg.Proxy}, args...)
	}
	executable, err := manager.YTDLPPath()
	if err != nil {
		http.Error(w, "Set up download tools first.", http.StatusConflict)
		return
	}
	// Retain only a small session queue; no download history is persisted.
	if len(s.jobs) >= 100 {
		for id, j := range s.jobs {
			if j.State != "queued" && j.State != "running" {
				delete(s.jobs, id)
			}
		}
	}
	if len(s.jobs) >= 100 {
		http.Error(w, "The queue is full. Wait for some downloads to finish.", http.StatusTooManyRequests)
		return
	}
	id := newToken()[:16]
	job := &Job{ID: id, URL: normalized, Format: request.Format, State: "queued", Message: "Waiting in queue", Created: time.Now()}
	s.jobs[id] = job
	queued, err := s.queue.Submit(id, func(ctx context.Context) error { return s.run(ctx, job, manager, executable, args) })
	if err != nil {
		delete(s.jobs, id)
		http.Error(w, "Download limit reached for this session. Adjust it in Settings if needed.", http.StatusTooManyRequests)
		return
	}
	job.cancel = queued.Cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := <-queued.Done
		s.mu.Lock()
		defer s.mu.Unlock()
		if errors.Is(err, context.Canceled) {
			job.State = "cancelled"
			job.Message = "Cancelled"
		}
	}()
	writeJSON(w, map[string]string{"id": id})
}
func (s *Server) run(ctx context.Context, job *Job, manager *deps.Manager, executable string, args []string) error {
	s.mu.Lock()
	job.State = "running"
	job.Message = "Getting video information…"
	s.mu.Unlock()
	report := func(p ytdlp.Progress) {
		s.mu.Lock()
		defer s.mu.Unlock()
		p.File = filepath.Base(p.File)
		if p.File == "." {
			p.File = ""
		}
		job.Progress = p
		switch p.Stage {
		case "downloading":
			job.Message = "Downloading audio"
		case "converting":
			job.Message = "Converting and adding tags…"
		case "saved":
			job.Message = "Saving audio…"
		}
	}
	runner := ytdlp.Runner{Executable: executable, Env: manager.Environment()}
	err := runner.Run(ctx, args, report, io.Discard)
	if err != nil && ctx.Err() == nil && ytdlp.Classify(err.Error()).Retry {
		s.mu.Lock()
		job.Message = "Updating yt-dlp, then retrying once…"
		s.mu.Unlock()
		if updateErr := manager.Update(ctx); updateErr == nil {
			if path, e := manager.YTDLPPath(); e == nil {
				runner.Executable = path
				err = runner.Run(ctx, args, report, io.Discard)
			}
		} else {
			err = fmt.Errorf("%w; tool update failed: %v", err, updateErr)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		job.State = "cancelled"
		job.Message = "Cancelled"
		return ctx.Err()
	}
	if err != nil {
		job.State = "failed"
		job.Message = ytdlp.Classify(err.Error()).Message
		s.lastLog = err.Error()
	} else {
		job.State = "complete"
		job.Message = "Audio saved"
		job.Progress.Percent = 100
	}
	return err
}
func (s *Server) jobSnapshotLocked() []Job {
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Created.Before(jobs[j].Created) })
	return jobs
}
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, s.jobSnapshotLocked())
}
func (s *Server) jobHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "DELETE") {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if job.State == "queued" || job.State == "running" {
		job.cancel()
		job.Message = "Cancelling…"
	} else {
		delete(s.jobs, id)
	}
	w.WriteHeader(http.StatusNoContent)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func newToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic("unable to create session token")
	}
	return fmt.Sprintf("%x", token)
}
func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}
func securityMiddleware(appURL, token string, next http.Handler) http.Handler {
	allowed, _ := url.Parse(appURL)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		if r.Host != allowed.Host {
			http.Error(w, "Not found.", http.StatusNotFound)
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("X-Ytaudio-Token") != token || !sameOrigin(r.Header.Get("Origin"), allowed) {
				http.Error(w, "Request was not authorized.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func sameOrigin(origin string, allowed *url.URL) bool {
	p, err := url.Parse(origin)
	return err == nil && p.User == nil && p.Scheme == allowed.Scheme && p.Host == allowed.Host && strings.TrimRight(p.Path, "/") == strings.TrimRight(allowed.Path, "/") && p.RawQuery == "" && p.Fragment == ""
}
