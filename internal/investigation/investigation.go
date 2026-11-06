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
	CollectedAt    string   `json:"collectedAt,omitempty"`
	ClusterContext string   `json:"clusterContext,omitempty"`
	Cluster        string   `json:"cluster,omitempty"`
	Pod            string   `json:"pod"`
	Events         string   `json:"events"`
	Logs           string   `json:"logs"`
	Warnings       []string `json:"warnings"`
}
type Collector interface {
	Collect(context.Context, string, string) (Evidence, error)
}
type ClusterCollector interface {
	CollectCluster(context.Context) (Evidence, error)
}
type Candidate struct {
	Action    string `json:"action"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}
type ProposalAnalyzer interface {
	AnalyzeWithProposals(context.Context, incidents.Incident, Evidence) (string, []Candidate, error)
}
type TraceEvent struct {
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	Outcome string    `json:"outcome"`
}
type HarnessAnalyzer interface {
	AnalyzeWithTrace(context.Context, incidents.Incident, Evidence) (string, []Candidate, []TraceEvent, error)
}
type RevisionProvider interface{ Revision() uint64 }
type ContextProvider interface{ InvestigationContext() (string, uint64) }
type Analyzer interface {
	Analyze(context.Context, incidents.Incident, Evidence) (string, error)
}
type Result struct {
	ID              string       `json:"id"`
	ClusterRevision uint64       `json:"clusterRevision"`
	Candidates      []Candidate  `json:"candidates,omitempty"`
	IncidentID      string       `json:"incidentId"`
	Status          string       `json:"status"`
	Evidence        Evidence     `json:"evidence"`
	Analysis        string       `json:"analysis,omitempty"`
	Error           string       `json:"error,omitempty"`
	CreatedAt       time.Time    `json:"createdAt"`
	Trace           []TraceEvent `json:"trace,omitempty"`
}
type Handler struct {
	Incidents *incidents.Store
	Collector Collector
	Analyzer  Analyzer
	mu        sync.RWMutex
	results   map[string]Result
}

func (h *Handler) Routes(mux *http.ServeMux) {
	if h.results == nil {
		h.results = make(map[string]Result)
	}
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
	var clusterContext string
	if v, ok := h.Collector.(ContextProvider); ok {
		clusterContext, result.ClusterRevision = v.InvestigationContext()
	} else if v, ok := h.Collector.(RevisionProvider); ok {
		result.ClusterRevision = v.Revision()
	}
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
	evidence.ClusterContext = clusterContext
	result.Evidence = evidence
	if err == nil {

		evidence.CollectedAt = time.Now().UTC().Format(time.RFC3339)
		result.Evidence = evidence
		if a, ok := h.Analyzer.(HarnessAnalyzer); ok {
			result.Analysis, result.Candidates, result.Trace, err = a.AnalyzeWithTrace(ctx, item, evidence)
		} else if a, ok := h.Analyzer.(ProposalAnalyzer); ok {
			result.Analysis, result.Candidates, err = a.AnalyzeWithProposals(ctx, item, evidence)
		} else {
			result.Analysis, err = h.Analyzer.Analyze(ctx, item, evidence)
		}
	}
	if v, ok := h.Collector.(RevisionProvider); ok && result.ClusterRevision != v.Revision() {
		err = errors.New("cluster configuration changed during investigation; run it again")
		result.Candidates = nil
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

func (h *Handler) Get(id string) (Result, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.results[id]
	return r, ok
}
