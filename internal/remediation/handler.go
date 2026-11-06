// Package remediation manages reviewed, single-use repair plans.
package remediation

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"sync"
	"time"
	"uuid"

	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

type Runner interface {
	PrepareRestart(context.Context, uint64, string, string) (kube.DeploymentSnapshot, error)
	ExecuteRestart(context.Context, kube.DeploymentSnapshot) (int64, error)
	VerifyRestart(context.Context, kube.DeploymentSnapshot) (kube.RolloutEvidence, error)
}
type Plans struct {
	mu             sync.Mutex
	items          map[string]Plan
	Investigations *investigation.Handler
	Runner         Runner
	Enabled        bool
}
type Plan struct {
	ID              string                  `json:"id"`
	InvestigationID string                  `json:"investigationId"`
	Action          string                  `json:"action"`
	Reason          string                  `json:"reason"`
	Target          kube.DeploymentSnapshot `json:"target"`
	Status          string                  `json:"status"`
	Error           string                  `json:"error,omitempty"`
	CreatedAt       time.Time               `json:"createdAt"`
	Verification    *kube.RolloutEvidence   `json:"verification,omitempty"`
	CompletedAt     time.Time               `json:"completedAt,omitzero"`
}

func (p *Plans) Routes(mux *http.ServeMux) {
	p.items = make(map[string]Plan)
	mux.HandleFunc("GET /api/remediations", p.list)
	mux.HandleFunc("POST /api/remediations", p.create)
	mux.HandleFunc("POST /api/remediations/{id}/execute", p.execute)
	mux.HandleFunc("POST /api/remediations/{id}/reject", p.reject)
	mux.HandleFunc("POST /api/remediations/{id}/verify", p.verify)
}
func acceptJSON(r *http.Request) bool {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return e == nil && media == "application/json"
}
func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.MarshalWrite(w, value)
}
func (p *Plans) list(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := make([]Plan, 0, len(p.items))
	for _, item := range p.items {
		items = append(items, item)
	}
	respond(w, struct {
		Enabled bool   `json:"enabled"`
		Plans   []Plan `json:"plans"`
	}{p.Enabled, items})
}
func (p *Plans) create(w http.ResponseWriter, r *http.Request) {
	if !acceptJSON(r) {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		InvestigationID string `json:"investigationId"`
		CandidateIndex  *int   `json:"candidateIndex"`
	}
	if json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 4096), &input, json.RejectUnknownMembers(true)) != nil || input.CandidateIndex == nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	result, ok := p.Investigations.Get(input.InvestigationID)
	if !ok || result.Status != "completed" || *input.CandidateIndex < 0 || *input.CandidateIndex >= len(result.Candidates) {
		http.Error(w, "investigation proposal not found", http.StatusNotFound)
		return
	}
	candidate := result.Candidates[*input.CandidateIndex]
	if candidate.Action != "restart_deployment" || len(candidate.Reason) > 2000 {
		http.Error(w, "unsupported proposal", http.StatusBadRequest)
		return
	}
	var observed struct {
		Sections []struct {
			Name     string `json:"name"`
			Findings []struct {
				Resource string `json:"resource"`
			} `json:"findings"`
		} `json:"sections"`
	}
	found := false
	if json.Unmarshal([]byte(result.Evidence.Cluster), &observed) == nil {
		for _, sec := range observed.Sections {
			if sec.Name == "deployments" {
				for _, f := range sec.Findings {
					if f.Resource == candidate.Namespace+"/"+candidate.Name {
						found = true
					}
				}
			}
		}
	}
	if !found {
		http.Error(w, "proposal target is not an observed Deployment finding", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	target, err := p.Runner.PrepareRestart(ctx, result.ClusterRevision, candidate.Namespace, candidate.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	id := uuid.NewV7()
	plan := Plan{ID: id.String(), InvestigationID: result.ID, Action: candidate.Action, Reason: candidate.Reason, Target: target, Status: "pending", CreatedAt: time.Now().UTC()}
	p.mu.Lock()
	p.items[plan.ID] = plan
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	_ = json.MarshalWrite(w, plan)
}
func (p *Plans) execute(w http.ResponseWriter, r *http.Request) {
	if !acceptJSON(r) || r.Header.Get("X-KubePilot-Confirm") != "restart-deployment" {
		http.Error(w, "explicit confirmation required", http.StatusBadRequest)
		return
	}
	if !p.Enabled {
		http.Error(w, "repair operations are disabled; set KUBEPILOT_ENABLE_REMEDIATION=true and restart the backend", http.StatusForbidden)
		return
	}
	var input struct {
		Confirm string `json:"confirm"`
	}
	if json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 4096), &input, json.RejectUnknownMembers(true)) != nil || input.Confirm != "restart" {
		http.Error(w, "confirmation required", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	plan, ok := p.items[r.PathValue("id")]
	if !ok {
		p.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if plan.Status != "pending" {
		p.mu.Unlock()
		http.Error(w, "plan already used", http.StatusConflict)
		return
	}
	if time.Since(plan.CreatedAt) > 15*time.Minute {
		plan.Status = "expired"
		p.items[plan.ID] = plan
		p.mu.Unlock()
		http.Error(w, "plan expired; create a new plan", http.StatusConflict)
		return
	}
	plan.Target.RestartToken = plan.ID
	plan.Status = "executing"
	p.items[plan.ID] = plan
	p.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	generation, err := p.Runner.ExecuteRestart(ctx, plan.Target)
	plan.Target.RestartGeneration = generation
	plan.CompletedAt = time.Now().UTC()
	if err != nil {
		plan.Status = "unconfirmed"
		plan.Error = fmt.Sprintf("Execution was not confirmed: %v. Check the Deployment before retrying.", err)
	} else {
		plan.Status = "submitted"
	}
	p.mu.Lock()
	p.items[plan.ID] = plan
	p.mu.Unlock()
	if errors.Is(err, context.DeadlineExceeded) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusGatewayTimeout)
	}
	respond(w, plan)
}

func (p *Plans) reject(w http.ResponseWriter, r *http.Request) {
	if !acceptJSON(r) {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	plan, ok := p.items[r.PathValue("id")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	if plan.Status != "pending" {
		http.Error(w, "plan already used", http.StatusConflict)
		return
	}
	plan.Status = "rejected"
	plan.CompletedAt = time.Now().UTC()
	p.items[plan.ID] = plan
	respond(w, plan)
}

// verify reads fresh evidence; it never issues a cluster mutation.
func (p *Plans) verify(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	plan, ok := p.items[r.PathValue("id")]
	p.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if plan.Status != "submitted" {
		http.Error(w, "only submitted plans can be verified", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	evidence, err := p.Runner.VerifyRestart(ctx, plan.Target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	p.mu.Lock()
	current := p.items[plan.ID]
	if current.Verification == nil || current.Verification.ObservedAt.Before(evidence.ObservedAt) {
		current.Verification = &evidence
		p.items[plan.ID] = current
	}
	p.mu.Unlock()
	respond(w, current)
}
