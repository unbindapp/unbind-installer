package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/unbindapp/unbind-installer/internal/utils"
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

// viewDNSConfig shows the DNS configuration screen
func viewDNSConfig(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Configure DNS"))
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

	s.WriteString(m.styles.Bold.Render("Option 1: Create a wildcard A record"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Create an 'A' record for *.yourdomain.com → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render(" If using Cloudflare, you still need a separate unbind-registry A record with proxy disabled"))
	s.WriteString("\n\n")
	s.WriteString(m.styles.Bold.Render("Option 2: Create two separate A records"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Create an 'A' record for unbind.yourdomain.com → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("2. Create an 'A' record for unbind-registry.yourdomain.com → " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render(" If using Cloudflare, unbind-registry must have proxy disabled (orange cloud off)"))
	s.WriteString("\n")
	s.WriteString(m.styles.Warning.Render(" Note: Using separate A records will disable automatic domain generation for unbind services"))
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
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

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
			isWildcard := strings.HasPrefix(domain, "*.")

			if isWildcard {
				// Wildcard domain
				baseDomain := strings.TrimPrefix(domain, "*.")
				m.dnsInfo.IsWildcard = true
				m.dnsInfo.UnbindDomain = "unbind." + baseDomain
				m.dnsInfo.RegistryDomain = "unbind-registry." + baseDomain
			} else {
				// Regular domain - derive the subdomains
				m.dnsInfo.IsWildcard = false
				m.dnsInfo.UnbindDomain = "unbind." + domain
				m.dnsInfo.RegistryDomain = "unbind-registry." + domain
			}

			m.state = StateDNSValidation
			m.isLoading = true
			m.dnsInfo.ValidationStarted = true
			m.dnsInfo.TestingStartTime = time.Now()

			return m, tea.Batch(
				m.spinner.Tick,
				m.startDNSValidation(),
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
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

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
			m.state = StateDNSSuccess
			m.isLoading = false

			// Schedule automatic advancement after 3 seconds
			return m, tea.Batch(
				m.listenForLogs(),
				tea.Tick(1*time.Second, func(time.Time) tea.Msg {
					return autoAdvanceMsg{}
				}),
			)
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
	s.WriteString(m.styles.Success.Render("✓ DNS Configuration Successful!"))
	s.WriteString("\n\n")

	// Success details
	if m.dnsInfo.CloudflareDetected {
		s.WriteString(m.styles.Bold.Render("Cloudflare detected: "))
		s.WriteString(m.styles.Success.Render("Yes"))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("Your wildcard domain is configured correctly with Cloudflare."))
		} else {
			s.WriteString(m.styles.Normal.Render("Your domains are configured correctly with Cloudflare."))
		}
	} else {
		s.WriteString(m.styles.Bold.Render("Configured domains:"))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.UnbindDomain))
		s.WriteString("\n")
		s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.RegistryDomain))
		s.WriteString("\n")
		if m.dnsInfo.IsWildcard {
			s.WriteString(m.styles.Normal.Render("  • " + m.dnsInfo.Domain + " (wildcard)"))
			s.WriteString("\n")
		}
		s.WriteString(m.styles.Bold.Render("Points to: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.ExternalIP))
	}

	// Validation details
	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Validation completed in %.1f seconds", m.dnsInfo.ValidationDuration.Seconds())))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Normal.Render("Your DNS configuration is working correctly and Unbind can proceed with installation."))
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
	s.WriteString(m.styles.Normal.Render("1. Press 'r' to retry the validation"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("2. Press 'c' to change the domain"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("3. Press Enter to continue anyway (not recommended)"))
	s.WriteString("\n\n")

	// Warning
	s.WriteString(m.styles.Error.Render("Warning: "))
	s.WriteString(m.styles.Normal.Render("Continuing without valid DNS configuration may cause issues"))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

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

		case "r":
			// Add feedback message
			m.logChan <- "Retrying DNS validation..."

			// Retry DNS validation
			m.state = StateDNSValidation
			m.isLoading = true
			m.dnsInfo.ValidationStarted = true
			m.dnsInfo.TestingStartTime = time.Now()

			return m, tea.Batch(
				m.spinner.Tick,
				m.startDNSValidation(),
				dnsValidationTimeout(30*time.Second),
				m.listenForLogs(),
			)

		case "c":
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

// initializeDomainInput initializes the text input for domain entry
func initializeDomainInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "yourdomain.com"
	ti.Focus()
	ti.Width = 30
	ti.Validate = func(s string) error {
		// Handle wildcard domain
		if strings.HasPrefix(s, "*.") {
			baseDomain := strings.TrimPrefix(s, "*.")
			if !utils.IsDNSName(baseDomain) {
				return fmt.Errorf("%s is not a valid domain", baseDomain)
			}
			return nil
		}

		// Handle regular domain
		if !utils.IsDNSName(s) {
			return fmt.Errorf("%s is not a valid domain", s)
		}
		return nil
	}
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#009900"))
	return ti
}
