package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

var (
	dashText   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2EAF4"))
	dashDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8495AA"))
	dashBorder = lipgloss.Color("#384B63")
	dashAccent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#80CEFA"))
	dashGreen  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#88DCC0"))
	dashAmber  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F6C783"))
	dashRed    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F28B9D"))
)

// safeText ensures Kubernetes data cannot inject terminal control sequences.
func safeText(s string) string { return clean(ansi.Strip(s)) }

func dashPanel(width, height int, content string) string {
	return lipgloss.NewStyle().Width(width).Height(height-2).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(dashBorder).Render(content)
}
func countStates(sections []kube.Section) (checked, attention, unknown int) {
	for _, s := range sections {
		checked += s.Checked
		switch s.State {
		case "attention":
			attention++
		case "unknown":
			unknown++
		}
	}
	return
}
func (m Model) dashboard(width int) string {
	var b strings.Builder
	heading := dashAccent.Render("◆  CLUSTER OVERVIEW")
	snapshot := "NO SNAPSHOT"
	if m.overview != nil {
		snapshot = "SNAPSHOT  " + stamp(m.overview.CollectedAt)
	}
	snapshotLabel := dashDim.Render(snapshot)
	space := max(2, width-lipgloss.Width(heading)-lipgloss.Width(snapshotLabel))
	b.WriteString(heading + strings.Repeat(" ", space) + snapshotLabel + "\n")
	b.WriteString(dashDim.Render("Read-only resource checks  ·  evidence for your next investigation") + "\n\n")
	if m.overview == nil {
		b.WriteString(dashPanel(width, 7, strings.Join([]string{
			dashAccent.Render("◈  CONNECT YOUR CLUSTER"), "", dashText.Render("No snapshot is available for this session."), dashDim.Render("c  Select context     r  Run checks     n  Start investigation"),
		}, "\n")))
		return b.String()
	}
	sections := m.overview.Sections
	checked, attention, unknown := countStates(sections)
	gap := 2
	cardWidth := (width - 2*gap) / 3
	widths := []int{cardWidth, cardWidth, width - 2*gap - 2*cardWidth}
	metrics := []struct {
		label, value, note string
		style              lipgloss.Style
	}{
		{"RESOURCES", fmt.Sprint(checked), fmt.Sprintf("across %d checks", len(sections)), dashAccent},
		{"NEEDS ATTENTION", fmt.Sprint(attention), "resource groups with findings", dashGreen},
		{"UNKNOWN", fmt.Sprint(unknown), "resource groups not fully checked", dashGreen},
	}
	if attention > 0 {
		metrics[1].style = dashRed
	}
	if unknown > 0 {
		metrics[2].style = dashAmber
	}
	cards := make([]string, 0, 3)
	for i, v := range metrics {
		cards = append(cards, dashPanel(widths[i], 5, strings.Join([]string{dashDim.Render(v.label), v.style.Render(v.value), dashDim.Render(v.note)}, "\n")))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cards[0], strings.Repeat(" ", gap), cards[1], strings.Repeat(" ", gap), cards[2]) + "\n\n")
	leftWidth := max(36, (width-gap)*55/100)
	rightWidth := width - gap - leftWidth
	left := []string{dashAccent.Render("RESOURCE CHECKS"), dashDim.Render(fmt.Sprintf("%-22s %-12s %s", "KIND", "STATUS", "CHECKED")), dashDim.Render(strings.Repeat("─", max(1, leftWidth-4)))}
	findings := []string{}
	for _, section := range sections {
		symbol, statusStyle := "●", dashGreen
		if section.State == "attention" {
			symbol, statusStyle = "▲", dashRed
		} else if section.State != "ok" {
			symbol, statusStyle = "?", dashAmber
		}
		label := ansi.Truncate(safeText(strings.ToUpper(section.Name)), 19, "…")
		label = fmt.Sprintf("%-19s", label)
		left = append(left, fmt.Sprintf("%s  %s %s  %s", statusStyle.Render(symbol), dashText.Render(label), statusStyle.Render(fmt.Sprintf("%-10s", strings.ToUpper(section.State))), dashText.Render(fmt.Sprint(section.Checked))))
		if section.Error != "" {
			findings = append(findings, dashAmber.Render("?  "+safeText(section.Name)+" · unavailable"), safeText(section.Error), "")
		}
		if section.Truncated {
			findings = append(findings, dashAmber.Render("?  "+safeText(section.Name)+" · truncated"), "Only part of this resource group was checked.", "")
		}
		for _, finding := range section.Findings {
			findings = append(findings, dashRed.Render("▲  "+safeText(finding.Resource)), safeText(finding.Detail), "")
		}
	}
	right := []string{dashAccent.Render("FINDINGS & GAPS"), dashDim.Render("Observed in this snapshot"), dashDim.Render(strings.Repeat("─", max(1, rightWidth-4)))}
	if len(findings) == 0 {
		right = append(right, "", dashGreen.Render("✓  No findings in sampled resources"), "", dashDim.Render("Checks cover selected Kubernetes resources."), dashDim.Render("A clean snapshot is not a health guarantee."))
	} else {
		right = append(right, findings...)
	}
	leftText := lipgloss.NewStyle().Width(leftWidth - 4).Render(strings.Join(left, "\n"))
	rightText := lipgloss.NewStyle().Width(rightWidth - 4).Render(strings.Join(right, "\n"))
	panelHeight := max(12, max(lipgloss.Height(leftText), lipgloss.Height(rightText))+2)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, dashPanel(leftWidth, panelHeight, leftText), strings.Repeat(" ", gap), dashPanel(rightWidth, panelHeight, rightText)))
	b.WriteString("\n\n" + dashDim.Render("NEXT  ") + dashText.Render("n  Investigate") + dashDim.Render("   ·   ") + dashText.Render("r  Refresh snapshot") + dashDim.Render("   ·   ") + dashText.Render("c  Switch context"))
	return b.String()
}
