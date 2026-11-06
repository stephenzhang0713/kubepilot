package investigation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

type collector struct{ err error }

func (c collector) Collect(_ context.Context, ns, pod string) (Evidence, error) {
	if ns != "default" || pod != "app" {
		panic("incorrect target")
	}
	return Evidence{Logs: "failure"}, c.err
}

type analyzer struct{}

func (analyzer) Analyze(_ context.Context, _ incidents.Incident, e Evidence) (string, error) {
	if e.Logs != "failure" {
		panic("missing evidence")
	}
	return "diagnosis", nil
}
func TestInvestigationLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status string
	}{{"success", nil, "completed"}, {"collector failure", errors.New("cluster unavailable"), "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			store := incidents.New()
			mux := http.NewServeMux()
			store.Routes(mux)
			(&Handler{Incidents: store, Collector: collector{tc.err}, Analyzer: analyzer{}}).Routes(mux)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/incidents", strings.NewReader(`{"title":"broken","namespace":"default","pod":"app"}`)))
			var incident incidents.Incident
			if err := json.Unmarshal(w.Body.Bytes(), &incident); err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/incidents/"+incident.ID+"/investigations", nil))
			if w.Code != http.StatusCreated {
				t.Fatal(w.Code, w.Body.String())
			}
			var result Result
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Status != tc.status || result.ID == "" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if tc.err != nil && result.Error == "" {
				t.Fatal("missing failure")
			}
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/investigations/"+result.ID, nil))
			if w.Code != http.StatusOK {
				t.Fatal(w.Code)
			}
		})
	}
}
