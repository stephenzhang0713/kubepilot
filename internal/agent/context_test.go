package agent

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

func TestContextBudgetKeepsEvidenceGapsAndSources(t *testing.T) {
	p := buildContext(incidents.Incident{Title: "alert", AlertSeverity: "critical", AlertSummary: "latency high"}, investigation.Evidence{CollectedAt: "2026-11-07T00:00:00Z", Pod: "CrashLoopBackOff", Logs: strings.Repeat("故障", 10000), Warnings: []string{"events forbidden"}})
	if p.Version != "1" || p.Incident.AlertSummary != "latency high" || p.CollectedAt == "" || len(p.Sources) != 5 || len(p.Warnings) != 1 {
		t.Fatalf("missing context: %+v", p)
	}
	if p.Sources[0].Available || !p.Sources[1].Available || !p.Sources[3].Truncated || len(p.Sources[3].Content) > 24000 || !utf8.ValidString(p.Sources[3].Content) || p.Sources[1].Content != "CrashLoopBackOff" {
		t.Fatalf("incorrect evidence selection: %+v", p.Sources)
	}
}
