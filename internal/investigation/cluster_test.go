package investigation

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

type clusterCollector struct{}

func (clusterCollector) Collect(context.Context, string, string) (Evidence, error) {
	panic("cluster investigation called pod collector")
}
func (clusterCollector) CollectCluster(context.Context) (Evidence, error) {
	return Evidence{Cluster: "node not ready"}, nil
}

type clusterAnalyzer struct{}

func (clusterAnalyzer) Analyze(_ context.Context, i incidents.Incident, e Evidence) (string, error) {
	if i.Scope != "cluster" || !strings.Contains(e.Cluster, "node") {
		panic("scope or evidence missing")
	}
	return "node finding", nil
}
func TestClusterInvestigation(t *testing.T) {
	store := incidents.New()
	mux := http.NewServeMux()
	store.Routes(mux)
	(&Handler{Incidents: store, Collector: clusterCollector{}, Analyzer: clusterAnalyzer{}}).Routes(mux)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/incidents", strings.NewReader(`{"title":"cluster issue","scope":"cluster"}`)))
	if res.Code != http.StatusCreated {
		t.Fatal(res.Body.String())
	}
	var incident incidents.Incident
	if err := json.Unmarshal(res.Body.Bytes(), &incident); err != nil {
		t.Fatal(err)
	}
	res = httptest.NewRecorder()
	mux.ServeHTTP(res, httptest.NewRequest("POST", "/api/incidents/"+incident.ID+"/investigations", nil))
	var result Result
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.Analysis != "node finding" {
		t.Fatalf("unexpected: %+v", result)
	}
}
