package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

func TestRestoreAlertAndInvestigation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store := incidents.New()
	if err := store.Restore(db); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	item, created, err := store.UpsertAlert("abc", "PodDown", "default", "api", "firing", start)
	if err != nil || !created {
		t.Fatalf("create: %v", err)
	}
	if err := db.Put("investigation", "test", []byte(`{"id":"test","incidentId":"`+item.ID+`","status":"completed","candidates":[{"action":"restart_deployment"}]}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store = incidents.New()
	if err := store.Restore(db); err != nil {
		t.Fatal(err)
	}
	if len(store.PendingAlerts()) != 1 {
		t.Fatal("pending alert was not restored")
	}
	_, created, err = store.UpsertAlert("abc", "PodDown", "default", "api", "firing", start)
	if err != nil || created {
		t.Fatalf("duplicate alert after restart: %v", err)
	}
	h := &investigation.Handler{}
	if err := h.Restore(db); err != nil {
		t.Fatal(err)
	}
	result, ok := h.Get("test")
	if !ok || result.IncidentID != item.ID || len(result.Candidates) != 0 {
		t.Fatalf("unexpected restored result: %+v", result)
	}
}
