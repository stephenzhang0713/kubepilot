package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/config"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/inspection"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
	"github.com/stephenzhang0713/kubepilot/internal/remediation"
	"github.com/stephenzhang0713/kubepilot/internal/tui"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	path, err := config.Path()
	if err != nil {
		return fmt.Errorf("locate settings: %w", err)
	}
	settings, err := config.Load(path)
	if err != nil {
		return err
	}
	if settings.Kubeconfig == "" {
		err = os.Unsetenv("KUBECONFIG")
	} else {
		err = os.Setenv("KUBECONFIG", settings.Kubeconfig)
	}
	if err != nil {
		return err
	}
	collector := kube.NewManagerForContext(settings.Context)
	var interval time.Duration
	if settings.InspectionInterval != "" {
		interval, err = time.ParseDuration(settings.InspectionInterval)
		if err != nil || interval < time.Minute {
			return fmt.Errorf("inspection interval must be at least 1m")
		}
	}
	analyzer := agent.Analyzer{BaseURL: settings.ModelURL, APIKey: settings.ModelKey, Model: settings.ModelName}
	store := incidents.New()
	investigations := &investigation.Handler{Incidents: store, Collector: collector, Analyzer: analyzer}
	mux := http.NewServeMux()
	collector.Routes(mux)
	inspector := inspection.New(collector, interval)
	inspector.Routes(mux)
	store.Routes(mux)
	investigations.Routes(mux)
	(&remediation.Plans{Investigations: investigations, Runner: collector, Enabled: settings.Remediation}).Routes(mux)
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Go(func() { inspector.Run(ctx) })
	defer func() { cancel(); workers.Wait() }()
	_, err = tea.NewProgram(tui.New(mux, settings.ModelName, settings.Remediation, tui.WithSettings(settings, path), tui.WithChat(analyzer.Chat))).Run()
	return err
}
