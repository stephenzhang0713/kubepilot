package prometheus

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

func TestCollectPodEvidence(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/prom/api/v1/query_range" || r.Header.Get("Authorization") != "Bearer token" || !strings.Contains(r.URL.Query().Get("query"), `pod="api"`) || r.URL.Query().Get("limit") != "50" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer server.Close()
	client, err := New(server.URL+"/prom", "token")
	if err != nil {
		t.Fatal(err)
	}
	data, err := client.Collect(t.Context(), incidents.Incident{Scope: "pod", Namespace: "default", Pod: "api"})
	if err != nil || calls != 2 || !strings.Contains(data, "resultType") {
		t.Fatalf("result %q, calls %d, error %v", data, calls, err)
	}
}
