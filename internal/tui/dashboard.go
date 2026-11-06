package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stephenzhang0713/kubepilot/internal/kube"
)

var (
	dashText     = lipgloss.NewStyle().Foreground(lipgloss.Color("#E2EAF4"))
	dashDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#8495AA"))
	dashBorder   = lipgloss.Color("#405671")
	colorBlue    = lipgloss.Color("#7CC8FF")
	colorViolet  = lipgloss.Color("#C3ACFF")
	colorCyan    = lipgloss.Color("#6DE1D4")
	colorRose    = lipgloss.Color("#F4ACDF")
	colorAmber   = lipgloss.Color("#FFD08A")
	colorSlate   = lipgloss.Color("#59718C")
	dashViolet   = lipgloss.NewStyle().Bold(true).Foreground(colorViolet)
	dashCyan     = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	dashRose     = lipgloss.NewStyle().Bold(true).Foreground(colorRose)
	dashSelected = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F2F6FF")).Background(lipgloss.Color("#263B59"))
	dashAccent   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#80CEFA"))
	dashGreen    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#88DCC0"))
	dashAmber    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F6C783"))
	dashRed      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F28B9D"))
)

// safeText ensures Kubernetes data cannot inject terminal control sequences.
func safeText(s string) string { return strings.ReplaceAll(clean(ansi.Strip(s)), "\t", "    ") }

func dashPanel(width, height int, content string) string {
	return dashPanelColor(width, height, content, dashBorder)
}
func dashPanelColor(width, height int, content string, border color.Color) string {
	style := lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(border)
	panel := style.Render(content)
	if missing := height - lipgloss.Height(panel); missing > 0 {
		panel = style.Render(content + strings.Repeat("\n ", missing))
	}
	return panel
}
func countStates(sections []kube.Section) (checked, attention, unknown int) {
	for _, s := range sections {
		checked += s.Checked
		if s.State == "attention" {
			attention++
		}
		if s.State == "unknown" || s.Truncated || s.Error != "" {
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
		{"UNKNOWN", fmt.Sprint(unknown), "groups with collection gaps", dashGreen},
	}
	if attention > 0 {
		metrics[1].style = dashRed
	}
	if unknown > 0 {
		metrics[2].style = dashAmber
	}
	borders := []color.Color{colorBlue, colorSlate, colorSlate}
	if attention > 0 {
		borders[1] = lipgloss.Color("#F28B9D")
	}
	if unknown > 0 {
		borders[2] = colorAmber
	}
	cards := make([]string, 0, 3)
	for i, v := range metrics {
		label := lipgloss.NewStyle().Bold(true).Foreground(borders[i]).Render(v.label)
		cards = append(cards, dashPanelColor(widths[i], 5, strings.Join([]string{label, v.style.Render(v.value), dashDim.Render(v.note)}, "\n"), borders[i]))
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
		label = fitCell(label, 19)
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
	right := []string{dashViolet.Render("FINDINGS & GAPS"), dashDim.Render("Observed in this snapshot"), dashDim.Render(strings.Repeat("─", max(1, rightWidth-4)))}
	if len(findings) == 0 {
		right = append(right, "", dashGreen.Render("✓  No findings in sampled resources"), "", dashDim.Render("Checks cover selected Kubernetes resources."), dashDim.Render("A clean snapshot is not a health guarantee."))
	} else {
		right = append(right, findings...)
	}
	leftText := ansi.Wrap(strings.Join(left, "\n"), leftWidth-4, "")
	rightText := ansi.Wrap(strings.Join(right, "\n"), rightWidth-4, "")
	panelHeight := max(12, max(lipgloss.Height(leftText), lipgloss.Height(rightText))+2)
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, dashPanelColor(leftWidth, panelHeight, leftText, colorBlue), strings.Repeat(" ", gap), dashPanelColor(rightWidth, panelHeight, rightText, colorViolet)))
	b.WriteString("\n\n" + dashDim.Render("NEXT  ") + dashViolet.Render("n") + dashText.Render("  Investigate") + dashDim.Render("   ·   ") + dashCyan.Render("r") + dashText.Render("  Refresh snapshot") + dashDim.Render("   ·   ") + dashRose.Render("c") + dashText.Render("  Switch context"))
	return b.String()
}
