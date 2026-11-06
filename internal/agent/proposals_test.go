package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

func TestAgentCreatesProposalWithoutExecuting(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			fmt.Fprint(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"get_kubernetes_evidence","arguments":"{\"reason\":\"inspect\"}"}}]}}]}`)
		case 2:
			fmt.Fprint(w, `{"id":"2","object":"chat.completion","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"b","type":"function","function":{"name":"propose_deployment_restart","arguments":"{\"namespace\":\"default\",\"name\":\"api\",\"reason\":\"Deployment unavailable\"}"}}]}}]}`)
		default:
			fmt.Fprint(w, `{"id":"3","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Review the proposed restart."}}]}`)
		}
	}))
	defer model.Close()
	text, proposals, trace, err := (Analyzer{BaseURL: model.URL, Model: "test", APIKey: "test"}).AnalyzeWithTrace(t.Context(), incidents.Incident{Scope: "cluster", Title: "degraded"}, investigation.Evidence{Cluster: "unavailable default/api"})
	if err != nil {
		t.Fatal(err)
	}
	if text == "" || len(proposals) != 1 || proposals[0].Name != "api" || proposals[0].Action != "restart_deployment" {
		t.Fatalf("unexpected: %q %+v", text, proposals)
	}
	if len(trace) != 2 || trace[0].Tool != "get_kubernetes_evidence" || trace[0].Outcome != "completed" || trace[1].Outcome != "proposed" {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestProposalRequiresEvidence(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"propose_deployment_restart","arguments":"{\"namespace\":\"default\",\"name\":\"api\",\"reason\":\"restart\"}"}}]}}]}`)
			return
		}
		fmt.Fprint(w, `{"id":"2","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"done"}}]}`)
	}))
	defer model.Close()
	_, proposals, trace, err := (Analyzer{BaseURL: model.URL, Model: "test", APIKey: "test"}).AnalyzeWithTrace(t.Context(), incidents.Incident{Title: "test"}, investigation.Evidence{})
	if err == nil || len(proposals) != 0 || len(trace) != 1 || trace[0].Outcome != "rejected" {
		t.Fatalf("err=%v proposals=%+v trace=%+v", err, proposals, trace)
	}
}
