package server

import (
	"context"
	"net/http"
	"time"

	"github.com/VaDKustiK/ytaudio/internal/deps"
	"github.com/VaDKustiK/ytaudio/internal/urlcheck"
)

func (s *Server) downloadLink(ctx context.Context, raw string, source urlcheck.Source) (urlcheck.Link, error) {
	// Validate before constructing a proxy client or making any network request.
	link, err := urlcheck.Parse(raw, source)
	if err != nil || !link.Short {
		return link, err
	}
	s.mu.RLock()
	proxy := s.cfg.Proxy
	s.mu.RUnlock()
	client, err := deps.HTTPClient(proxy)
	if err != nil {
		return urlcheck.Link{}, err
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return urlcheck.Resolve(ctx, client, link.URL, link.Source)
}

func (s *Server) resolveLink(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var request struct {
		URL    string          `json:"url"`
		Source urlcheck.Source `json:"source"`
	}
	if !decode(w, r, &request) {
		return
	}
	link, err := s.downloadLink(r.Context(), request.URL, request.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, link)
}
