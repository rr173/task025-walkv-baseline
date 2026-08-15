// Package httpapi exposes the WAL KV store over HTTP.
package httpapi

import (
	"encoding/json"
	"net/http"

	"task025-walkv/internal/walstore"
)

// Server wires a walstore.Store to HTTP handlers.
type Server struct {
	store *walstore.Store
	stats map[string]int // per-endpoint hit counter (unsynchronized)
}

// NewServer creates a Server backed by store.
func NewServer(store *walstore.Store) *Server {
	return &Server{store: store, stats: make(map[string]int)}
}

// Handler returns the HTTP mux for the store.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/set", s.handleSet)
	mux.HandleFunc("/get", s.handleGet)
	mux.HandleFunc("/delete", s.handleDelete)
	mux.HandleFunc("/compact", s.handleCompact)
	mux.HandleFunc("/stats", s.handleStats)
	return mux
}

type setRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

type getResponse struct {
	Found bool   `json:"found"`
	Value string `json:"value,omitempty"`
}

type statsResponse struct {
	Keys     int   `json:"keys"`
	WALBytes int64 `json:"wal_bytes"`
}

func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	s.stats["set"]++
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "empty key", http.StatusBadRequest)
		return
	}
	if err := s.store.Set([]byte(req.Key), []byte(req.Value)); err != nil {
		http.Error(w, "set failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	s.stats["get"]++
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}
	v, ok := s.store.Get([]byte(key))
	if !ok {
		writeJSON(w, http.StatusOK, getResponse{Found: false})
		return
	}
	writeJSON(w, http.StatusOK, getResponse{Found: true, Value: string(v)})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	s.stats["delete"]++
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req setRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "empty key", http.StatusBadRequest)
		return
	}
	if err := s.store.Delete([]byte(req.Key)); err != nil {
		http.Error(w, "delete failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
	s.stats["compact"]++
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.store.Compact(); err != nil {
		http.Error(w, "compact failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.stats["stats"]++
	size, err := s.store.WALSize()
	if err != nil {
		http.Error(w, "stat failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, statsResponse{
		Keys:     s.store.Len(),
		WALBytes: size,
	})
}

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
