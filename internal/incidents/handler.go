// Package incidents provides the initial in-memory incident API.
package incidents

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/audit"
)

type Incident struct {
	Scope     string    `json:"scope"`
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	CreatedAt time.Time `json:"createdAt"`
}

type Store struct {
	mu    sync.RWMutex
	items map[string]Incident
	Audit *audit.Journal
}

func New() *Store { return &Store{items: make(map[string]Incident)} }
func (s *Store) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/incidents", s.create)
	mux.HandleFunc("GET /api/incidents", s.list)
	mux.HandleFunc("GET /api/incidents/{id}", s.get)
}
func (s *Store) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Scope     string `json:"scope"`
		Title     string `json:"title"`
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.Pod = strings.TrimSpace(input.Pod)
	if input.Scope == "" {
		input.Scope = "pod"
	}
	if input.Title == "" || (input.Scope != "pod" && input.Scope != "cluster") || (input.Scope == "pod" && (input.Namespace == "" || input.Pod == "")) {
		http.Error(w, "title and a valid scope are required; pod scope also needs namespace and pod", http.StatusBadRequest)
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, "cannot create incident", http.StatusInternalServerError)
		return
	}
	item := Incident{Scope: input.Scope, ID: hex.EncodeToString(id[:]), Title: input.Title, Namespace: input.Namespace, Pod: input.Pod, CreatedAt: time.Now().UTC()}
	if s.Audit != nil {
		if err := s.Audit.Append(audit.Event{Kind: "incident.created", IncidentID: item.ID, Status: item.Scope}); err != nil {
			http.Error(w, "audit unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	s.mu.Lock()
	s.items[item.ID] = item
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(item)
}
func (s *Store) list(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	items := make([]Incident, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}
func (s *Store) Get(id string) (Incident, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[id]
	return item, ok
}
func (s *Store) get(w http.ResponseWriter, r *http.Request) {
	item, ok := s.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}
