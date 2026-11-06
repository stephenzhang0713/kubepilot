package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestIncidentToEvidenceToAgent(t *testing.T) {
	cluster := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/default/pods/app":
			fmt.Fprint(w, `{"metadata":{"name":"app","uid":"uid1"},"spec":{"containers":[{"name":"app","image":"example"}]},"status":{"phase":"Running"}}`)
		case "/api/v1/namespaces/default/events":
			fmt.Fprint(w, `{"items":[{"message":"Back-off"}]}`)
		case "/api/v1/namespaces/default/pods/app/log":
			fmt.Fprint(w, "connection refused")
		default:
			http.NotFound(w, r)
		}
	}))
	defer cluster.Close()
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"id":"1","object":"chat.completion","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_kubernetes_evidence","arguments":"{\"reason\":\"inspect\"}"}}]}}]}`)
		} else {
			encoded, _ := json.Marshal(body)
			if !strings.Contains(string(encoded), "connection refused") {
				t.Error("model did not receive pod logs")
			}
			fmt.Fprint(w, `{"id":"2","object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Inspect the failing connection."}}]}`)
		}
	}))
	defer model.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: cluster.URL})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	store := incidents.New()
	store.Routes(mux)
	(&investigation.Handler{Incidents: store, Collector: &kube.Collector{Client: client}, Analyzer: agent.Analyzer{BaseURL: model.URL, Model: "test", APIKey: "test"}}).Routes(mux)
	create := httptest.NewRecorder()
	mux.ServeHTTP(create, httptest.NewRequest("POST", "/api/incidents", strings.NewReader(`{"title":"Unhealthy","namespace":"default","pod":"app"}`)))
	var item incidents.Incident
	if err := json.Unmarshal(create.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	run := httptest.NewRecorder()
	mux.ServeHTTP(run, httptest.NewRequest("POST", "/api/incidents/"+item.ID+"/investigations", nil))
	var result investigation.Result
	if err := json.Unmarshal(run.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || !strings.Contains(result.Analysis, "failing connection") || !strings.Contains(result.Evidence.Events, "Back-off") {
		t.Fatalf("unexpected response: %+v", result)
	}
}
