package tui

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/config"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"

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
	commands, ok := cmd().(tea.BatchMsg)
	if !ok || len(commands) == 0 {
		t.Fatal("expected chat and spinner commands")
	}
	reply := commands[0]()
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

func TestDashboardShowsFindingsAndFits(t *testing.T) {
	for _, width := range []int{90, 120, 160, 220} {
		m := New(http.NewServeMux(), "model", false)
		m.width = width
		m.height = 40
		m.overview = &kube.Overview{Sections: []kube.Section{
			{Name: "nodes", State: "ok", Checked: 1},
			{Name: "deployments", State: "attention", Checked: 2, Findings: []kube.Finding{{Resource: "default/api", Detail: "0/2 replicas available"}}},
			{Name: "events", State: "unknown", Error: "permission denied"},
		}}
		output := m.View().Content
		for _, text := range []string{"RESOURCE CHECKS", "FINDINGS & GAPS", "default/api", "permission denied"} {
			if !strings.Contains(output, text) {
				t.Errorf("width %d: missing %q", width, text)
			}
		}
		for line := range strings.SplitSeq(output, "\n") {
			if lipgloss.Width(line) > width {
				t.Errorf("width %d: line overflows: %d", width, lipgloss.Width(line))
				break
			}
		}
	}
}
func TestDashboardScrollsLongFindings(t *testing.T) {
	m := New(http.NewServeMux(), "model", false)
	m.width = 100
	m.height = 24
	section := kube.Section{Name: "pods", State: "attention"}
	for i := range 20 {
		section.Findings = append(section.Findings, kube.Finding{Resource: fmt.Sprintf("ns/pod-%02d", i), Detail: "not ready"})
	}
	m.overview = &kube.Overview{Sections: []kube.Section{section}}
	if m.maxOffset() == 0 {
		t.Fatal("long findings must be scrollable")
	}
	m.offset = m.maxOffset()
	if !strings.Contains(m.View().Content, "ns/pod-19") {
		t.Fatal("last finding is not reachable")
	}
}

func TestDashboardCountsTruncatedAttentionAsGap(t *testing.T) {
	checked, attention, unknown := countStates([]kube.Section{{State: "attention", Checked: 3, Truncated: true}, {State: "unknown", Error: "forbidden"}, {State: "ok", Checked: 1}})
	if checked != 4 || attention != 1 || unknown != 2 {
		t.Fatalf("unexpected counts: %d %d %d", checked, attention, unknown)
	}
}

func TestPanelHeightAndAlignment(t *testing.T) {
	left := dashPanel(45, 14, "heading\ncontent\nmore")
	right := dashPanel(38, 14, "heading\nshort")
	if lipgloss.Height(left) != 14 || lipgloss.Height(right) != 14 {
		t.Fatalf("panel heights differ: %d, %d", lipgloss.Height(left), lipgloss.Height(right))
	}
	for line := range strings.SplitSeq(left, "\n") {
		if lipgloss.Width(line) != 45 {
			t.Fatalf("wrong panel width %d", lipgloss.Width(line))
		}
	}
}

func TestWorkspaceSelectionStaysVisible(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = investigationsPage
	m.width = 100
	m.height = 22
	for i := range 35 {
		m.incidents = append(m.incidents, incidents.Incident{Title: fmt.Sprintf("incident-%02d", i), Scope: "cluster"})
	}
	m.selected = 34
	if !strings.Contains(ansi.Strip(m.View().Content), "incident-34") {
		t.Fatal("last selected investigation is not visible")
	}
}
func TestWorkspaceLayoutsAndChatInput(t *testing.T) {
	for _, size := range []struct{ width, height int }{{90, 22}, {120, 32}, {160, 45}} {
		m := New(http.NewServeMux(), "test", false)
		m.width = size.width
		m.height = size.height
		m.result = &investigation.Result{ID: "selected", Analysis: "test"}
		m.mode = "chat-input"
		m.page = chatPage
		m.chatInput = "why is this pod pending?"
		m.turns = []agent.Turn{{Role: "assistant", Text: strings.Repeat("evidence and context ", 100)}}
		view := m.View().Content
		if !strings.Contains(ansi.Strip(view), "why is this pod pending?") {
			t.Errorf("%dx%d chat input is not visible", size.width, size.height)
		}
		for line := range strings.SplitSeq(view, "\n") {
			if lipgloss.Width(line) > size.width {
				t.Errorf("%dx%d chat line overflows: %d", size.width, size.height, lipgloss.Width(line))
				break
			}
		}
	}
}

func TestStartInvestigationFromRepairClearsListSelection(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = repairsPage
	m.selected = 8
	m.offset = 6
	m = key(m, "n")
	if m.page != investigationsPage || m.mode != "form" || m.selected != 0 || m.offset != 0 {
		t.Fatal("new investigation should reset the selection and scroll position")
	}
}

