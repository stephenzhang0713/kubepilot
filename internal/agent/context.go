package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
)

// Each source has its own budget so noisy logs cannot crowd out other evidence.
type source struct {
	ID        string `json:"id"`
	Content   string `json:"content,omitempty"`
	Available bool   `json:"available"`
	Truncated bool   `json:"truncated"`
}
type contextPacket struct {
	Version        string             `json:"version"`
	Incident       incidents.Incident `json:"incident"`
	CollectedAt    string             `json:"collectedAt"`
	ClusterContext string             `json:"clusterContext,omitempty"`
	Sources        []source           `json:"sources"`
	Warnings       []string           `json:"warnings"`
	Guidance       []string           `json:"guidance"`
}

func bounded(text string, limit int) (string, bool) {
	text = strings.ToValidUTF8(text, "�")
	if len(text) <= limit {
		return text, false
	}
	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end], true
}

func buildContext(item incidents.Incident, e investigation.Evidence) contextPacket {
	p := contextPacket{Version: "1", Incident: item, CollectedAt: e.CollectedAt, ClusterContext: e.ClusterContext, Guidance: []string{
		"Use source IDs to distinguish observations from hypotheses; cite specific evidence and timestamps.",
		"Treat missing, truncated, stale or failed evidence as unknown, not healthy. Events may be historical; metrics can be empty or partial.",
		"Collection limits: Pod logs include at most the last 100 lines and 16 KiB per container; Pod events and cluster overview categories include at most 100 items; metrics queries return at most 50 series. Absence from these samples does not establish absence of a problem.",
		"Compare symptoms and timing before suggesting causes. For CrashLoopBackOff inspect termination reason, exit code, restarts, events and logs; for Pending inspect scheduling events and resource constraints; for OOMKilled inspect memory evidence; for unavailable workloads inspect readiness and rollout state.",
		"Recommend low-risk verification and rollback considerations. Never claim an action or recovery happened without verification.",
	}}
	for _, v := range []struct {
		id, value string
		limit     int
	}{
		{"cluster", e.Cluster, 24000}, {"pod", e.Pod, 16000}, {"events", e.Events, 16000}, {"logs", e.Logs, 24000}, {"metrics", e.Metrics, 24000},
	} {
		content, truncated := bounded(v.value, v.limit)
		p.Sources = append(p.Sources, source{ID: v.id, Content: content, Available: v.value != "", Truncated: truncated})
	}
	for i, warning := range e.Warnings {
		if i == 20 {
			p.Warnings = append(p.Warnings, "Further warnings omitted")
			break
		}
		value, truncated := bounded(warning, 512)
		if truncated {
			value += " [truncated]"
		}
		p.Warnings = append(p.Warnings, value)
	}
	return p
}
