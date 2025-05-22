package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewMainDomainInput shows the main domain configuration screen
func viewDNSConfig(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Configure DNS for Unbind"))
	s.WriteString("\n\n")

	// Display detected IP addresses
	if m.dnsInfo != nil {
		if m.dnsInfo.ExternalIP != "" {
			s.WriteString(m.styles.Bold.Render("External IP: "))
			s.WriteString(m.styles.Key.Render(m.dnsInfo.ExternalIP))
			s.WriteString("\n\n")
		}
	}

	// DNS Configuration instructions
	s.WriteString(m.styles.Normal.Render("For Unbind to work properly, you need to configure DNS entries pointing to your external IP address."))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Bold.Render("Option 1 (Recommended): Create a wildcard A record"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Create an 'A' record for *.yourdomain.com → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n\n")
	s.WriteString(m.styles.Bold.Render("Option 2: Create a standalone A record"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Create an 'A' record for yourdomain.com → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("This is the domain you'll use to access Unbind."))
	s.WriteString("\n")
	s.WriteString(m.styles.Warning.Render(" Note: Not using a wildcard A record will disable automatic domain generation for unbind services"))
	s.WriteString("\n\n")

	// Domain input field
	domainInput := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#009900")).
		Padding(0, 1).
		Render(fmt.Sprintf("Domain: %s", m.domainInput.View()))

	s.WriteString(domainInput)
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("Enter your domain (e.g. yourdomain.com)"))
	s.WriteString("\n\n")

	// Continue button
	continuePrompt := m.styles.HighlightButton.Render(" Press Enter to validate DNS ")
	s.WriteString(continuePrompt)
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+c to quit"))

	return s.String()
}

func (m Model) updateDNSConfigState(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle text input updates
	m.domainInput, cmd = m.domainInput.Update(msg)

	// Store the domain value
	if m.dnsInfo == nil {
		m.dnsInfo = &dnsInfo{}
	}
	m.dnsInfo.Domain = m.domainInput.Value()

	// If Enter was pressed with a valid domain, start validation
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
		if m.dnsInfo.Domain != "" {
			// Parse the domain and determine if it's a wildcard
			domain := m.dnsInfo.Domain
			m.dnsInfo.UnbindDomain = strings.TrimPrefix(domain, "*.")

			m.state = StateDNSValidation
			m.isLoading = true
			m.dnsInfo.ValidationStarted = true
			m.dnsInfo.TestingStartTime = time.Now()

			return m, tea.Batch(
				m.spinner.Tick,
				m.startMainDNSValidation(),
				dnsValidationTimeout(30*time.Second),
				m.listenForLogs(),
			)
		}
	}

	if _, ok := msg.(tea.WindowSizeMsg); ok {
		// Handle window size changes
		return m, tea.Batch(cmd, m.listenForLogs())
	}

	// For any other message, keep updating the input and listening for logs
	return m, tea.Batch(cmd, m.listenForLogs())
}

// dnsValidationTimeout generates a command that sends a timeout message after the specified duration
func dnsValidationTimeout(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg {
		return dnsValidationTimeoutMsg{}
	})
}
