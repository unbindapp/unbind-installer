package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// viewInstallingK3S shows the K3S installation screen
func viewInstallingK3S(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing K3S..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// K3S components being installed
	s.WriteString(m.styles.Bold.Render("Installing K3S with:"))
	s.WriteString("\n")

	components := []string{
		"Disabled Flannel backend",
		"Disabled kube-proxy",
		"Disabled ServiceLB",
		"Disabled Network Policy",
		"Disabled Traefik",
	}

	for _, comp := range components {
		s.WriteString(fmt.Sprintf(" %s %s\n", m.styles.Key.Render("•"), m.styles.Normal.Render(comp)))
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
			// Truncate the message if it's too long
			const maxLength = 80 // Reasonable terminal width

			displayMsg := msg
			if len(msg) > maxLength {
				displayMsg = msg[:maxLength-3] + "..."
			}

			s.WriteString(fmt.Sprintf(" %s\n", m.styles.Subtle.Render(displayMsg)))
		}
	}

	return s.String()
}

// updateInstallingK3SState handles updates in the K3S installation state
func (m Model) updateInstallingK3SState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case k3sInstallCompleteMsg:
		// Install Cilium after K3S
		m.state = StateInstallingCilium
		m.isLoading = true
		m.kubeClient = msg.kubeClient
		m.kubeConfig = msg.kubeConfig

		return m, tea.Batch(
			m.spinner.Tick,
			m.installCilium(),
			m.listenForLogs(),
		)

	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}
