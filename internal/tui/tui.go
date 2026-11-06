// Package tui provides the local, keyboard-driven KubePilot interface.
package tui

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/stephenzhang0713/kubepilot/internal/agent"
	"github.com/stephenzhang0713/kubepilot/internal/config"
	"github.com/stephenzhang0713/kubepilot/internal/incidents"
	"github.com/stephenzhang0713/kubepilot/internal/inspection"
	"github.com/stephenzhang0713/kubepilot/internal/investigation"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
	"github.com/stephenzhang0713/kubepilot/internal/remediation"
)

type page int

const (
	overviewPage page = iota
	investigationsPage
	chatPage
	repairsPage
	settingsPage
)

var pages = []string{"Overview", "Investigations", "Assistant", "Repairs", "Settings"}

type response struct {
	action string
	data   []byte
	err    error
}
type tick time.Time
type chatReply struct {
	text string
	err  error
}

type Model struct {
	handler          http.Handler
	modelName        string
	repairEnabled    bool
	width, height    int
	page             page
	mode             string
	busy             bool
	busyLabel        string
	frame            int
	status           string
	statusError      bool
	offset, selected int
	cluster          kube.ConfigStatus
	contexts         []string
	overview         *kube.Overview
	inspection       inspection.Status
	incidents        []incidents.Incident
	result           *investigation.Result
	plans            []remediation.Plan
	fields           [3]string
	field            int
	scope            string
	candidate        int
	settings         config.Config
	activeSettings   config.Config
	settingsPath     string
	editValue        string
	chat             func(context.Context, investigation.Result, []agent.Turn) (string, error)
	turns            []agent.Turn
	chatInput        string
	chatResultID     string
}

type Option func(*Model)

func WithChat(fn func(context.Context, investigation.Result, []agent.Turn) (string, error)) Option {
	return func(m *Model) { m.chat = fn }
}
func WithSettings(settings config.Config, path string) Option {
	return func(m *Model) { m.settings = settings; m.activeSettings = settings; m.settingsPath = path }
}
func New(handler http.Handler, modelName string, repairEnabled bool, options ...Option) Model {
	m := Model{handler: handler, modelName: modelName, repairEnabled: repairEnabled, width: 100, height: 30, scope: "cluster", fields: [3]string{"Cluster investigation", "default", ""}}
	for _, option := range options {
		option(&m)
	}
	return m
}
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.request("bootstrap", "GET", "/api/kubeconfig", nil), pulse())
}
func pulse() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tick(t) })
}

