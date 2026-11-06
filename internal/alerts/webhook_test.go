package alerts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

func TestWebhookDeduplicatesAndResolves(t *testing.T) {
	store := incidents.New()
	mux := http.NewServeMux()
	New(store, "secret").Routes(mux)
	send := func(status, token string) int {
		body := `{"version":"4","alerts":[{"status":"` + status + `","fingerprint":"abc","startsAt":"2026-01-01T00:00:00Z","labels":{"alertname":"PodDown","namespace":"default","pod":"api","severity":"critical"},"annotations":{"summary":"Pod is unavailable"}}]}`
		r := httptest.NewRequest("POST", "/api/alerts/alertmanager", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if send("firing", "bad") != 401 {
		t.Fatal("missing authentication")
	}
	if send("firing", "secret") != 204 || send("firing", "secret") != 204 {
		t.Fatal("firing rejected")
	}
	items := store.PendingAlerts()
	if len(items) != 1 || items[0].Scope != "pod" || items[0].AlertSeverity != "critical" || items[0].AlertSummary != "Pod is unavailable" {
		t.Fatalf("unexpected incidents: %+v", items)
	}
	if send("resolved", "secret") != 204 || send("firing", "secret") != 204 {
		t.Fatal("notification rejected")
	}
	item, _ := store.Get(items[0].ID)
	if item.AlertStatus != "resolved" {
		t.Fatalf("late firing reopened incident: %+v", item)
	}
}

func TestWorkerProcessesPendingAlert(t *testing.T) {
	store := incidents.New()
	item, _, err := store.UpsertAlert("abc", "PodDown", "default", "api", "firing", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan string, 1)
	receiver := New(store, "secret")
	go receiver.Run(ctx, func(_ context.Context, id string) error { done <- id; return nil })
	select {
	case id := <-done:
		if id != item.ID {
			t.Fatalf("unexpected incident %s", id)
		}
	case <-time.After(time.Second):
		t.Fatal("pending alert was not investigated")
	}
}
