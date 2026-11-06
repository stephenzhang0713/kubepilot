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
	Scope              string    `json:"scope"`
	ID                 string    `json:"id"`
	Title              string    `json:"title"`
	Namespace          string    `json:"namespace"`
	Pod                string    `json:"pod"`
	CreatedAt          time.Time `json:"createdAt"`
	AlertFingerprint   string    `json:"alertFingerprint,omitempty"`
	AlertStartsAt      time.Time `json:"alertStartsAt"`
	AlertStatus        string    `json:"alertStatus,omitempty"`
	AlertInvestigation string    `json:"alertInvestigation,omitempty"`
}

type Persistence interface {
	Put(string, string, []byte) error
	Load(string) ([][]byte, error)
}

type Store struct {
	mu          sync.RWMutex
	items       map[string]Incident
	Audit       *audit.Journal
	Persistence Persistence
}

func New() *Store { return &Store{items: make(map[string]Incident)} }
func (s *Store) Restore(p Persistence) error {
	records, err := p.Load("incident")
	if err != nil {
		return err
	}
	for _, record := range records {
		var item Incident
		if err := json.Unmarshal(record, &item); err != nil {
			return err
		}
		s.items[item.ID] = item
	}
	s.Persistence = p
	return nil
}
func (s *Store) save(item Incident) error {
	if s.Persistence != nil {
		data, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err = s.Persistence.Put("incident", item.ID, data); err != nil {
			return err
		}
	}
	s.items[item.ID] = item
	return nil
}

// UpsertAlert deduplicates repeated notifications for the same alert occurrence.
func (s *Store) UpsertAlert(fingerprint, title, namespace, pod, status string, startsAt time.Time) (Incident, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.AlertFingerprint == fingerprint && item.AlertStartsAt.Equal(startsAt) {
			// Late firing notifications must not reopen a resolved occurrence.
			if item.AlertStatus != "resolved" && status == "resolved" {
				item.AlertStatus = status
				if err := s.save(item); err != nil {
					return Incident{}, false, err
				}
			}
			return item, false, nil
		}
	}
	if status != "firing" {
		return Incident{}, false, nil
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Incident{}, false, err
	}
	scope := "cluster"
	if namespace != "" && pod != "" {
		scope = "pod"
	}
	item := Incident{ID: hex.EncodeToString(id[:]), Title: title, Scope: scope, Namespace: namespace, Pod: pod, CreatedAt: time.Now().UTC(), AlertFingerprint: fingerprint, AlertStartsAt: startsAt, AlertStatus: status, AlertInvestigation: "pending"}
	if err := s.save(item); err != nil {
		return Incident{}, false, err
	}
	return item, true, nil
}
func (s *Store) PendingAlerts() []Incident {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Incident
	for _, item := range s.items {
		if item.AlertInvestigation == "pending" {
			out = append(out, item)
		}
	}
	return out
}
func (s *Store) FinishAlert(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok || item.AlertInvestigation != "pending" {
		return nil
	}
	item.AlertInvestigation = "completed"
	return s.save(item)
}
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
	err := s.save(item)
	s.mu.Unlock()
	if err != nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
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
