package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

func TestAgentCallsEvidenceTool(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"test","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_kubernetes_evidence","arguments":"{\"reason\":\"inspect\"}"}}]}}]}`))
			return
		}
		found := false
		for _, m := range req.Messages {
			if m.Role == "tool" && strings.Contains(m.Content.(string), "CrashLoopBackOff") {
				found = true
			}
		}
		if !found {
			t.Error("evidence was not sent back to model")
		}
		_, _ = w.Write([]byte(`{"id":"2","object":"chat.completion","model":"test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Likely crash loop"}}]}`))
	}))
	defer server.Close()
	got, err := (Analyzer{BaseURL: server.URL, APIKey: "test", Model: "test"}).Analyze(t.Context(), incidents.Incident{Title: "broken", Pod: "app", Namespace: "default"}, investigation.Evidence{Pod: "CrashLoopBackOff"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Likely crash loop" || calls.Load() != 2 {
		t.Fatalf("got %q after %d requests", got, calls.Load())
	}
}
