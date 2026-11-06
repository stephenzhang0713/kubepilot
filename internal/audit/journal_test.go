package audit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalSurvivesReopenAndLists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Event{Kind: "plan.created", PlanID: "plan"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	mux := http.NewServeMux()
	j.Routes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/audit", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"planId":"plan"`) {
		t.Fatalf("missing persisted event: %s", w.Body.String())
	}
}
func TestJournalRejectsPartialAndStopsAfterWriteFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.WriteFile(path, []byte(`{"kind":"incomplete"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if j, err := Open(path); err == nil {
		j.Close()
		t.Fatal("accepted incomplete record")
	}
	path = filepath.Join(t.TempDir(), "audit.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.Append(Event{Kind: "first"}); err == nil {
		t.Fatal("accepted failed write")
	}
	if err := j.Append(Event{Kind: "second"}); err == nil {
		t.Fatal("journal did not remain failed")
	}
}
