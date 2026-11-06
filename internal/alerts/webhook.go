// Package alerts accepts Alertmanager notifications.
package alerts

import (
	"context"
	"crypto/subtle"
	json "encoding/json/v2"
	"net/http"
	"strings"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
)

type Receiver struct {
	Store *incidents.Store
	Token string
	wake  chan struct{}
}

func New(store *incidents.Store, token string) *Receiver {
	return &Receiver{Store: store, Token: token, wake: make(chan struct{}, 1)}
}

type alert struct {
	Status      string            `json:"status"`
	Fingerprint string            `json:"fingerprint"`
	StartsAt    time.Time         `json:"startsAt"`
	Labels      map[string]string `json:"labels"`
}

func (a *Receiver) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/alerts/alertmanager", a.receive)
}
func (a *Receiver) receive(w http.ResponseWriter, r *http.Request) {
	if a.Token == "" {
		http.Error(w, "alert receiver disabled", http.StatusServiceUnavailable)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+a.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var payload struct {
		Version string  `json:"version"`
		Alerts  []alert `json:"alerts"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 1<<20), &payload); err != nil || payload.Version != "4" || len(payload.Alerts) == 0 || len(payload.Alerts) > 100 {
		http.Error(w, "invalid Alertmanager payload", http.StatusBadRequest)
		return
	}
	for _, item := range payload.Alerts {
		if (item.Status != "firing" && item.Status != "resolved") || item.Fingerprint == "" || len(item.Fingerprint) > 256 || item.StartsAt.IsZero() || len(item.Labels["alertname"]) > 256 || len(item.Labels["namespace"]) > 253 || len(item.Labels["pod"]) > 253 {
			http.Error(w, "invalid alert", http.StatusBadRequest)
			return
		}
	}
	for _, item := range payload.Alerts {
		title := strings.TrimSpace(item.Labels["alertname"])
		if title == "" {
			title = "Alertmanager alert"
		}
		if _, _, err := a.Store.UpsertAlert(item.Fingerprint, title, item.Labels["namespace"], item.Labels["pod"], item.Status, item.StartsAt); err != nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

// Run processes persisted pending incidents serially; unfinished work is retried after restart.
func (a *Receiver) Run(ctx context.Context, investigate func(context.Context, string) error) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		for _, item := range a.Store.PendingAlerts() {
			if ctx.Err() != nil {
				return
			}
			if item.AlertStatus != "resolved" {
				if err := investigate(ctx, item.ID); err != nil {
					break
				}
			}
			if err := a.Store.FinishAlert(item.ID); err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-ticker.C:
		}
	}
}
