package inspection

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

type fakeCollector struct {
	revision uint64
	err      error
	change   bool
}

func (f *fakeCollector) Revision() uint64 { return f.revision }
func (f *fakeCollector) Overview(context.Context) (kube.Overview, error) {
	if f.change {
		f.revision++
	}
	return kube.Overview{Sections: []kube.Section{{Name: "nodes", State: "attention"}}}, f.err
}

func TestInspectionReportsFailuresAndContextChanges(t *testing.T) {
	f := &fakeCollector{}
	i := New(f, time.Minute)
	mux := http.NewServeMux()
	i.Routes(mux)
	status := func() Status {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/inspection", nil))
		var result Status
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	i.inspect(t.Context())
	if got := status(); got.Overview == nil || got.Overview.Sections[0].State != "attention" || got.LastAttempt.IsZero() {
		t.Fatalf("missing inspection result: %+v", got)
	}
	f.revision++
	if got := status(); got.Overview != nil || got.Error == "" {
		t.Fatalf("stale result exposed after context change: %+v", got)
	}
	f.err = errors.New("connection failed")
	i.inspect(t.Context())
	if got := status(); got.Overview != nil || got.Error != "connection failed" {
		t.Fatalf("failure not reported: %+v", got)
	}
	f.err = nil
	f.change = true
	i.inspect(t.Context())
	if got := status(); got.Overview != nil || got.Error == "" {
		t.Fatalf("mixed context result exposed: %+v", got)
	}
}
