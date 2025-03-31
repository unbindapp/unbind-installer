package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// viewInstallingPackages shows the packages installation screen
func viewInstallingPackages(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing required packages..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Packages being installed
	s.WriteString(m.styles.Bold.Render("Installing:"))
	s.WriteString("\n")

	packages := []string{
		"curl",
		"wget",
		"ca-certificates",
		"apt-transport-https",
	}

	for _, pkg := range packages {
		s.WriteString(fmt.Sprintf("  %s %s\n", m.styles.Key.Render("•"), m.styles.Normal.Render(pkg)))
	}

	// Installation logs if any
	if len(m.logMessages) > 0 {
		s.WriteString("\n")
		s.WriteString(m.styles.Bold.Render("Installation logs:"))
		s.WriteString("\n")

		// Show last 5 log messages (or fewer if there aren't that many)
		startIdx := 0
		if len(m.logMessages) > 5 {
			startIdx = len(m.logMessages) - 5
		}

		for _, msg := range m.logMessages[startIdx:] {
			s.WriteString(fmt.Sprintf("  %s\n", m.styles.Subtle.Render(msg)))
		}
	}

	return s.String()
}

func (m Model) updateInstallingPackagesState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case installCompleteMsg:
		m.state = StateInstallComplete
		m.isLoading = false

		// Schedule automatic advancement after 1 second
		return m, tea.Batch(
			m.listenForLogs(),
			tea.Tick(1*time.Second, func(time.Time) tea.Msg {
				return autoAdvanceMsg{}
			}),
		)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// viewInstallComplete shows the installation complete screen
func viewInstallComplete(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Installation summary
	s.WriteString(m.styles.Bold.Render("Installed Packages:"))
	s.WriteString("\n")

	packages := []string{
		"curl",
		"wget",
		"ca-certificates",
		"apt-transport-https",
	}

	for _, pkg := range packages {
		s.WriteString(fmt.Sprintf("  %s %s\n", m.styles.Success.Render("✓"), m.styles.Normal.Render(pkg)))
	}

	s.WriteString("\n")
	s.WriteString(m.styles.Success.Render("✓ Finished installing pre-requisites!"))
	s.WriteString("\n\n")

	return s.String()
}

// updateInstallCompleteState handles updates in the installation complete state
func (m Model) updateInstallCompleteState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case autoAdvanceMsg:
		// Start IP detection for DNS configuration
		m.state = StateDetectingIPs
		m.isLoading = true
		return m, tea.Batch(
			m.spinner.Tick,
			m.startDetectingIPs(),
			m.listenForLogs(),
		)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}
