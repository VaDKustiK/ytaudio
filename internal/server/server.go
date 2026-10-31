package server

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/config"
	"github.com/VaDKustiK/ytaudio/internal/deps"
	"github.com/VaDKustiK/ytaudio/internal/queue"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
	"github.com/VaDKustiK/ytaudio/internal/ytdlp"
)

//go:embed web/*
var webFiles embed.FS

type Server struct {
	config  ConfigView
	appURL  string
	token   string
	deps    *deps.Manager
	mu      sync.RWMutex
	setup   SetupState
	jobs    map[string]*downloadJob
	queue   *queue.Queue
	history []HistoryItem
}

type ConfigView struct {
	OutputFolder string
	Format       string
}

func New(cfg config.Config, appURL string) *Server {
	manager, managerErr := deps.New(cfg.Proxy)
	setup := SetupState{}
	if managerErr != nil {
		setup.Error = managerErr.Error()
	}

	return &Server{
		config: ConfigView{OutputFolder: cfg.OutputFolder, Format: cfg.Format},
		appURL: appURL,
		token:  newToken(),
		deps:   manager,
		setup:  setup,
		jobs:   make(map[string]*downloadJob),
		queue:  queue.New(cfg.DailyCap),
	}
}

func (s *Server) Close() {
	if s.queue != nil {
		s.queue.Close()
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/diagnostics", s.diagnostics)
	mux.HandleFunc("/api/setup", s.setupDependencies)
	mux.HandleFunc("/api/check-updates", s.checkUpdates)
	mux.HandleFunc("/api/download", s.startDownload)
	mux.HandleFunc("/api/jobs/", s.jobEvents)
	mux.HandleFunc("/api/history", s.historyHandler)
	return securityMiddleware(s.appURL, s.token, mux)
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := template.Must(template.New("index").ParseFS(webFiles, "web/index.html")).ExecuteTemplate(w, "index", struct {
		Token        string
		OutputFolder string
		Format       string
	}{s.token, s.config.OutputFolder, s.config.Format}); err != nil {
		http.Error(w, "Unable to load the application.", http.StatusInternalServerError)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status":"ok"}`)
}

type SetupState struct {
	Running bool    `json:"running"`
	Message string  `json:"message,omitempty"`
	Percent float64 `json:"percent"`
	Bytes   int64   `json:"bytes,omitempty"`
	Total   int64   `json:"total,omitempty"`
	Error   string  `json:"error,omitempty"`
}

func (s *Server) diagnostics(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	setup := s.setup
	s.mu.RUnlock()
	response := struct {
		OS         string        `json:"os"`
		Arch       string        `json:"arch"`
		Setup      SetupState    `json:"setup"`
		Dependents []deps.Status `json:"dependencies"`
	}{OS: runtime.GOOS, Arch: runtime.GOARCH, Setup: setup}
	if s.deps != nil {
		response.Dependents = s.deps.Status(r.Context())
	}
	writeJSON(w, response)
}

func (s *Server) setupDependencies(w http.ResponseWriter, r *http.Request) {
	if s.deps == nil {
		http.Error(w, "Dependency manager is unavailable.", http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	if s.setup.Running {
		s.mu.Unlock()
		writeJSON(w, s.setup)
		return
	}
	s.setup = SetupState{Running: true, Message: "Starting dependency setup"}
	s.mu.Unlock()
	go func() {
		setupContext, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		err := s.deps.Setup(setupContext, func(progress deps.Progress) {
			s.mu.Lock()
			s.setup.Message = progress.Message
			s.setup.Percent = progress.Percent
			s.setup.Bytes = progress.Bytes
			s.setup.Total = progress.Total
			s.mu.Unlock()
			log.Printf("setup: %s %.1f%%", progress.Message, progress.Percent)
		})
		s.mu.Lock()
		s.setup.Running = false
		if err != nil {
			s.setup.Error = err.Error()
			s.setup.Message = "Setup failed"
		} else {
			s.setup.Error = ""
			s.setup.Message = "Setup complete"
		}
		s.mu.Unlock()
	}()
	writeJSON(w, SetupState{Running: true, Message: "Starting dependency setup"})
}

func (s *Server) checkUpdates(w http.ResponseWriter, r *http.Request) {
	if s.deps == nil {
		http.Error(w, "Dependency manager is unavailable.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.deps.Status(r.Context()))
}

type downloadRequest struct {
	URL             string       `json:"url"`
	Format          ytdlp.Format `json:"format"`
	ConfirmPlaylist bool         `json:"confirm_playlist"`
}

type downloadJob struct {
	ID     string
	Events chan ytdlp.Progress
	Done   chan struct{}
	Cancel context.CancelFunc
	Error  string
	Queued bool
}

type HistoryItem struct {
	ID      string `json:"id"`
	URL     string `json:"url"`
	Format  string `json:"format"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	var request downloadRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Enter a YouTube URL.", http.StatusBadRequest)
		return
	}
	normalized, err := urlcheck.Normalize(request.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	parsedURL, _ := url.Parse(normalized)
	isPlaylist := parsedURL.Query().Get("list") != ""
	if isPlaylist && !request.ConfirmPlaylist {
		http.Error(w, "This URL is a playlist. Confirm playlist download to continue (maximum 100 items).", http.StatusConflict)
		return
	}
	if err := ytdlp.OutputFolder(s.config.OutputFolder); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.deps == nil {
		http.Error(w, "Dependency manager is unavailable.", http.StatusInternalServerError)
		return
	}
	executable, err := s.deps.YTDLPPath()
	if err != nil {
		http.Error(w, "Complete dependency setup before downloading.", http.StatusConflict)
		return
	}
	id := newToken()[:16]
	job := &downloadJob{ID: id, Events: make(chan ytdlp.Progress, 16), Done: make(chan struct{}), Queued: true}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()
	queued, err := s.queue.Submit(id, func(ctx context.Context) error {
		job.Queued = false
		return s.runDownload(ctx, job, executable, normalized, request.Format, isPlaylist)
	})
	if err != nil {
		s.mu.Lock()
		delete(s.jobs, id)
		s.mu.Unlock()
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	job.Cancel = queued.Cancel
	writeJSON(w, map[string]string{"id": id})
}

func (s *Server) runDownload(ctx context.Context, job *downloadJob, executable, rawURL string, format ytdlp.Format, playlist bool) error {
	defer close(job.Done)
	args, err := ytdlp.BuildArgsWithPlaylist(rawURL, s.config.OutputFolder, format, playlist)
	if err == nil {
		err = (ytdlp.Runner{Executable: executable}).Run(ctx, args, func(progress ytdlp.Progress) {
			select {
			case job.Events <- progress:
			default:
			}
		}, io.Discard)
		if err != nil && ytdlp.Classify(err.Error()).Retry && s.deps != nil {
			_ = s.deps.Update(ctx)
			if updated, pathErr := s.deps.YTDLPPath(); pathErr == nil {
				err = (ytdlp.Runner{Executable: updated}).Run(ctx, args, func(progress ytdlp.Progress) {
					select {
					case job.Events <- progress:
					default:
					}
				}, io.Discard)
			}
		}
	}
	if err != nil {
		classified := ytdlp.Classify(err.Error())
		job.Error = classified.Message
		s.mu.Lock()
		s.history = append(s.history, HistoryItem{ID: job.ID, URL: rawURL, Format: string(format), Status: "failed", Message: classified.Message})
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		s.history = append(s.history, HistoryItem{ID: job.ID, URL: rawURL, Format: string(format), Status: "complete"})
		s.mu.Unlock()
	}
	close(job.Events)
	return err
}

func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/jobs/")
	s.mu.RLock()
	job, ok := s.jobs[id]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		if job.Cancel != nil {
			job.Cancel()
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is unavailable.", http.StatusInternalServerError)
		return
	}
	for progress := range job.Events {
		data, _ := json.Marshal(progress)
		fmt.Fprintf(w, "event: progress\ndata: %s\n\n", data)
		flusher.Flush()
	}

	if job.Error != "" {
		data, _ := json.Marshal(map[string]string{"error": job.Error})
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
	} else {
		fmt.Fprint(w, "event: complete\ndata: {}\n\n")
	}
	flusher.Flush()
}

func (s *Server) historyHandler(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	history := append([]HistoryItem(nil), s.history...)
	s.mu.RUnlock()
	if history == nil {
		history = []HistoryItem{}
	}
	writeJSON(w, history)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, "Unable to encode response.", http.StatusInternalServerError)
	}
}

func newToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic("unable to create session token")
	}
	return fmt.Sprintf("%x", token)
}

func securityMiddleware(appURL, token string, next http.Handler) http.Handler {
	allowedOrigin, err := url.Parse(appURL)
	if err != nil {
		panic(err)
	}
	allowedHost := allowedOrigin.Host
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != allowedHost {
			http.Error(w, "Not found.", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if r.Header.Get("X-Ytaudio-Token") != token || !sameOrigin(r.Header.Get("Origin"), allowedOrigin) {
				http.Error(w, "Request was not authorized.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func sameOrigin(origin string, allowed *url.URL) bool {
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Scheme == allowed.Scheme && parsed.Host == allowed.Host && strings.TrimRight(parsed.Path, "/") == strings.TrimRight(allowed.Path, "/")
}
