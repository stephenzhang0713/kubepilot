package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stephenzhang0713/kubepilot/internal/config"
)

var (
	accent    = lipgloss.Color("#7DD3FC")
	muted     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8293A8"))
	title     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#E7F1FF"))
	highlight = lipgloss.NewStyle().Bold(true).Foreground(accent)
	good      = lipgloss.NewStyle().Foreground(lipgloss.Color("#86EFAC"))
	warning   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF77"))
	bad       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FB7185"))
)

// clean prevents cluster and model output from sending terminal control sequences.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, s)
}
func stamp(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
func state(s string) string {
	switch strings.ToLower(s) {
	case "ok", "completed", "verified":
		return good.Render(strings.ToUpper(s))
	case "attention", "pending", "submitted":
		return warning.Render(strings.ToUpper(s))
	case "unknown", "failed", "error":
		return bad.Render(strings.ToUpper(s))
	default:
		return muted.Render(strings.ToUpper(s))
	}
}
func (m Model) viewportHeight() int { return max(1, m.height-8) }
func (m Model) viewportLines() []string {
	width := max(1, m.width-6)
	if m.page == overviewPage && m.mode == "" && m.width >= 90 && m.height >= 22 {
		return strings.Split(m.dashboard(width), "\n")
	}
	text := lipgloss.NewStyle().Width(width).Render(styleContent(clean(ansi.Strip(m.content()))))
	return strings.Split(text, "\n")
}
func (m Model) maxOffset() int { return max(0, len(m.viewportLines())-m.viewportHeight()) }

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}
func (m Model) render() string {
	if m.width < 30 || m.height < 10 {
		return "KubePilot\nPlease enlarge your terminal (30 × 10 minimum)."
	}
	width := m.width - 4
	brand := highlight.Render("◈  KUBEPILOT") + muted.Render("   /   SRE WORKSPACE")
	context := m.cluster.Context
	if context == "" {
		context = "not configured"
	}
	context = strings.ReplaceAll(safeText(context), "\n", " ")
	modelName := strings.ReplaceAll(safeText(m.modelName), "\n", " ")
	session := muted.Render("LOCAL SESSION")
	if m.width >= 70 {
		brand += strings.Repeat(" ", max(1, m.width-2-lipgloss.Width(brand)-lipgloss.Width(session))) + session
	}
	header := brand + "\n" + muted.Render("   CONTEXT  ") + dashText.Render(context) + muted.Render("    ·    MODEL  ") + dashText.Render(modelName)

	var tabs []string
	for i, p := range pages {
		label := fmt.Sprintf(" %d %s ", i+1, p)
		if page(i) == m.page {
			tabs = append(tabs, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0B1726")).Background(accent).Render(label))
		} else {
			tabs = append(tabs, muted.Render(label))
		}
	}
	nav := strings.Join(tabs, " ")
	if m.width < 72 {
		nav = fmt.Sprintf("[1] [2] [3] [4] [5]  %s", pages[m.page])
	}
	var headerLines []string
	for line := range strings.SplitSeq(header, "\n") {
		headerLines = append(headerLines, ansi.Truncate(line, m.width-2, "…"))
	}
	header = strings.Join(headerLines, "\n")
	nav = ansi.Truncate(nav, m.width-2, "…")
	lines := m.viewportLines()
	available := m.viewportHeight()
	offset := min(max(0, m.offset), max(0, len(lines)-available))
	if m.mode == "chat-input" {
		offset = max(0, len(lines)-available)
	}
	if m.mode == "contexts" || (m.mode == "" && (m.page == investigationsPage || m.page == repairsPage || m.page == settingsPage)) {
		offset = min(max(0, m.selected-available+3), max(0, len(lines)-available))
	}
	end := min(len(lines), offset+available)
	body := strings.Join(lines[offset:end], "\n")
	body = lipgloss.NewStyle().Width(width).Height(available).Padding(0, 1).Render(body)

	status := m.status
	if m.busy {
		status = []string{"◐", "◓", "◑", "◒"}[m.frame%4] + " " + m.busyLabel + "…"
	}
	if status == "" {
		status = "Ready"
	}
	if m.statusError {
		status = bad.Render(clean(status))
	} else {
		status = muted.Render(clean(status))
	}
	help := "1–5 pages  ·  n investigate  ·  r refresh  ·  c context  ·  q quit"
	switch m.mode {
	case "chat-input":
		help = "enter send  ·  ctrl+u clear  ·  esc cancel"
	case "setting-edit":
		help = "enter save  ·  ctrl+u clear  ·  esc cancel"
	case "form":
		help = "tab move  ·  space select scope  ·  enter investigate  ·  esc cancel"
	case "contexts":
		help = "↑/↓ select  ·  enter connect  ·  esc back"
	case "result":
		help = "↑/↓ scroll  ·  a ask assistant  ·  [/] proposal  ·  p prepare  ·  esc back"
	case "confirm":
		help = "y confirm restart  ·  n / esc cancel"
	case "plan":
		help = "↑/↓ scroll  ·  a approve  ·  x reject  ·  v verify  ·  esc back"
	default:
		if m.page == chatPage {
			help = "enter ask  ·  ↑/↓ scroll  ·  1–5 switch  ·  q quit"
		}
		if m.page == settingsPage {
			help = "↑/↓ select  ·  enter edit / toggle  ·  c context  ·  q quit"
		}
		if m.page == investigationsPage {
			help = "n new investigation  ·  ↑/↓ select  ·  enter open  ·  r refresh  ·  q quit"
		}
		if m.page == repairsPage {
			help = "↑/↓ select  ·  enter review  ·  r refresh  ·  q quit"
		}
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(header + "\n" + nav + "\n" + body + "\n" + ansi.Truncate(status, m.width-2, "…") + "\n" + muted.Render(ansi.Truncate(help, m.width-2, "…")))
}
func (m Model) content() string {
	var b strings.Builder
	heading := func(name, sub string) { fmt.Fprintf(&b, "%s\n%s\n\n", title.Render(name), muted.Render(sub)) }
	switch m.mode {
	case "setting-edit":
		field := config.Fields[m.selected]
		heading("Edit "+field.Label, field.Description)
		value := m.editValue
		if field.Secret {
			value = strings.Repeat("•", len([]rune(value)))
			b.WriteString("Enter a new value; saving an empty value clears it.\n\n")
		}
		fmt.Fprintf(&b, "› %s_\n\n", value)
		b.WriteString("Enter saves to local settings. Restart KubePilot to apply.")
		return b.String()
	case "contexts":
		heading("Choose a Kubernetes context", "Loaded from your local kubeconfig")
		if len(m.contexts) == 0 {
			b.WriteString("No contexts found. Check KUBECONFIG.")
		}
		for i, c := range m.contexts {
			marker := "  "
			if i == m.selected {
				marker = "› "
			}
			current := ""
			if c == m.cluster.Context {
				current = "  (active)"
			}
			fmt.Fprintf(&b, "%s%s%s\n", marker, c, current)
		}
		return b.String()
	case "form":
		heading("New investigation", "Collect evidence and ask the configured model for an analysis")
		labels := []string{"Scope", "Title", "Namespace", "Pod"}
		values := []string{m.scope, m.fields[0], m.fields[1], m.fields[2]}
		for i := range labels {
			marker := "  "
			if i == m.field {
				marker = "› "
			}
			if m.scope == "cluster" && i >= 2 {
				continue
			}
			fmt.Fprintf(&b, "%s%-12s %s\n\n", marker, labels[i], values[i])
		}
		b.WriteString(muted.Render("Enter starts the investigation. Evidence is sent to the configured model."))
		return b.String()
	case "result":
		heading("Investigation", "Analysis and supporting evidence")
		if m.result == nil {
			return b.String() + "No result available."
		}
		r := m.result
		fmt.Fprintf(&b, "Status: %s   Created: %s\n\n", state(r.Status), stamp(r.CreatedAt))
		if r.Error != "" {
			fmt.Fprintf(&b, "ERROR\n%s\n\n", r.Error)
		}
		fmt.Fprintf(&b, "ANALYSIS\n%s\n\n", r.Analysis)
		for _, w := range r.Evidence.Warnings {
			fmt.Fprintf(&b, "WARNING  %s\n", w)
		}
		if len(r.Candidates) > 0 {
			fmt.Fprintf(&b, "\nPROPOSALS  [%d/%d]\n", m.candidate+1, len(r.Candidates))
			for i, c := range r.Candidates {
				marker := "  "
				if i == m.candidate {
					marker = "› "
				}
				fmt.Fprintf(&b, "%s%s  %s/%s\n  %s\n", marker, c.Action, c.Namespace, c.Name, c.Reason)
			}
		}
		for _, item := range []struct{ name, value string }{{"CLUSTER", r.Evidence.Cluster}, {"POD", r.Evidence.Pod}, {"EVENTS", r.Evidence.Events}, {"LOGS", r.Evidence.Logs}} {
			if item.value != "" {
				fmt.Fprintf(&b, "\n%s\n%s\n", item.name, item.value)
			}
		}
		for _, t := range r.Trace {
			fmt.Fprintf(&b, "\nTOOL  %s  %s  %s", stamp(t.Time), t.Tool, t.Outcome)
		}
		return b.String()
	case "plan", "confirm":
		if len(m.plans) == 0 {
			return "No plan selected."
		}
		p := m.plans[m.selected]
		heading("Review repair plan", "Review the target and impact before approving")
		fmt.Fprintf(&b, "STATUS       %s\nACTION       %s\nCONTEXT      %s\nTARGET       %s/%s\nREPLICAS     %d\nCREATED      %s\n\nREASON\n%s\n", state(p.Status), p.Action, p.Target.Context, p.Target.Namespace, p.Target.Name, p.Target.Replicas, stamp(p.CreatedAt), p.Reason)
		if p.Error != "" {
			fmt.Fprintf(&b, "\nERROR\n%s\n", p.Error)
		}
		if p.Verification != nil {
			v := p.Verification
			fmt.Fprintf(&b, "\nROLLOUT  updated %d/%d  ready %d/%d  available %d/%d\nObserved: %s  verified: %t\n", v.Updated, v.Desired, v.Ready, v.Desired, v.Available, v.Desired, stamp(v.ObservedAt), v.Verified)
		}
		if m.mode == "confirm" {
			fmt.Fprintf(&b, "\nCONFIRM: Restart Deployment %s/%s in %s?\nThis may disrupt service. Press y to submit, n to cancel.", p.Target.Namespace, p.Target.Name, p.Target.Context)
		}
		return b.String()
	}
	switch m.page {
	case overviewPage:
		heading("Cluster overview", "Read-only snapshot of Kubernetes resources")
		if m.overview == nil {
			b.WriteString("     ╭─────────╮\n     │  ◈  ◈  │    No cluster snapshot yet\n     │    ◈    │    c  choose a context\n     ╰────┬────╯    r  run read-only checks\n          ╰───────  n  start an investigation")
			break
		}
		attention, unknown, checked := 0, 0, 0
		for _, section := range m.overview.Sections {
			checked += section.Checked
			switch section.State {
			case "attention":
				attention++
			case "unknown":
				unknown++
			}
		}
		if m.width >= 78 {
			fmt.Fprintf(&b, "   ╭──────────────╮   ╭──────────────╮   ╭──────────────╮\n   │  ◉  %-8d │   │  !  %-8d │   │  ?  %-8d │\n   │  CHECKED     │   │  ATTN GROUPS │   │  UNKNOWN GRP │\n   ╰──────────────╯   ╰──────────────╯   ╰──────────────╯\n\n", checked, attention, unknown)
		} else {
			fmt.Fprintf(&b, "◉ %d checked  ▲ %d attention  ? %d unknown\n\n", checked, attention, unknown)
		}
		fmt.Fprintf(&b, "Snapshot  %s  ·  states are not a health guarantee\n\n", stamp(m.overview.CollectedAt))
		for _, s := range m.overview.Sections {
			icon := "●"
			if s.State == "attention" {
				icon = "▲"
			} else if s.State == "unknown" {
				icon = "?"
			}
			fmt.Fprintf(&b, "%s  %-18s %s   %d checked\n", icon, strings.ToUpper(s.Name), state(s.State), s.Checked)
			if s.Error != "" {
				fmt.Fprintf(&b, "  %s\n", s.Error)
			}
			if s.Truncated {
				b.WriteString("  Results truncated\n")
			}
			for _, f := range s.Findings {
				fmt.Fprintf(&b, "  • %s  %s\n", f.Resource, f.Detail)
			}
			b.WriteString("\n")
		}
	case investigationsPage:
		heading("Investigations", "Select an incident or press n to start a new investigation (session only)")
		if len(m.incidents) == 0 {
			b.WriteString("No incidents yet. Press n to investigate.")
		}
		for i, item := range m.incidents {
			marker := "  "
			if i == m.selected {
				marker = "› "
			}
			target := item.Scope
			if item.Scope == "pod" {
				target = item.Namespace + "/" + item.Pod
			}
			fmt.Fprintf(&b, "%s%s  ·  %s  ·  %s\n", marker, item.Title, target, stamp(item.CreatedAt))
		}
	case repairsPage:
		heading("Repair plans", "Review, approve and verify proposed actions")
		if !m.repairEnabled {
			b.WriteString("Writes are disabled. Enable remediation in Settings and restart.\n\n")
		}
		if len(m.plans) == 0 {
			b.WriteString("No plans yet. Prepare one from an investigation proposal.")
		}
		for i, p := range m.plans {
			marker := "  "
			if i == m.selected {
				marker = "› "
			}
			fmt.Fprintf(&b, "%s%s  %s/%s  ·  %s\n", marker, state(p.Status), p.Target.Namespace, p.Target.Name, stamp(p.CreatedAt))
		}
	case chatPage:
		heading("✦  Investigation assistant", "Ask follow-up questions about the latest collected evidence")
		if m.result == nil {
			b.WriteString("     ╭───────────╮\n     │  ◉   ◉    │\n     │    ▽      │   No investigation selected\n     ╰────┬──────╯   Start one from Investigations first.\n          ╰───────   Evidence stays in this session.\n")
			break
		}
		fmt.Fprintf(&b, "Snapshot  %s   ·   %s\n", stamp(m.result.CreatedAt), m.result.Status)
		if len(m.turns) == 0 {
			b.WriteString("\n     ╭───────────╮\n     │  ◉   ◉    │   Ask about the evidence, likely causes,\n     │    ▽      │   or safe verification steps.\n     ╰───────────╯\n")
		}
		for _, turn := range m.turns {
			label := "YOU"
			icon := "›"
			if turn.Role == "assistant" {
				label = "KUBEPILOT"
				icon = "✦"
			}
			fmt.Fprintf(&b, "\n%s  %s\n%s\n", icon, label, turn.Text)
		}
		if m.mode == "chat-input" {
			fmt.Fprintf(&b, "\n╭─ YOUR QUESTION ─────────────────────\n│ %s_\n╰─────────────────────────────────────", m.chatInput)
		} else {
			b.WriteString("\n╭─ YOUR QUESTION ─────────────────────\n│ Press Enter to ask a question\n╰─────────────────────────────────────")
		}
	case settingsPage:
		heading("Settings", "Enter to edit • c to choose a Kubernetes context")
		for i, field := range config.Fields {
			marker := "  "
			if i == m.selected {
				marker = "› "
			}
			value := m.settings.Value(i)
			if i == 6 && value == "" && m.settings.Kubeconfig == m.activeSettings.Kubeconfig {
				value = m.cluster.Context
			}
			if field.Secret {
				if value == "" {
					value = "not set"
				} else {
					value = "••••••••"
				}
			} else if value == "" {
				value = "—"
			}
			fmt.Fprintf(&b, "%s%-21s %s\n", marker, field.Label, value)
		}
		if m.settings != m.activeSettings {
			b.WriteString("\nSaved changes will apply after restart.\n")
		}
		fmt.Fprintf(&b, "\nSecrets are stored in a local settings file.\nInspection last attempted: %s\n", stamp(m.inspection.LastAttempt))
		if m.inspection.Error != "" {
			fmt.Fprintf(&b, "Inspection error: %s", m.inspection.Error)
		}
	}
	return b.String()
}

// Style only after stripping terminal sequences from external data.
func styleContent(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case i == 0:
			lines[i] = title.Render(line)
		case i == 1:
			lines[i] = muted.Render(line)
		case strings.HasPrefix(trim, "› "):
			lines[i] = highlight.Render(line)
		case strings.HasPrefix(trim, "▲"):
			lines[i] = warning.Render(line)
		case strings.HasPrefix(trim, "?"):
			lines[i] = bad.Render(line)
		case strings.HasPrefix(trim, "●") || strings.HasPrefix(trim, "✦"):
			lines[i] = highlight.Render(line)
		case strings.HasPrefix(trim, "╭") || strings.HasPrefix(trim, "╰"):
			lines[i] = muted.Render(line)
		}
	}
	return strings.Join(lines, "\n")
}