func (m Model) call(ctx context.Context, method, path string, body any, confirm bool) ([]byte, error) {
	var content []byte
	var err error
	if body != nil {
		content, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	req := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(content))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if confirm {
		req.Header.Set("X-KubePilot-Confirm", "restart-deployment")
	}
	res := httptest.NewRecorder()
	m.handler.ServeHTTP(res, req)
	if res.Code < 200 || res.Code >= 300 {
		return nil, fmt.Errorf("%s", strings.TrimSpace(res.Body.String()))
	}
	return res.Body.Bytes(), nil
}
func (m Model) request(action, method, path string, body any) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute+10*time.Second)
		defer cancel()
		data, err := m.call(ctx, method, path, body, action == "execute")
		return response{action, data, err}
	}
}
func (m Model) start(action, label, method, path string, body any) (tea.Model, tea.Cmd) {
	m.busy, m.busyLabel, m.statusError = true, label, false
	return m, m.request(action, method, path, body)
}
func (m Model) loadPage() tea.Cmd {
	switch m.page {
	case overviewPage:
		return m.request("overview", "GET", "/api/cluster/overview", nil)
	case investigationsPage:
		return m.request("incidents", "GET", "/api/incidents", nil)
	case repairsPage:
		return m.request("plans", "GET", "/api/remediations", nil)
	case chatPage:
		return func() tea.Msg { return response{action: "chat-page"} }
	default:
		return m.request("settings", "GET", "/api/inspection", nil)
	}
}
func (m Model) refresh() (tea.Model, tea.Cmd) {
	m.busy, m.busyLabel, m.statusError = true, "Refreshing", false
	return m, m.loadPage()
}
func (m Model) investigate() tea.Cmd {
	input := struct {
		Title     string `json:"title"`
		Scope     string `json:"scope"`
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
	}{m.fields[0], m.scope, "", ""}
	if m.scope == "pod" {
		input.Namespace, input.Pod = m.fields[1], m.fields[2]
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute+10*time.Second)
		defer cancel()
		data, err := m.call(ctx, "POST", "/api/incidents", input, false)
		if err != nil {
			return response{"investigate", nil, err}
		}
		var item incidents.Incident
		if err := json.Unmarshal(data, &item); err != nil {
			return response{"investigate", nil, err}
		}
		data, err = m.call(ctx, "POST", "/api/incidents/"+item.ID+"/investigations", nil, false)
		return response{"investigate", data, err}
	}
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if m.busy {
			return m, nil
		}
		pasted := strings.ReplaceAll(strings.ReplaceAll(clean(msg.Content), "\n", " "), "\t", " ")
		switch m.mode {
		case "chat-input":
			m.chatInput = appendInput(m.chatInput, pasted, 4000)
		case "setting-edit":
			m.editValue = appendInput(m.editValue, pasted, 4096)
		case "form":
			if m.field > 0 {
				m.fields[m.field-1] = appendInput(m.fields[m.field-1], pasted, 256)
			}
		}
		return m, nil
	case chatReply:
		m.busy = false
		if msg.err != nil {
			m.status = "Assistant: " + msg.err.Error()
			m.statusError = true
		} else {
			m.turns = append(m.turns, agent.Turn{Role: "assistant", Text: msg.text})
			m.status = "Response ready"
			m.statusError = false
		}
		m.offset = m.maxOffset()
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tick:
		m.frame++
		return m, pulse()
	case response:
		m.busy = false
		if msg.err != nil {
			m.status, m.statusError = msg.err.Error(), true
			return m, nil
		}
		m.statusError = false
		var err error
		switch msg.action {
		case "bootstrap", "context":
			err = json.Unmarshal(msg.data, &m.cluster)
			if msg.action == "context" {
				m.overview, m.result = nil, nil
				m.turns = nil
				m.chatInput = ""
				m.chatResultID = ""
				m.mode = ""
				m.selected = 0
				m.status = "Context selected. Refresh to verify connectivity."
				if m.settingsPath != "" {
					next := m.settings
					next.Context = m.cluster.Context
					if saveErr := next.Save(m.settingsPath); saveErr != nil {
						m.status = "Context selected but could not save: " + saveErr.Error()
						m.statusError = true
					} else {
						m.settings = next
						m.activeSettings.Context = next.Context
					}
				}
			}
			if msg.action == "bootstrap" {
				return m.refresh()
			}
		case "contexts":
			var result struct {
				Contexts []string `json:"contexts"`
			}
			err = json.Unmarshal(msg.data, &result)
			m.contexts = result.Contexts
			m.selected = 0
			m.offset = 0
			m.mode = "contexts"
			if i := slices.Index(m.contexts, m.cluster.Context); i >= 0 {
				m.selected = i
			}
		case "overview":
			var result kube.Overview
			err = json.Unmarshal(msg.data, &result)
			if err == nil {
				m.overview = &result
				m.status = "Overview updated"
			}
		case "incidents":
			err = json.Unmarshal(msg.data, &m.incidents)
			slices.SortFunc(m.incidents, func(a, b incidents.Incident) int { return b.CreatedAt.Compare(a.CreatedAt) })
			m.selected = clamp(m.selected, len(m.incidents))
			m.status = "History updated"
		case "investigate", "results":
			if msg.action == "investigate" {
				var result investigation.Result
				err = json.Unmarshal(msg.data, &result)
				if err == nil {
					m.result = &result
				}
			} else {
				var results []investigation.Result
				err = json.Unmarshal(msg.data, &results)
				slices.SortFunc(results, func(a, b investigation.Result) int { return b.CreatedAt.Compare(a.CreatedAt) })
				if len(results) == 0 {
					m.status = "No investigations for this incident"
					return m, nil
				}
				m.result = &results[0]
			}
			if err == nil {
				if m.chatResultID != m.result.ID {
					m.turns = nil
					m.chatInput = ""
					m.chatResultID = m.result.ID
				}
				m.mode = "result"
				m.offset = 0
				m.candidate = 0
				m.status = "Investigation " + m.result.Status
			}
		case "plans":
			var result struct {
				Enabled bool               `json:"enabled"`
				Plans   []remediation.Plan `json:"plans"`
			}
			err = json.Unmarshal(msg.data, &result)
			m.plans, m.repairEnabled = result.Plans, result.Enabled
			slices.SortFunc(m.plans, func(a, b remediation.Plan) int { return b.CreatedAt.Compare(a.CreatedAt) })
			m.selected = clamp(m.selected, len(m.plans))
			m.status = "Plans updated"
		case "prepare", "execute", "reject", "verify":
			m.mode = ""
			m.page = repairsPage
			m.status = "Plan updated"
			return m.refresh()
		case "chat-page":
			m.status = "Ask about the latest investigation"
		case "settings":
			err = json.Unmarshal(msg.data, &m.inspection)
			m.status = "Settings updated"
		}
		if err != nil {
			m.status, m.statusError = "Could not decode response: "+err.Error(), true
		}
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.busy {
			return m, nil
		}
		if m.mode == "chat-input" {
			return m.updateChat(msg)
		}
		if m.mode == "setting-edit" {
			return m.updateSetting(msg)
		}
		if m.mode == "form" {
			return m.updateForm(msg)
		}
		if m.mode == "confirm" {
			if key == "esc" || key == "n" {
				m.mode = "plan"
				m.status = "Repair cancelled"
				return m, nil
			}
			if key == "y" && len(m.plans) > 0 {
				return m.start("execute", "Submitting restart", "POST", "/api/remediations/"+m.plans[m.selected].ID+"/execute", map[string]string{"confirm": "restart"})
			}
			return m, nil
		}
		if m.mode == "contexts" {
			switch key {
			case "esc":
				m.mode = ""
				m.offset = 0
				m.selected = 0
			case "up", "k":
				m.selected = max(0, m.selected-1)
			case "down", "j":
				m.selected = min(max(0, len(m.contexts)-1), m.selected+1)
			case "enter":
				if len(m.contexts) > 0 {
					return m.start("context", "Switching context", "PUT", "/api/kubeconfig/local", map[string]string{"context": m.contexts[m.selected]})
				}
			}
			return m, nil
		}
		if m.mode == "plan" {
			switch key {
			case "esc", "backspace":
				m.mode = ""
				m.offset = 0
			case "up", "k":
				m.offset = max(0, m.offset-1)
			case "down", "j":
				m.offset = min(m.maxOffset(), m.offset+1)
			case "pgup":
				m.offset = max(0, m.offset-m.viewportHeight())
			case "pgdown":
				m.offset = min(m.maxOffset(), m.offset+m.viewportHeight())
			case "a":
				if len(m.plans) > 0 && m.plans[m.selected].Status == "pending" {
					m.mode = "confirm"
					m.offset = 0
				}
			case "x":
				if len(m.plans) > 0 && m.plans[m.selected].Status == "pending" {
					return m.start("reject", "Rejecting plan", "POST", "/api/remediations/"+m.plans[m.selected].ID+"/reject", map[string]string{})
				}
			case "v":
				if len(m.plans) > 0 {
					return m.start("verify", "Checking rollout", "POST", "/api/remediations/"+m.plans[m.selected].ID+"/verify", map[string]string{})
				}
			}
			return m, nil
		}
		if m.mode == "result" {
			switch key {
			case "esc", "backspace":
				m.mode = ""
				m.offset = 0
				return m.refresh()
			case "up", "k":
				m.offset = max(0, m.offset-1)
			case "down", "j":
				m.offset = min(m.maxOffset(), m.offset+1)
			case "pgup":
				m.offset = max(0, m.offset-m.viewportHeight())
			case "pgdown":
				m.offset = min(m.maxOffset(), m.offset+m.viewportHeight())
			case "[":
				m.candidate = max(0, m.candidate-1)
			case "]":
				if m.result != nil {
					m.candidate = min(max(0, len(m.result.Candidates)-1), m.candidate+1)
				}
			case "a":
				m.page = chatPage
				m.mode = ""
				m.offset = m.maxOffset()
				return m, nil
			case "p":
				if m.result != nil && len(m.result.Candidates) > 0 {
					return m.start("prepare", "Preparing plan", "POST", "/api/remediations", map[string]any{"investigationId": m.result.ID, "candidateIndex": m.candidate})
				}
			}
			return m, nil
		}
		switch key {
		case "q":
			return m, tea.Quit
		case "1", "2", "3", "4", "5":
			m.page = page(key[0] - '1')
			m.offset, m.selected = 0, 0
			return m.refresh()
		case "tab", "right":
			m.page = (m.page + 1) % page(len(pages))
			m.offset, m.selected = 0, 0
			return m.refresh()
		case "shift+tab", "left":
			m.page = (m.page + page(len(pages)) - 1) % page(len(pages))
			m.offset, m.selected = 0, 0
			return m.refresh()
		case "r":
			return m.refresh()
		case "c":
			return m.start("contexts", "Loading contexts", "GET", "/api/kubeconfig/contexts", nil)
		case "n":
			if m.page == investigationsPage || m.page == overviewPage {
				m.page = investigationsPage
				m.mode = "form"
				m.field = 0
				m.status = ""
			}
		case "up", "k":
			if m.page == investigationsPage || m.page == repairsPage || m.page == settingsPage {
				m.selected = max(0, m.selected-1)
			} else {
				m.offset = max(0, m.offset-1)
			}
		case "down", "j":
			if m.page == investigationsPage {
				m.selected = min(max(0, len(m.incidents)-1), m.selected+1)
			} else if m.page == repairsPage {
				m.selected = min(max(0, len(m.plans)-1), m.selected+1)
			} else if m.page == settingsPage {
				m.selected = min(len(config.Fields)-1, m.selected+1)
			} else {
				m.offset = min(m.maxOffset(), m.offset+1)
			}
		case "pgup":
			m.offset = max(0, m.offset-m.viewportHeight())
		case "pgdown":
			m.offset = min(m.maxOffset(), m.offset+m.viewportHeight())
		case "enter":
			if m.page == chatPage {
				if m.result == nil {
					m.status = "Run an investigation first"
					m.statusError = true
					return m, nil
				}
				m.mode = "chat-input"
				m.status = ""
				return m, nil
			}
			if m.page == settingsPage {
				if m.selected == 6 {
					return m.start("contexts", "Loading contexts", "GET", "/api/kubeconfig/contexts", nil)
				}
				if m.settingsPath == "" {
					m.status = "Settings are unavailable"
					m.statusError = true
					return m, nil
				}
				if config.Fields[m.selected].Toggle {
					value := "true"
					if m.settings.Value(m.selected) == "true" {
						value = "false"
					}
					return m.saveSetting(value)
				}
				m.mode = "setting-edit"
				m.editValue = m.settings.Value(m.selected)
				if config.Fields[m.selected].Secret {
					m.editValue = ""
				}
				m.offset = 0
				m.status = ""
				return m, nil
			}
			if m.page == investigationsPage && len(m.incidents) > 0 {
				return m.start("results", "Loading investigation", "GET", "/api/incidents/"+m.incidents[m.selected].ID+"/investigations", nil)
			}
			if m.page == repairsPage && len(m.plans) > 0 {
				m.mode = "plan"
				m.offset = 0
			}
		case "esc":
			m.mode = ""
			m.offset = 0
		}
	}
	return m, nil
}
func (m Model) updateForm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ""
		return m, nil
	case "tab", "down":
		count := 4
		if m.scope == "cluster" {
			count = 2
		}
		m.field = (m.field + 1) % count
	case "shift+tab", "up":
		count := 4
		if m.scope == "cluster" {
			count = 2
		}
		m.field = (m.field + count - 1) % count
	case "space":
		if m.field == 0 {
			if m.scope == "cluster" {
				m.scope = "pod"
			} else {
				m.scope = "cluster"
			}
			return m, nil
		}
		m.fields[m.field-1] += " "
	case "enter":
		if m.field == 0 {
			if m.scope == "cluster" {
				m.scope = "pod"
			} else {
				m.scope = "cluster"
			}
			return m, nil
		}
		if strings.TrimSpace(m.fields[0]) == "" || (m.scope == "pod" && (strings.TrimSpace(m.fields[1]) == "" || strings.TrimSpace(m.fields[2]) == "")) {
			m.status = "Title and Pod target are required"
			m.statusError = true
			return m, nil
		}
		m.busy = true
		m.busyLabel = "Collecting evidence and analyzing"
		m.statusError = false
		return m, m.investigate()
	case "backspace":
		if m.field > 0 {
			s := m.fields[m.field-1]
			if len(s) > 0 {
				_, n := utf8.DecodeLastRuneInString(s)
				m.fields[m.field-1] = s[:len(s)-n]
			}
		}
	default:
		if m.field > 0 && msg.Text != "" && len([]rune(m.fields[m.field-1])) < 256 {
			m.fields[m.field-1] = appendInput(m.fields[m.field-1], msg.Text, 256)
		}
	}
	return m, nil
}
func clamp(value, length int) int { return min(max(0, value), max(0, length-1)) }

