package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewWelcome shows the welcome screen with installation information
func viewWelcome(m Model) string {
	s := strings.Builder{}

	// Show the banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Description text with styling
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Width(60).
		Align(lipgloss.Center)

	// Installation description
	description := "I will install unbind and all of its its dependencies on your system."
	s.WriteString(descStyle.Render(description))
	s.WriteString("\n\n")

	// Requirements section
	reqTitle := m.styles.Bold.Render("Requirements:")
	s.WriteString(reqTitle)
	s.WriteString("\n")

	reqList := []string{
		"Port 80 and 443 accessible, if using a firewall",
		"A domain pointing to the server IP (I will guide you through this later!)",
	}

	for _, req := range reqList {
		// Highlight the checkmark in green
		checkmark := m.styles.Success.Render("✓")
		text := m.styles.Normal.Render(req) // Remove the checkmark from the original string
		s.WriteString(checkmark + " " + text)
		s.WriteString("\n")
	}

	s.WriteString("\n")

	// What will be installed section
	installTitle := m.styles.Bold.Render("What will be installed")
	subtitle := m.styles.Subtle.Render("(most of these are automatically managed by unbind)")
	s.WriteString(installTitle)
	s.WriteString("\n")
	s.WriteString(subtitle)
	s.WriteString("\n")

	// Create a highlighted bullet
	bullet := m.styles.Key.Render("•")

	installList := []string{
		"k3s - Lightweight Kubernetes",
		"registry - Private Docker registry",
		"monitoring - Monitoring stack (Prometheus, Metrics Exporters)",
		"logging - Indexed logging (Alloy, Loki)",
		"dex - OpenID Connect provider",
		"buildkitd - Docker BuildKit daemon",
		"unbind - All unbind components",
	}

	for _, item := range installList {
		s.WriteString(bullet + " " + m.styles.Normal.Render(item))
		s.WriteString("\n")
	}

	s.WriteString("\n")

	// Footer with continue prompt
	continuePrompt := m.styles.HighlightButton.Render(" Press Enter to continue ")

	s.WriteString(descStyle.Render(continuePrompt))
	s.WriteString("\n\n")

	// Quit option
	s.WriteString(m.styles.Subtle.Render("Press 'Ctrl+c' to quit"))

	return s.String()
}

// updateWelcomeState handles updates in the welcome state
func (m Model) updateWelcomeState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" {
			// When Enter is pressed on the welcome screen,
			// transition to the k3s check state and start checking
			m.state = StateCheckK3s
			m.isLoading = true
			return m, tea.Batch(
				m.spinner.Tick,
				checkK3sCommand(),
				m.listenForLogs(),
			)
		}
	}

	// For any other message, just keep listening for logs
	return m, m.listenForLogs()
}
