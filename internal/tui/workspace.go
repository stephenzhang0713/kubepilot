package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stephenzhang0713/kubepilot/internal/config"
)

func oneLine(s string) string { return strings.Join(strings.Fields(safeText(s)), " ") }
func wrapPanelText(width int, lines []string) string {
	return ansi.Wrap(strings.Join(lines, "\n"), max(1, width-4), "")
}
func panelWidth(width int) int {
	if width < 84 {
		return width
	}
	return (width - 2) * 57 / 100
}
func fitCell(value string, width int) string {
	value = ansi.Truncate(oneLine(value), width, "…")
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}
func listColumns(width int) (int, int, bool) {
	inner := panelWidth(width) - 4
	date := inner >= 65
	available := inner - 4
	if date {
		available -= 20
	}
	title := max(10, available*3/5)
	return title, max(8, available-title), date
}
func sideBySide(width, minHeight int, left, right []string, sectionColor color.Color, primaryLast bool) string {
	if width < 84 {
		leftText, rightText := wrapPanelText(width, left), wrapPanelText(width, right)
		leftPanel := dashPanelColor(width, max(8, lipgloss.Height(leftText)+2), leftText, sectionColor)
		rightPanel := dashPanelColor(width, max(8, lipgloss.Height(rightText)+2), rightText, colorSlate)
		if primaryLast {
			return rightPanel + "\n\n" + leftPanel
		}
		return leftPanel + "\n\n" + rightPanel
	}
	gap := 2
	leftWidth := (width - gap) * 57 / 100
	rightWidth := width - gap - leftWidth
	leftText, rightText := wrapPanelText(leftWidth, left), wrapPanelText(rightWidth, right)
	height := max(minHeight, max(14, max(lipgloss.Height(leftText), lipgloss.Height(rightText))+2))
	return lipgloss.JoinHorizontal(lipgloss.Top, dashPanelColor(leftWidth, height, leftText, sectionColor), strings.Repeat(" ", gap), dashPanelColor(rightWidth, height, rightText, colorSlate))
}
func pageHeading(p page, name, description string) string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(pageColor(p))
	return heading.Render("◆  "+name) + "\n" + dashDim.Render("   "+description) + "\n\n"
}
func (m Model) workspace(width int) string {
	switch m.page {
	case investigationsPage:
		return m.investigationsWorkspace(width)
	case repairsPage:
		return m.repairsWorkspace(width)
	case settingsPage:
		return m.settingsWorkspace(width)
	case chatPage:
		return m.chatWorkspace(width)
	}
	return ""
}
func (m Model) investigationsWorkspace(width int) string {
	left := []string{dashViolet.Render("INVESTIGATIONS"), dashDim.Render("Current session · select an incident to open its latest result"), ""}
	right := []string{dashViolet.Render("GET STARTED"), dashDim.Render("From symptom to evidence"), "", dashViolet.Render("01") + dashText.Render("  Start an investigation"), dashDim.Render("Press n and choose cluster or Pod scope."), "", dashCyan.Render("02") + dashText.Render("  Collect and analyze"), dashDim.Render("KubePilot gathers Kubernetes evidence and asks your configured model."), "", dashRose.Render("03") + dashText.Render("  Review and follow up"), dashDim.Render("Open the result, inspect evidence, then ask the assistant."), "", dashDim.Render("Session data is cleared when KubePilot exits.")}
	if len(m.incidents) == 0 {
		left = append(left, "", dashDim.Render("     ◇"), dashText.Render("  No investigations yet"), dashDim.Render("  Press n to investigate your cluster or a Pod."))
	} else {
		titleWidth, targetWidth, showDate := listColumns(width)
		header := "   " + fitCell("INCIDENT", titleWidth) + " " + fitCell("TARGET", targetWidth)
		if showDate {
			header += " CREATED"
		}
		left = append(left, dashDim.Render(header), dashDim.Render(strings.Repeat("─", max(1, panelWidth(width)-4))))
		for i, item := range m.incidents {
			marker := "  "
			style := dashText
			if i == m.selected {
				marker = "› "
				style = dashSelected
			}
			target := item.Scope
			if item.Scope == "pod" {
				target = item.Namespace + "/" + item.Pod
			}
			line := marker + fitCell(item.Title, titleWidth) + " " + fitCell(target, targetWidth)
			if showDate {
				line += " " + stamp(item.CreatedAt)
			}
			left = append(left, style.Render(ansi.Truncate(line, max(1, panelWidth(width)-4), "…")))
		}
		if m.selected < len(m.incidents) {
			item := m.incidents[m.selected]
			target := item.Scope
			if item.Scope == "pod" {
				target = item.Namespace + "/" + item.Pod
			}
			right = []string{dashViolet.Render("SELECTED INCIDENT"), "", dashDim.Render("TITLE"), dashText.Render(oneLine(item.Title)), "", dashDim.Render("TARGET"), dashText.Render(oneLine(target)), "", dashDim.Render("CREATED"), dashText.Render(stamp(item.CreatedAt)), "", dashDim.Render("Enter  Open latest investigation"), dashDim.Render("n      Start another investigation")}
		}
	}
	return pageHeading(investigationsPage, "INVESTIGATIONS", "Browse this session or begin a new diagnosis") + sideBySide(width, m.viewportHeight()-3, left, right, pageColor(investigationsPage), false)
}
func (m Model) repairsWorkspace(width int) string {
	enabled := "Disabled"
	if m.repairEnabled {
		enabled = "Enabled"
	}
	left := []string{dashAmber.Render("REPAIR PLANS"), dashDim.Render("Review each proposed action before submitting"), ""}
	right := []string{dashAmber.Render("APPROVAL & VERIFICATION"), dashDim.Render("Controlled actions"), "", dashText.Render("Remediation: " + enabled), "", dashDim.Render("Plans are generated from investigation proposals."), dashDim.Render("Review the target and reason before approving."), "", dashDim.Render("A submitted restart does not establish recovery."), dashDim.Render("Use v to check the rollout after submission."), "", dashDim.Render("Plans are available only during this session.")}
	if len(m.plans) == 0 {
		left = append(left, "", dashDim.Render("     ◇"), dashText.Render("  No repair plans"), dashDim.Render("  Prepare a plan from an investigation proposal."), "", dashDim.Render("  n  Start an investigation"))
	} else {
		targetWidth := max(8, panelWidth(width)-4-2-12-1)
		showDate := targetWidth >= 40
		if showDate {
			targetWidth -= 20
		}
		header := "   " + fitCell("STATUS", 12) + " " + fitCell("TARGET", targetWidth)
		if showDate {
			header += " CREATED"
		}
		left = append(left, dashDim.Render(header), dashDim.Render(strings.Repeat("─", max(1, panelWidth(width)-4))))
		for i, p := range m.plans {
			marker := "  "
			style := dashText
			if i == m.selected {
				marker = "› "
				style = dashSelected
			}
			line := marker + fitCell(strings.ToUpper(p.Status), 12) + " " + fitCell(p.Target.Namespace+"/"+p.Target.Name, targetWidth)
			if showDate {
				line += " " + stamp(p.CreatedAt)
			}
			left = append(left, style.Render(ansi.Truncate(line, max(1, panelWidth(width)-4), "…")))
		}
		if m.selected < len(m.plans) {
			p := m.plans[m.selected]
			right = []string{dashAmber.Render("SELECTED PLAN"), "", dashDim.Render("STATUS"), dashText.Render(oneLine(p.Status)), "", dashDim.Render("TARGET"), dashText.Render(oneLine(p.Target.Context + ":" + p.Target.Namespace + "/" + p.Target.Name)), "", dashDim.Render("ACTION"), dashText.Render(oneLine(p.Action)), "", dashDim.Render("Enter  Review the plan and its impact")}
		}
	}
	return pageHeading(repairsPage, "REPAIRS", "Review and verify proposed actions") + sideBySide(width, m.viewportHeight()-3, left, right, pageColor(repairsPage), false)
}
func (m Model) settingValue(i int) string {
	value := m.settings.Value(i)
	if i == 6 && value == "" && m.settings.Kubeconfig == m.activeSettings.Kubeconfig {
		value = m.cluster.Context
	}
	if config.Fields[i].Secret {
		if value == "" {
			return "Not set"
		}
		return "••••••••"
	}
	if value == "" {
		return "—"
	}
	return oneLine(value)
}
func (m Model) settingsWorkspace(width int) string {
	left := []string{dashRose.Render("CONFIGURATION"), dashDim.Render("Select a setting, then press Enter"), "", dashDim.Render("   SETTING                   VALUE"), dashDim.Render(strings.Repeat("─", max(1, panelWidth(width)-4)))}
	for i, field := range config.Fields {
		marker := "  "
		style := dashText
		if i == m.selected {
			marker = "› "
			style = dashSelected
		}
		line := fmt.Sprintf("%s%-25s %s", marker, field.Label, m.settingValue(i))
		left = append(left, style.Render(ansi.Truncate(line, max(1, panelWidth(width)-4), "…")))
	}
	field := config.Fields[clamp(m.selected, len(config.Fields))]
	right := []string{dashRose.Render("SETTING DETAILS"), "", dashText.Render(field.Label), dashDim.Render(field.Description), "", dashDim.Render("SAVED VALUE"), dashText.Render(m.settingValue(m.selected)), ""}
	if field.Secret {
		right = append(right, dashDim.Render("The key is hidden here and stored in the local settings file."), dashDim.Render("An empty value clears the key."), "")
	}
	if m.selected == 6 {
		right = append(right, dashDim.Render("Enter  Choose a context · applies immediately"))
	} else if field.Toggle {
		right = append(right, dashDim.Render("Enter  Toggle this setting"), dashDim.Render("Restart to apply saved changes."))
	} else {
		right = append(right, dashDim.Render("Enter  Edit this setting"), dashDim.Render("Restart to apply saved changes."))
	}
	if m.settings != m.activeSettings {
		right = append(right, "", dashAmber.Render("●  Saved changes require a restart"))
	}
	right = append(right, "", dashDim.Render("Last inspection: "+stamp(m.inspection.LastAttempt)))
	if m.inspection.Error != "" {
		right = append(right, dashAmber.Render("Inspection: "+oneLine(m.inspection.Error)))
	}
	return pageHeading(settingsPage, "SETTINGS", "Configure your local troubleshooting workspace") + sideBySide(width, m.viewportHeight()-3, left, right, pageColor(settingsPage), false)
}
func (m Model) chatWorkspace(width int) string {
	left := []string{dashCyan.Render("CONVERSATION"), dashDim.Render("Ask about evidence from the selected investigation"), ""}
	right := []string{dashViolet.Render("EVIDENCE CONTEXT"), ""}
	if m.result == nil {
		left = append(left, "", dashText.Render("  No investigation selected"), dashDim.Render("  Start an investigation to collect evidence first."), "", dashDim.Render("  n  Start investigation"), "", dashDim.Render("  ── QUESTION ─────────────────────"), dashDim.Render("  Available after an investigation"))
		right = append(right, dashDim.Render("No investigation snapshot is available."), "", dashText.Render("01  Press n to start"), dashText.Render("02  Review the collected evidence"), dashText.Render("03  Ask a follow-up question"))
	} else {
		right = append(right, dashDim.Render("SNAPSHOT"), dashText.Render(stamp(m.result.CreatedAt)), "", dashDim.Render("STATUS"), dashText.Render(oneLine(m.result.Status)), "", dashDim.Render("Answers use this investigation snapshot."), dashDim.Render("Run a new investigation for fresh evidence."))
		if len(m.turns) == 0 {
			left = append(left, dashDim.Render("No messages yet."), dashDim.Render("Ask about likely causes or safe verification steps."), "")
		}
		for _, turn := range m.turns {
			if turn.Role == "assistant" {
				left = append(left, dashCyan.Render("✦ KUBEPILOT"), safeText(turn.Text), "")
			} else {
				left = append(left, dashGreen.Render("› YOU"), safeText(turn.Text), "")
			}
		}
		left = append(left, dashDim.Render("── QUESTION ─────────────────────────────────────"))
		if m.mode == "chat-input" {
			left = append(left, dashText.Render("› "+safeText(m.chatInput)+"_"))
		} else {
			left = append(left, dashDim.Render("Press Enter to ask a question"))
		}
	}
	return pageHeading(chatPage, "ASSISTANT", "Discuss the investigation and its collected evidence") + sideBySide(width, m.viewportHeight()-3, left, right, pageColor(chatPage), true)
}