func (m Model) saveSetting(value string) (tea.Model, tea.Cmd) {
	next := m.settings
	if err := next.Set(m.selected, value); err != nil {
		m.status = err.Error()
		m.statusError = true
		return m, nil
	}
	if m.selected == 3 && next.Kubeconfig != m.settings.Kubeconfig {
		next.Context = ""
	}
	if err := next.Save(m.settingsPath); err != nil {
		m.status = "Could not save settings: " + err.Error()
		m.statusError = true
		return m, nil
	}
	m.settings = next
	m.mode = ""
	m.editValue = ""
	m.status = "Saved. Restart KubePilot to apply changes."
	m.statusError = false
	return m, nil
}
func (m Model) updateSetting(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ""
		m.editValue = ""
		m.status = "Edit cancelled"
		m.statusError = false
	case "enter":
		return m.saveSetting(m.editValue)
	case "ctrl+u":
		m.editValue = ""
	case "backspace":
		if len(m.editValue) > 0 {
			_, n := utf8.DecodeLastRuneInString(m.editValue)
			m.editValue = m.editValue[:len(m.editValue)-n]
		}
	case "space":
		m.editValue += " "
	default:
		if msg.Text != "" && len([]rune(m.editValue)) < 4096 {
			m.editValue = appendInput(m.editValue, msg.Text, 4096)
		}
	}
	return m, nil
}

