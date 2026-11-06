package main

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/audit"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/inspection"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
	"github.com/stephenzhang0713/kubepilot/internal/remediation"
)

func main() {
	journal, err := audit.Open(cmp.Or(os.Getenv("KUBEPILOT_AUDIT_FILE"), "data/audit.jsonl"))
	if err != nil {
		log.Fatalf("open audit journal: %v", err)
	}
	defer journal.Close()
	collector := kube.NewManager()
	var interval time.Duration
	if value := os.Getenv("KUBEPILOT_INSPECTION_INTERVAL"); value != "" {
		interval, err = time.ParseDuration(value)
		if err != nil || interval < time.Minute {
			log.Fatal("KUBEPILOT_INSPECTION_INTERVAL must be a duration of at least 1m")
		}
	}
	analyzer := agent.Analyzer{BaseURL: cmp.Or(os.Getenv("MODEL_BASE_URL"), "http://localhost:11434/v1"), APIKey: cmp.Or(os.Getenv("MODEL_API_KEY"), "ollama"), Model: cmp.Or(os.Getenv("MODEL_NAME"), "qwen2.5:7b")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	collector.Routes(mux)
	inspector := inspection.New(collector, interval)
	inspector.Routes(mux)
	go inspector.Run(context.Background())
	journal.Routes(mux)
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.MarshalWrite(w, struct {
			Model         string `json:"model"`
			RepairEnabled bool   `json:"repairEnabled"`
		}{analyzer.Model, os.Getenv("KUBEPILOT_ENABLE_REMEDIATION") == "true"})
	})
	store := incidents.New()
	store.Audit = journal
	store.Routes(mux)
	investigations := &investigation.Handler{Incidents: store, Collector: collector, Analyzer: analyzer, Audit: journal}
	investigations.Routes(mux)
	(&remediation.Plans{Investigations: investigations, Runner: collector, Audit: journal, Enabled: os.Getenv("KUBEPILOT_ENABLE_REMEDIATION") == "true"}).Routes(mux)
	server := &http.Server{Addr: cmp.Or(os.Getenv("LISTEN_ADDR"), "127.0.0.1:8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
