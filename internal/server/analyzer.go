package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/VaDKustiK/ytaudio/internal/analyzer"
)

func (s *Server) analyzerLocked() (*analyzer.Manager, error) {
	if s.analysis != nil {
		return s.analysis, nil
	}
	if s.closed {
		return nil, fmt.Errorf("app is closing")
	}
	m, err := analyzer.New(s.ctx, s.version, s.cfg.Proxy)
	if err == nil {
		s.analysis = m
	}
	return m, err
}
func (s *Server) analyzerManager() (*analyzer.Manager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.analyzerLocked()
}
func (s *Server) SelectAudioPath(path string) (analyzer.File, error) {
	m, err := s.analyzerManager()
	if err != nil {
		return analyzer.File{}, err
	}
	return m.Register(path, false)
}
func (s *Server) SetAudioPicker(pick func() (string, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pickAudio = pick
}
func (s *Server) analysisHandler(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET", "POST", "DELETE") {
		return
	}
	m, err := s.analyzerManager()
	if err != nil {
		http.Error(w, "Unable to prepare analyzer storage.", http.StatusInternalServerError)
		return
	}
	switch r.Method {
	case "GET":
		writeJSON(w, m.Snapshot())
	case "DELETE":
		m.Cancel()
		w.WriteHeader(http.StatusNoContent)
	case "POST":
		var request struct {
			FileID string `json:"file_id"`
		}
		if !decode(w, r, &request) {
			return
		}
		s.mu.RLock()
		tools, mode := s.deps, s.cfg.AnalyzerPerformance
		s.mu.RUnlock()
		ffmpeg := ""
		if tools != nil {
			ffmpeg = tools.FFmpegPath()
		}
		state, err := m.Start(request.FileID, ffmpeg, mode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, state)
	}
}
func (s *Server) analysisSelect(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var request struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &request) {
		return
	}
	file, err := s.SelectAudioPath(request.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, file)
}
func (s *Server) analysisPick(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	s.mu.RLock()
	pick := s.pickAudio
	s.mu.RUnlock()
	if pick == nil {
		http.Error(w, "Use the file selector in browser mode.", http.StatusConflict)
		return
	}
	path, err := pick()
	if err != nil {
		http.Error(w, "Unable to open audio picker.", http.StatusInternalServerError)
		return
	}
	if path == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	file, err := s.SelectAudioPath(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, file)
}
func (s *Server) analysisUpload(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	name := filepath.Base(r.URL.Query().Get("name"))
	extension := strings.ToLower(filepath.Ext(name))
	if extension == "" || len(extension) > 8 {
		http.Error(w, "Choose an audio file.", http.StatusBadRequest)
		return
	}
	m, err := s.analyzerManager()
	if err != nil {
		http.Error(w, "Analyzer storage is unavailable.", http.StatusInternalServerError)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
	file, err := os.CreateTemp("", "ytaudio-audio-*"+extension)
	if err != nil {
		http.Error(w, "Unable to prepare audio.", http.StatusInternalServerError)
		return
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	_, err = io.Copy(file, r.Body)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		http.Error(w, "Unable to read audio (browser limit: 256 MB).", http.StatusBadRequest)
		return
	}
	selected, err := m.RegisterUpload(path, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	keep = true
	selected.Name = name
	writeJSON(w, selected)
}
func (s *Server) analysisInstall(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	m, err := s.analyzerManager()
	if err == nil {
		err = m.Install()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, m.Snapshot())
}
