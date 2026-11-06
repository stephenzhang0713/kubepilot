// Package inspection runs optional, read-only periodic cluster inspections.
package inspection

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

type Collector interface {
	Overview(context.Context) (kube.Overview, error)
	Revision() uint64
}

type Status struct {
	Enabled         bool           `json:"enabled"`
	IntervalSeconds int64          `json:"intervalSeconds"`
	Running         bool           `json:"running"`
	LastAttempt     time.Time      `json:"lastAttempt,omitzero"`
	Error           string         `json:"error,omitempty"`
	Overview        *kube.Overview `json:"overview,omitempty"`
}

type Inspector struct {
	collector Collector
	interval  time.Duration
	mu        sync.RWMutex
	status    Status
	revision  uint64
}

func New(collector Collector, interval time.Duration) *Inspector {
	return &Inspector{collector: collector, interval: interval, status: Status{Enabled: interval > 0, IntervalSeconds: int64(interval.Seconds())}}
}

func (i *Inspector) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inspection", i.get)
}

func (i *Inspector) get(w http.ResponseWriter, _ *http.Request) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	status := i.status
	if status.Overview != nil && i.revision != i.collector.Revision() {
		status.Overview = nil
		status.Error = "cluster configuration changed; awaiting next inspection"
	}
	_ = json.MarshalWrite(w, status)
}

// Run stops when ctx is canceled; inspections never overlap.
func (i *Inspector) Run(ctx context.Context) {
	if i.interval <= 0 {
		return
	}
	i.inspect(ctx)
	ticker := time.NewTicker(i.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.inspect(ctx)
		}
	}
}

func (i *Inspector) inspect(ctx context.Context) {
	i.mu.Lock()
	i.status.Running = true
	i.status.LastAttempt = time.Now().UTC()
	i.mu.Unlock()
	revision := i.collector.Revision()
	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	result, err := i.collector.Overview(checkCtx)
	if err == nil {
		err = checkCtx.Err()
	}
	cancel()
	if err == nil && revision != i.collector.Revision() {
		err = errors.New("cluster configuration changed during inspection")
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.status.Running = false
	i.status.Error = ""
	// Never present an old or mixed-context snapshot as the latest result.
	i.status.Overview = nil
	if err != nil {
		i.status.Error = err.Error()
		return
	}
	i.status.Overview = &result
	i.revision = revision
}
