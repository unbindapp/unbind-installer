package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// viewDNSValidation shows the DNS validation screen
func viewDNSValidation(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Validating DNS configuration..."))
	s.WriteString("\n\n")

	// Display what we're testing
	s.WriteString(m.styles.Bold.Render("Testing:"))
	s.WriteString("\n")

	if m.dnsInfo.IsWildcard {
		s.WriteString(fmt.Sprintf("  • Wildcard: %s\n", m.styles.Normal.Render(m.dnsInfo.Domain)))
	} else {
		s.WriteString(fmt.Sprintf("  • Domain: %s\n", m.styles.Normal.Render(m.dnsInfo.Domain)))
	}

	s.WriteString(fmt.Sprintf("  • Unbind: %s\n", m.styles.Normal.Render(m.dnsInfo.UnbindDomain)))
	s.WriteString(fmt.Sprintf("  • Registry: %s\n", m.styles.Normal.Render(m.dnsInfo.RegistryDomain)))
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
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+c to quit"))

	return s.String()
}

// updateDNSValidationState handles updates in the DNS validation state
func (m Model) updateDNSValidationState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case dnsValidationCompleteMsg:
		m.dnsInfo.ValidationSuccess = msg.success
		m.dnsInfo.CloudflareDetected = msg.cloudflare
		m.dnsInfo.RegistryIssue = msg.registryIssue
		m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)

		if msg.success {
			// Always go to registry type selection upon successful DNS validation
			m.state = StateRegistryTypeSelection
			m.isLoading = false
			m.logChan <- "DNS validation successful. Please configure your registry."
			return m, m.listenForLogs()
		} else {
			m.state = StateDNSFailed
			m.isLoading = false
			return m, m.listenForLogs()
		}

	case dnsValidationTimeoutMsg:
		m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)
		m.state = StateDNSFailed
		m.isLoading = false
		m.logChan <- "DNS validation timed out after 30 seconds"
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// viewDNSSuccess shows the DNS success screen
func viewDNSSuccess(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Success message
	s.WriteString(m.styles.Success.Render("✓ Configuration Successful!"))
	s.WriteString("\n\n")

	// DNS Configuration details
	s.WriteString(m.styles.Bold.Render("DNS Configuration:"))
	s.WriteString("\n")

	if m.dnsInfo.CloudflareDetected {
		s.WriteString(m.styles.Normal.Render("• Cloudflare detected: "))
		s.WriteString(m.styles.Success.Render("Yes"))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("• Wildcard domain configured correctly with Cloudflare"))
		} else {
			s.WriteString(m.styles.Normal.Render("• Domains configured correctly with Cloudflare"))
		}
	} else {
		s.WriteString(m.styles.Bold.Render("Configured domains:"))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("• " + m.dnsInfo.UnbindDomain))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("• " + m.dnsInfo.Domain + " (wildcard)"))
			s.WriteString("\n")
		}
		s.WriteString(m.styles.Bold.Render("Points to: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.ExternalIP))
	}

	// Registry Configuration details
	s.WriteString("\n\n")
	s.WriteString(m.styles.Bold.Render("Registry Configuration:"))
	s.WriteString("\n")

	if m.dnsInfo.RegistryType == RegistrySelfHosted {
		s.WriteString(m.styles.Normal.Render("• Self-hosted registry configured at:"))
		s.WriteString("\n")
		s.WriteString(m.styles.Success.Render("  " + m.dnsInfo.RegistryDomain))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("• Registry will be deployed as part of Unbind installation"))
	} else {
		s.WriteString(m.styles.Normal.Render("• External registry configured:"))
		s.WriteString("\n")
		s.WriteString(m.styles.Success.Render(fmt.Sprintf("  %s account: %s",
			getRegistryDisplayName(m.dnsInfo.RegistryHost),
			m.dnsInfo.RegistryUsername)))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("• Local registry component will be disabled"))
	}

	// Validation details
	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Validation completed in %.1f seconds", m.dnsInfo.ValidationDuration.Seconds())))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Normal.Render("Your configuration is complete and Unbind can proceed with installation."))
	s.WriteString("\n\n")

	// Continue button
	continuePrompt := m.styles.HighlightButton.Render(" Press Enter to continue ")
	s.WriteString(continuePrompt)
	s.WriteString("\n\n")

	return s.String()
}

func (m Model) updateDNSSuccessState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "enter" {
			// Start K3S installation
			m.state = StateInstallingK3S
			m.isLoading = true
			return m, tea.Batch(
				m.spinner.Tick,
				m.installK3S(),
				m.listenForLogs(),
			)
		}
	case autoAdvanceMsg:
		// Auto-advance to K3S installation
		m.state = StateInstallingK3S
		m.isLoading = true
		return m, tea.Batch(
			m.spinner.Tick,
			m.installK3S(),
			m.listenForLogs(),
		)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, m.listenForLogs()
}

// viewDNSFailed shows the DNS failure screen
func viewDNSFailed(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Error message
	s.WriteString(m.styles.Error.Render("! DNS Configuration Validation Failed"))
	s.WriteString("\n\n")

	if m.dnsInfo.RegistryIssue {
		s.WriteString(m.styles.Bold.Render("! Registry DNS configuration issue detected"))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("Please ensure that Cloudflare proxy is disabled for " + m.dnsInfo.RegistryDomain))
		s.WriteString("\n\n")
	} else {
		// Failure details
		s.WriteString(m.styles.Bold.Render("Checked domains:"))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.UnbindDomain))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.RegistryDomain))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.Domain + " (wildcard)"))
			s.WriteString("\n")
		}
		s.WriteString(m.styles.Bold.Render("Expected to point to: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.ExternalIP))
		s.WriteString("\n\n")

		// Validation details
		s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Validation attempted for %.1f seconds", m.dnsInfo.ValidationDuration.Seconds())))
		s.WriteString("\n\n")

		// Troubleshooting tips
		s.WriteString(m.styles.Bold.Render("Troubleshooting Tips:"))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("1. Verify you created an 'A' record for " + m.dnsInfo.Domain))
		} else {
			s.WriteString(m.styles.Normal.Render("1. Verify you created 'A' records for both unbind and unbind-registry subdomains"))
		}
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("2. Ensure all records point to your external IP: " + m.dnsInfo.ExternalIP))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("3. If using Cloudflare, unbind-registry must have proxy disabled (orange cloud off)"))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("4. DNS changes can take time to propagate (sometimes up to 24-48 hours)"))
		s.WriteString("\n\n")
	}

	// Options
	s.WriteString(m.styles.Bold.Render("Options:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Press Ctrl+r to retry the validation"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("2. Press Ctrl+e to change the domain"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("3. Press Enter to continue anyway (not recommended)"))
	s.WriteString("\n\n")

	// Warning
	s.WriteString(m.styles.Error.Render("Warning: "))
	s.WriteString(m.styles.Normal.Render("Continuing without valid DNS configuration may cause issues"))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+c to quit"))

	return s.String()
}

// updateDNSFailedState handles updates in the DNS failed state
func (m Model) updateDNSFailedState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			// Continue anyway despite DNS validation failure
			m.logChan <- "Continuing without validated DNS configuration..."
			return m, tea.Quit

		case "ctrl+r":
			// Add feedback message
			m.logChan <- "Retrying DNS validation..."

			// Retry DNS validation
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

		case "ctrl+e":
			// Go back to DNS configuration
			m.state = StateDNSConfig
			m.isLoading = false

			// Reset domain input
			m.domainInput.SetValue("")
			m.domainInput.Focus()

			return m, m.listenForLogs()
		}
	}

	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, m.listenForLogs()
}
