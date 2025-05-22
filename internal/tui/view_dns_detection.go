package tui

import (
	"fmt"
	"strings"
	
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// viewDetectingIPs shows the IP detection screen
func viewDetectingIPs(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Detecting your network configuration..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Process logs
	if len(m.logMessages) > 0 {
		s.WriteString(m.styles.Bold.Render("Network Detection:"))
		s.WriteString("\n")

		// Show the last few log messages
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

// updateDetectingIPsState handles updates in the detecting IPs state
func (m Model) updateDetectingIPsState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case detectIPsCompleteMsg:
		m.state = StateDNSConfig
		m.isLoading = false

		// Initialize DNS info if needed
		if m.dnsInfo == nil {
			m.dnsInfo = &dnsInfo{}
		}

		// Store the detected IPs
		if msg.ipInfo != nil {
			m.dnsInfo.InternalIP = msg.ipInfo.InternalIP
			m.dnsInfo.ExternalIP = msg.ipInfo.ExternalIP
			m.dnsInfo.CIDR = msg.ipInfo.CIDR
		}

		// Focus the domain input field
		m.domainInput.Focus()

		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}