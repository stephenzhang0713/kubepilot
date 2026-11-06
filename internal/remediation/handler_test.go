package remediation

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

type stubCollector struct{}

func (stubCollector) Collect(context.Context, string, string) (investigation.Evidence, error) {
	return investigation.Evidence{}, nil
}
func (stubCollector) CollectCluster(context.Context) (investigation.Evidence, error) {
	return investigation.Evidence{Cluster: `{"sections":[{"name":"deployments","findings":[{"resource":"default/api"}]}]}`}, nil
}
func (stubCollector) Revision() uint64 { return 1 }

type stubAnalyzer struct{}

func (stubAnalyzer) Analyze(context.Context, incidents.Incident, investigation.Evidence) (string, error) {
	return "", nil
}
func (stubAnalyzer) AnalyzeWithProposals(context.Context, incidents.Incident, investigation.Evidence) (string, []investigation.Candidate, error) {
	return "diagnosis", []investigation.Candidate{{Action: "restart_deployment", Namespace: "default", Name: "api", Reason: "unavailable"}}, nil
}

type stubRunner struct{ calls atomic.Int32 }

func (*stubRunner) PrepareRestart(_ context.Context, rev uint64, ns, name string) (kube.DeploymentSnapshot, error) {
	return kube.DeploymentSnapshot{Namespace: ns, Name: name, Revision: rev, UID: "uid", ResourceVersion: "1"}, nil
}
func (s *stubRunner) ExecuteRestart(context.Context, kube.DeploymentSnapshot) (int64, error) {
	s.calls.Add(1)
	return 1, nil
}
func (*stubRunner) VerifyRestart(context.Context, kube.DeploymentSnapshot) (kube.RolloutEvidence, error) {
	return kube.RolloutEvidence{ObservedAt: time.Now(), Verified: true}, nil
}
func TestApprovalRequiredAndSingleUse(t *testing.T) {
	mux := http.NewServeMux()
	store := incidents.New()
	store.Routes(mux)
	investigations := &investigation.Handler{Incidents: store, Collector: stubCollector{}, Analyzer: stubAnalyzer{}}
	investigations.Routes(mux)
	runner := &stubRunner{}
	plans := &Plans{Investigations: investigations, Runner: runner, Enabled: true}
	plans.Routes(mux)
	request := func(method, path, body string, confirmed bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if confirmed {
			r.Header.Set("X-KubePilot-Confirm", "restart-deployment")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/incidents", `{"title":"test","scope":"cluster"}`, false)
	var incident incidents.Incident
	if err := json.Unmarshal(w.Body.Bytes(), &incident); err != nil {
		t.Fatal(err)
	}
	w = request("POST", "/api/incidents/"+incident.ID+"/investigations", "{}", false)
	var result investigation.Result
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"investigationId": result.ID, "candidateIndex": 0})
	w = request("POST", "/api/remediations", string(b), false)
	if w.Code != 201 {
		t.Fatalf("prepare: %d %s", w.Code, w.Body.String())
	}
	var plan Plan
	_ = json.Unmarshal(w.Body.Bytes(), &plan)
	if runner.calls.Load() != 0 {
		t.Fatal("operation executed without approval")
	}
	w = request("POST", "/api/remediations/"+plan.ID+"/execute", `{"confirm":"restart"}`, false)
	if w.Code != 400 {
		t.Fatalf("missing confirmation: %d", w.Code)
	}
	w = request("POST", "/api/remediations/"+plan.ID+"/execute", `{"confirm":"restart"}`, true)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"submitted"`)) {
		t.Fatalf("execute: %d %s", w.Code, w.Body.String())
	}
	w = request("POST", "/api/remediations/"+plan.ID+"/verify", `{}`, false)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"verified":true`)) {
		t.Fatalf("verify: %d %s", w.Code, w.Body.String())
	}
	w = request("POST", "/api/remediations/"+plan.ID+"/execute", `{"confirm":"restart"}`, true)
	if w.Code != 409 || runner.calls.Load() != 1 {
		t.Fatal("plan was executed twice")
	}
}

func TestDisabledExecutionAndExpiry(t *testing.T) {
	runner := &stubRunner{}
	p := &Plans{Runner: runner, Enabled: false, items: map[string]Plan{"plan": {ID: "plan", Status: "pending", CreatedAt: time.Now()}}}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/remediations/plan/execute", strings.NewReader(`{"confirm":"restart"}`))
		r.SetPathValue("id", "plan")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-KubePilot-Confirm", "restart-deployment")
		w := httptest.NewRecorder()
		p.execute(w, r)
		return w
	}
	if w := request(); w.Code != http.StatusForbidden || runner.calls.Load() != 0 {
		t.Fatal("disabled operation executed")
	}
	p.Enabled = true
	p.items["plan"] = Plan{ID: "plan", Status: "pending", CreatedAt: time.Now().Add(-16 * time.Minute)}
	if w := request(); w.Code != http.StatusConflict || runner.calls.Load() != 0 {
		t.Fatal("expired operation executed")
	}
}

func TestRejectPreventsExecution(t *testing.T) {
	runner := &stubRunner{}
	p := &Plans{Runner: runner, Enabled: true, items: map[string]Plan{"plan": {ID: "plan", Status: "pending", CreatedAt: time.Now()}}}
	r := httptest.NewRequest("POST", "/api/remediations/plan/reject", strings.NewReader(`{}`))
	r.SetPathValue("id", "plan")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.reject(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/api/remediations/plan/execute", strings.NewReader(`{"confirm":"restart"}`))
	r.SetPathValue("id", "plan")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-KubePilot-Confirm", "restart-deployment")
	w = httptest.NewRecorder()
	p.execute(w, r)
	if w.Code != http.StatusConflict || runner.calls.Load() != 0 {
		t.Fatal("rejected plan executed")
	}
}
