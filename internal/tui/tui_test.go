package tui

import (
	"context"
	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/config"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
	"github.com/stephenzhang0713/kubepilot/internal/remediation"
)

func key(m Model, s string) Model {
	updated, _ := m.Update(tea.KeyPressMsg{Code: []rune(s)[0], Text: s})
	return updated.(Model)
}
func TestRepairRequiresReviewAndConfirmation(t *testing.T) {
	m := New(http.NewServeMux(), "test", true)
	m.page = repairsPage
	m.plans = []remediation.Plan{{Status: "pending", Target: kube.DeploymentSnapshot{Context: "prod", Namespace: "app", Name: "api"}}}
	m = key(m, "a")
	if m.mode != "" {
		t.Fatal("approval should require opening the plan")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	m = key(m, "a")
	if m.mode != "confirm" || !strings.Contains(m.View().Content, "app/api") {
		t.Fatal("confirmation must show the selected target")
	}
	m = key(m, "n")
	if m.mode != "plan" {
		t.Fatal("cancelling should return to review")
	}
}
func TestContextSelectionAndFormNavigation(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.mode = "contexts"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.selected != 0 {
		t.Fatal("empty context list must not select a negative index")
	}
	m.mode = "form"
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(Model)
	if m.field != 0 {
		t.Fatal("cluster form should skip Pod fields")
	}
}
func TestViewRemovesTerminalControlSequences(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = investigationsPage
	m.status = "\x1b]0;injected\x07"
	if strings.Contains(m.View().Content, "\x1b]0;injected") {
		t.Fatal("status should not contain terminal control characters")
	}
}

func TestSettingsEditPersistsAndMasksSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := config.Defaults()
	settings.ModelKey = "old-secret"
	m := New(http.NewServeMux(), "test", false, WithSettings(settings, path))
	m.page = settingsPage
	m.selected = 2
	if strings.Contains(m.View().Content, "old-secret") {
		t.Fatal("settings exposed secret")
	}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if strings.Contains(m.View().Content, "old-secret") {
		t.Fatal("editor exposed old secret")
	}
	m = key(m, "new-secret")
	if strings.Contains(m.View().Content, "new-secret") {
		t.Fatal("editor exposed new secret")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	saved, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ModelKey != "new-secret" || m.activeSettings.ModelKey != "old-secret" {
		t.Fatal("saved and active settings should remain separate until restart")
	}
}
func TestSettingsRejectInvalidValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := config.Defaults()
	m := New(http.NewServeMux(), "test", false, WithSettings(settings, path))
	m.page = settingsPage
	m.selected = 5
	m.mode = "setting-edit"
	m.editValue = "5s"
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if !m.statusError || m.mode != "setting-edit" || m.settings != settings {
		t.Fatal("invalid value should keep editor open without changing settings")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid setting must not be saved")
	}
}

func TestChatUsesSelectedInvestigation(t *testing.T) {
	var got investigation.Result
	var turns []agent.Turn
	chat := func(_ context.Context, result investigation.Result, history []agent.Turn) (string, error) {
		got = result
		turns = history
		return "Check the Pod events.", nil
	}
	m := New(http.NewServeMux(), "test", false, WithChat(chat))
	m.page = chatPage
	m.result = &investigation.Result{ID: "investigation-1", Evidence: investigation.Evidence{Logs: "backoff"}}
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(tea.PasteMsg{Content: "What happened?"})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("question should start an asynchronous request")
	}
	reply := cmd()
	next, _ = m.Update(reply)
	m = next.(Model)
	if got.ID != "investigation-1" || got.Evidence.Logs != "backoff" || len(turns) != 1 || turns[0].Text != "What happened?" {
		t.Fatal("question or evidence not passed to assistant")
	}
	if len(m.turns) != 2 || m.turns[1].Text != "Check the Pod events." || m.busy {
		t.Fatal("assistant response not shown")
	}
}

func TestViewFitsTerminalSizes(t *testing.T) {
	for _, size := range []struct{ width, height int }{{30, 10}, {50, 16}, {80, 24}, {120, 32}} {
		m := New(http.NewServeMux(), strings.Repeat("model", 20), false)
		m.width = size.width
		m.height = size.height
		for _, p := range []page{overviewPage, investigationsPage, chatPage, repairsPage, settingsPage} {
			m.page = p
			lines := strings.Split(m.View().Content, "\n")
			if len(lines) > size.height {
				t.Errorf("%dx%d page %d has %d rows", size.width, size.height, p, len(lines))
			}
			for _, line := range lines {
				if lipgloss.Width(line) > size.width {
					t.Errorf("%dx%d page %d line width %d", size.width, size.height, p, lipgloss.Width(line))
					break
				}
			}
		}
	}
}