func (m Model) updateChat(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = ""
		m.chatInput = ""
		return m, nil
	case "ctrl+u":
		m.chatInput = ""
	case "backspace":
		if len(m.chatInput) > 0 {
			_, n := utf8.DecodeLastRuneInString(m.chatInput)
			m.chatInput = m.chatInput[:len(m.chatInput)-n]
		}
	case "space":
		m.chatInput += " "
	case "enter":
		question := strings.TrimSpace(m.chatInput)
		if question == "" {
			return m, nil
		}
		if m.chat == nil || m.result == nil {
			m.status = "Assistant unavailable"
			m.statusError = true
			return m, nil
		}
		m.turns = append(m.turns, agent.Turn{Role: "user", Text: question})
		m.chatInput = ""
		m.mode = ""
		m.busy = true
		m.busyLabel = "Thinking with investigation evidence"
		m.offset = m.maxOffset()
		result := *m.result
		turns := append([]agent.Turn(nil), m.turns...)
		chat := m.chat
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			defer cancel()
			answer, err := chat(ctx, result, turns)
			return chatReply{text: answer, err: err}
		}
	default:
		if msg.Text != "" && len([]rune(m.chatInput)) < 4000 {
			m.chatInput = appendInput(m.chatInput, msg.Text, 4000)
		}
	}
	return m, nil
}

func appendInput(existing, added string, limit int) string {
	runes := []rune(existing)
	remaining := limit - len(runes)
	if remaining <= 0 {
		return existing
	}
	extra := []rune(added)
	if len(extra) > remaining {
		extra = extra[:remaining]
	}
	return existing + string(extra)
}
