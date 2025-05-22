package tui

import (
	"fmt"
	"strings"
	"time"
	
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// viewRegistryDomainInput shows the registry domain configuration screen
func viewRegistryDomainInput(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Configure Registry Domain for Unbind"))
	s.WriteString("\n\n")

	// Display detected IP addresses
	if m.dnsInfo != nil {
		if m.dnsInfo.ExternalIP != "" {
			s.WriteString(m.styles.Bold.Render("External IP: "))
			s.WriteString(m.styles.Key.Render(m.dnsInfo.ExternalIP))
			s.WriteString("\n\n")
		}
	}

	// Main domain information
	if m.dnsInfo.IsWildcard {
		s.WriteString(m.styles.Bold.Render("Wildcard Domain: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.Domain))
	} else {
		s.WriteString(m.styles.Bold.Render("Main Domain: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.UnbindDomain))
	}
	s.WriteString("\n\n")

	// Registry Domain Configuration instructions
	s.WriteString(m.styles.Normal.Render("Unbind needs a registry domain that points to your server."))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("This domain must resolve directly to your external IP without any proxying."))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Bold.Render("Required DNS Configuration:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("Create an 'A' record for your registry domain → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("If using Cloudflare, make sure proxy is disabled (grey cloud) for this domain."))
	s.WriteString("\n\n")

	// Registry Domain input field with suggestion
	if m.dnsInfo.IsWildcard {
		m.registryInput.Placeholder = "unbind-registry." + strings.TrimPrefix(m.dnsInfo.Domain, "*.")
	} else {
		m.registryInput.Placeholder = "registry." + m.dnsInfo.UnbindDomain
	}

	registryInput := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#009900")).
		Padding(0, 1).
		Render(fmt.Sprintf("Registry Domain: %s", m.registryInput.View()))

	s.WriteString(registryInput)
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("Enter your registry domain (e.g. registry.yourdomain.com)"))
	s.WriteString("\n\n")

	// Navigation hints
	s.WriteString(m.styles.Bold.Render("Navigation:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Enter"))
	s.WriteString(m.styles.Normal.Render(" to validate domain"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Ctrl+b"))
	s.WriteString(m.styles.Normal.Render(" to go back to registry type selection"))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

	return s.String()
}

// updateRegistryDomainInputState handles updates in the registry domain input state
func (m Model) updateRegistryDomainInputState(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Check if back button was pressed
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+b" {
		// Go back to registry type selection
		m.state = StateRegistryTypeSelection
		return m, m.listenForLogs()
	}

	// Handle text input updates
	m.registryInput, cmd = m.registryInput.Update(msg)

	// Store the registry domain value
	if m.dnsInfo == nil {
		m.dnsInfo = &dnsInfo{}
	}
	m.dnsInfo.RegistryDomain = m.registryInput.Value()

	// If Enter was pressed with a valid domain, start validation
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
		if m.dnsInfo.RegistryDomain != "" {
			m.state = StateRegistryDNSValidation
			m.isLoading = true
			m.dnsInfo.ValidationStarted = true
			m.dnsInfo.TestingStartTime = time.Now()

			return m, tea.Batch(
				m.spinner.Tick,
				m.startRegistryDNSValidation(),
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

// viewRegistryDNSValidation shows the registry DNS validation screen
func viewRegistryDNSValidation(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Validating Registry Domain..."))
	s.WriteString("\n\n")

	// Display what we're testing
	s.WriteString(m.styles.Bold.Render("Testing:"))
	s.WriteString("\n")
	s.WriteString(fmt.Sprintf("  • Registry Domain: %s\n", m.styles.Normal.Render(m.dnsInfo.RegistryDomain)))
	s.WriteString(fmt.Sprintf("  • Expected IP: %s\n", m.styles.Key.Render(m.dnsInfo.ExternalIP)))
	s.WriteString("\n")

	s.WriteString(m.styles.Subtle.Render("DNS changes can take up to 24-48 hours to propagate worldwide,"))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("though they often take effect within minutes."))
	s.WriteString("\n\n")

	// Process logs
	if len(m.logMessages) > 0 {
		s.WriteString(m.styles.Bold.Render("Validation Logs:"))
		s.WriteString("\n")

		// Show the last few log messages
		startIdx := 0
		if len(m.logMessages) > 8 {
			startIdx = len(m.logMessages) - 8
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

	// Status bar at the bottom
	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

	return s.String()
}

// updateRegistryDNSValidationState handles updates in the registry DNS validation state
func (m Model) updateRegistryDNSValidationState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case dnsValidationCompleteMsg:
		m.dnsInfo.ValidationSuccess = msg.success
		m.dnsInfo.CloudflareDetected = msg.cloudflare
		m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)

		if msg.success {
			// If registry validation is successful, proceed to success state
			m.state = StateDNSSuccess
			m.isLoading = false

			// Schedule automatic advancement after 1 second
			return m, tea.Batch(
				m.listenForLogs(),
				tea.Tick(1*time.Second, func(time.Time) tea.Msg {
					return autoAdvanceMsg{}
				}),
			)
		} else {
			// If validation fails, go back to registry input
			m.state = StateRegistryDomainInput
			m.isLoading = false
			m.logChan <- "Please try a different registry domain"

			// Clear registry domain to force re-entry
			m.dnsInfo.RegistryDomain = ""
			m.registryInput.SetValue("")
			m.registryInput.Focus()

			return m, m.listenForLogs()
		}

	case dnsValidationTimeoutMsg:
		m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)
		m.state = StateRegistryDomainInput
		m.isLoading = false
		m.logChan <- "Registry domain validation timed out, please try again"

		// Clear registry domain to force re-entry
		m.dnsInfo.RegistryDomain = ""
		m.registryInput.SetValue("")
		m.registryInput.Focus()

		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}