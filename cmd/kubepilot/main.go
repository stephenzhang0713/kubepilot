package main

import (
	"cmp"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

func main() {
	collector := kube.NewManager()
	analyzer := agent.Analyzer{BaseURL: cmp.Or(os.Getenv("MODEL_BASE_URL"), "http://localhost:11434/v1"), APIKey: cmp.Or(os.Getenv("MODEL_API_KEY"), "ollama"), Model: cmp.Or(os.Getenv("MODEL_NAME"), "qwen2.5:7b")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	collector.Routes(mux)
	store := incidents.New()
	store.Routes(mux)
	(&investigation.Handler{Incidents: store, Collector: collector, Analyzer: analyzer}).Routes(mux)
	server := &http.Server{Addr: cmp.Or(os.Getenv("LISTEN_ADDR"), "127.0.0.1:8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