func TestColorsDowngradeWithoutChangingContent(t *testing.T) {
	m := New(http.NewServeMux(), "model", false)
	m.width = 120
	m.height = 32
	m.overview = &kube.Overview{Sections: []kube.Section{{Name: "nodes", State: "ok", Checked: 1}, {Name: "pods", State: "attention", Findings: []kube.Finding{{Resource: "default/api", Detail: "not ready"}}}}}
	original := m.View().Content
	if !strings.Contains(original, "38;2;") {
		t.Fatal("expected RGB foreground colors in the rendered view")
	}
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.NoTTY} {
		var output bytes.Buffer
		writer := colorprofile.Writer{Forward: &output, Profile: profile}
		if _, err := writer.Write([]byte(original)); err != nil {
			t.Fatal(err)
		}
		if ansi.Strip(output.String()) != ansi.Strip(original) {
			t.Fatalf("profile %s changed view content", profile)
		}
		if profile != colorprofile.TrueColor && strings.Contains(output.String(), "38;2;") {
			t.Fatalf("profile %s did not downgrade RGB colors", profile)
		}
	}
}

func TestFitCellUsesTerminalWidth(t *testing.T) {
	for _, text := range []string{"故障排查", "👩‍💻 team", "e\u0301xample", "a-very-long-namespace-name"} {
		value := fitCell(text, 12)
		if got := ansi.StringWidth(value); got != 12 {
			t.Errorf("%q: width %d, want 12", text, got)
		}
	}
}
func TestCompactSettingsCanScrollToDetails(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = settingsPage
	m.width = 70
	m.height = 20
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "SETTING DETAILS") {
		t.Fatal("settings details should be reachable with Page Down")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	if m.manualScroll || !strings.Contains(ansi.Strip(m.View().Content), "Model name") {
		t.Fatal("selection should regain focus after navigation")
	}
}
func TestCompactChatShowsInputOnOpen(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = chatPage
	m.width = 70
	m.height = 20
	m.result = &investigation.Result{ID: "i", Analysis: "test"}
	next, _ := m.Update(response{action: "chat-page"})
	m = next.(Model)
	if !strings.Contains(ansi.Strip(m.View().Content), "Press Enter to ask a question") {
		t.Fatal("chat input should be visible when opening compact chat")
	}
}
func TestCompactPagesFitTerminal(t *testing.T) {
	for _, width := range []int{62, 70, 89, 90, 150, 220} {
		m := New(http.NewServeMux(), "test", false)
		m.width = width
		m.height = 24
		for _, p := range []page{overviewPage, investigationsPage, chatPage, repairsPage, settingsPage} {
			m.page = p
			for line := range strings.SplitSeq(m.View().Content, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("page %d width %d overflow: %d", p, width, ansi.StringWidth(line))
				}
			}
		}
	}
}

func TestDeleteLastGrapheme(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"", ""}, {"e\u0301", ""}, {"ab👩‍💻", "ab"}, {"中👍🏽", "中"}, {"test🇨🇳", "test"}} {
		if got := deleteLastGrapheme(tc.input); got != tc.want {
			t.Errorf("delete from %q: got %q, want %q", tc.input, got, tc.want)
		}
	}
}
func TestSpinnerStopsWhenIdleAndIgnoresOldTicks(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	next, cmd := m.Update(tick{generation: 0})
	m = next.(Model)
	if cmd != nil || m.frame != 0 {
		t.Fatal("idle spinner should not schedule another frame")
	}
	_ = m.begin("first")
	old := m.busyGeneration
	_ = m.begin("second")
	next, cmd = m.Update(tick{generation: old})
	m = next.(Model)
	if cmd != nil || m.frame != 0 {
		t.Fatal("stale tick should not start a second spinner")
	}
	next, cmd = m.Update(tick{generation: m.busyGeneration})
	m = next.(Model)
	if cmd == nil || m.frame != 1 {
		t.Fatal("active spinner should schedule its next frame")
	}
}
func TestConfirmationShowsTargetBeforeLongReason(t *testing.T) {
	m := New(http.NewServeMux(), "test", true)
	m.page = repairsPage
	m.mode = "confirm"
	m.width = 100
	m.height = 22
	m.plans = []remediation.Plan{{Status: "pending", Action: "restart_deployment", Target: kube.DeploymentSnapshot{Context: "production", Namespace: "default", Name: "api"}, Reason: strings.Repeat("reason ", 1000)}}
	view := ansi.Strip(m.View().Content)
	for _, s := range []string{"default/api", "production", "Press y to submit"} {
		if !strings.Contains(view, s) {
			t.Errorf("confirmation should show %q before a long reason", s)
		}
	}
}

func TestSelectionMarkerIsNotTakenFromUnselectedText(t *testing.T) {
	m := New(http.NewServeMux(), "test", false)
	m.page = investigationsPage
	m.width = 100
	m.height = 22
	for i := range 35 {
		title := fmt.Sprintf("incident-%02d", i)
		if i == 0 {
			title = "› misleading title"
		}
		m.incidents = append(m.incidents, incidents.Incident{Title: title, Scope: "cluster"})
	}
	m.selected = 34
	if !strings.Contains(ansi.Strip(m.View().Content), "incident-34") {
		t.Fatal("selected incident hidden by marker in an earlier title")
	}
}
func TestSafeTextStripsTerminalControlsAndTabs(t *testing.T) {
	got := safeText("a\tb\x1b]0;unsafe\x07c")
	if strings.ContainsAny(got, "\x1b\t\a") || !strings.Contains(got, "a    b") {
		t.Fatalf("unsafe terminal text: %q", got)
	}
}
