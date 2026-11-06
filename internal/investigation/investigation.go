// Package investigation coordinates read-only evidence collection and analysis.
package investigation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

type Evidence struct {
	Cluster  string   `json:"cluster,omitempty"`
	Pod      string   `json:"pod"`
	Events   string   `json:"events"`
	Logs     string   `json:"logs"`
	Warnings []string `json:"warnings"`
}
type Collector interface {
	Collect(context.Context, string, string) (Evidence, error)
}
type ClusterCollector interface {
	CollectCluster(context.Context) (Evidence, error)
}
type Analyzer interface {
	Analyze(context.Context, incidents.Incident, Evidence) (string, error)
}
type Result struct {
	ID         string    `json:"id"`
	IncidentID string    `json:"incidentId"`
	Status     string    `json:"status"`
	Evidence   Evidence  `json:"evidence"`
	Analysis   string    `json:"analysis,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}
type Handler struct {
	Incidents *incidents.Store
	Collector Collector
	Analyzer  Analyzer
	mu        sync.RWMutex
	results   map[string]Result
}

func (h *Handler) Routes(mux *http.ServeMux) {
	h.results = make(map[string]Result)
	mux.HandleFunc("POST /api/incidents/{id}/investigations", h.run)
	mux.HandleFunc("GET /api/incidents/{id}/investigations", h.list)
	mux.HandleFunc("GET /api/investigations/{id}", h.get)
}
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	item, ok := h.Incidents.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, "cannot create investigation", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result := Result{ID: hex.EncodeToString(id[:]), IncidentID: item.ID, Status: "failed", CreatedAt: time.Now().UTC()}
	var evidence Evidence
	var err error
	if item.Scope == "cluster" {
		collector, ok := h.Collector.(ClusterCollector)
		if !ok {
			err = errors.New("cluster investigations are unavailable")
		} else {
			evidence, err = collector.CollectCluster(ctx)
		}
	} else {
		evidence, err = h.Collector.Collect(ctx, item.Namespace, item.Pod)
	}
	result.Evidence = evidence
	if err == nil {
		result.Analysis, err = h.Analyzer.Analyze(ctx, item, evidence)
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			result.Error = "investigation timed out"
		} else {
			result.Error = err.Error()
		}
	} else {
		result.Status = "completed"
	}
	h.mu.Lock()
	h.results[result.ID] = result
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(result)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.Incidents.Get(r.PathValue("id")); !ok {
		http.NotFound(w, r)
		return
	}
	h.mu.RLock()
	items := make([]Result, 0)
	for _, item := range h.results {
		if item.IncidentID == r.PathValue("id") {
			items = append(items, item)
		}
	}
	h.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	item, ok := h.results[r.PathValue("id")]
	h.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}
