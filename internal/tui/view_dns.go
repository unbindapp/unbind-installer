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
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

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
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

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
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

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

// initializeRegistryInput initializes the text input for registry domain entry
func initializeRegistryInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "registry.yourdomain.com"
	ti.Width = 30
	ti.Validate = func(s string) error {
		// Handle regular domain
		if !utils.IsDNSName(s) {
			return fmt.Errorf("%s is not a valid domain", s)
		}
		return nil
	}
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#009900"))
	return ti
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

// initializeUsernameInput initializes the text input for registry username entry
func initializeUsernameInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "username"
	ti.Width = 30
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#009900"))
	return ti
}

// initializePasswordInput initializes the text input for registry password entry
func initializePasswordInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "password"
	ti.Width = 30
	ti.EchoMode = textinput.EchoPassword
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#009900"))
	return ti
}

// viewRegistryTypeSelection shows a view for selecting registry type
func viewRegistryTypeSelection(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Select Registry Type for Unbind"))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Normal.Render("Unbind requires a container registry to store Docker images. You can:"))
	s.WriteString("\n\n")

	// Option 1: Self-hosted
	s.WriteString(m.styles.Bold.Render("1. Self-hosted Registry"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("   Allow Unbind to install a registry on your server"))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("   - Requires DNS name pointing to your server"))
	s.WriteString("\n\n")

	// Option 2: External registry
	s.WriteString(m.styles.Bold.Render("2. External Registry"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("   Use Docker Hub, GHCR, Quay, or another registry service"))
	s.WriteString("\n")
	s.WriteString(m.styles.Subtle.Render("   - Requires existing account credentials"))
	s.WriteString("\n\n")

	// Navigation hints
	s.WriteString(m.styles.Bold.Render("Navigation:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("1"))
	s.WriteString(m.styles.Normal.Render(" for Self-hosted Registry"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("2"))
	s.WriteString(m.styles.Normal.Render(" for External Registry"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Ctrl+b"))
	s.WriteString(m.styles.Normal.Render(" to go back to DNS configuration"))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

	return s.String()
}

// updateRegistryTypeSelectionState handles selection of registry type
func (m Model) updateRegistryTypeSelectionState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "1":
			// Self-hosted registry selected
			if m.dnsInfo == nil {
				m.dnsInfo = &dnsInfo{}
			}
			m.dnsInfo.RegistryType = RegistrySelfHosted
			m.dnsInfo.DisableLocalRegistry = false
			m.state = StateRegistryDomainInput
			m.registryInput.Focus()
			return m, m.listenForLogs()

		case "2":
			// External registry selected
			if m.dnsInfo == nil {
				m.dnsInfo = &dnsInfo{}
			}
			m.dnsInfo.RegistryType = RegistryExternal
			m.dnsInfo.DisableLocalRegistry = true
			m.state = StateExternalRegistryInput
			m.usernameInput.Focus()
			return m, m.listenForLogs()

		case "ctrl+b":
			// Go back to DNS configuration
			m.state = StateDNSConfig
			m.domainInput.Focus()
			return m, m.listenForLogs()
		}
	}

	if _, ok := msg.(tea.WindowSizeMsg); ok {
		// Handle window size changes
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// viewExternalRegistryInput shows the input screen for external registry credentials
func viewExternalRegistryInput(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Enter External Registry Credentials"))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Normal.Render("Please select a registry and enter your credentials:"))
	s.WriteString("\n\n")

	// Registry selection
	s.WriteString(m.styles.Bold.Render("Select Registry:"))
	s.WriteString("\n")

	// Docker Hub
	if m.selectedRegistry == 0 {
		s.WriteString(m.styles.SelectedOption.Render("→ [F1] Docker Hub (docker.io)"))
	} else {
		s.WriteString(m.styles.Normal.Render("  [F1] Docker Hub (docker.io)"))
	}
	s.WriteString("\n")

	// GitHub Container Registry
	if m.selectedRegistry == 1 {
		s.WriteString(m.styles.SelectedOption.Render("→ [F2] GitHub Container Registry (ghcr.io)"))
	} else {
		s.WriteString(m.styles.Normal.Render("  [F2] GitHub Container Registry (ghcr.io)"))
	}
	s.WriteString("\n")

	// Red Hat Quay
	if m.selectedRegistry == 2 {
		s.WriteString(m.styles.SelectedOption.Render("→ [F3] Red Hat Quay (quay.io)"))
	} else {
		s.WriteString(m.styles.Normal.Render("  [F3] Red Hat Quay (quay.io)"))
	}
	s.WriteString("\n")

	// Custom Registry
	if m.selectedRegistry == 3 {
		s.WriteString(m.styles.SelectedOption.Render("→ [F4] Custom Registry"))
	} else {
		s.WriteString(m.styles.Normal.Render("  [F4] Custom Registry"))
	}
	s.WriteString("\n\n")

	// Custom registry field if selected
	if m.selectedRegistry == 3 {
		customRegistryInput := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#009900")).
			Padding(0, 1).
			Render(fmt.Sprintf("Registry Host: %s", m.registryHostInput.View()))

		s.WriteString(customRegistryInput)
		s.WriteString("\n\n")
	}

	// Show current registry
	s.WriteString(m.styles.Bold.Render("Registry: "))
	var registryHost string
	switch m.selectedRegistry {
	case 0:
		registryHost = "docker.io"
	case 1:
		registryHost = "ghcr.io"
	case 2:
		registryHost = "quay.io"
	case 3:
		registryHost = m.registryHostInput.Value()
		if registryHost == "" {
			registryHost = "registry.example.com"
		}
	}
	s.WriteString(m.styles.Normal.Render(getRegistryDisplayName(registryHost)))
	s.WriteString("\n\n")

	// Username input field
	usernameInput := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#009900")).
		Padding(0, 1).
		Render(fmt.Sprintf("Username: %s", m.usernameInput.View()))

	s.WriteString(usernameInput)
	s.WriteString("\n\n")

	// Password input field
	passwordInput := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#009900")).
		Padding(0, 1).
		Render(fmt.Sprintf("Password: %s", m.passwordInput.View()))

	s.WriteString(passwordInput)
	s.WriteString("\n\n")

	s.WriteString(m.styles.Subtle.Render("We'll validate these credentials before proceeding"))
	s.WriteString("\n\n")

	// Navigation hints
	s.WriteString(m.styles.Bold.Render("Navigation:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Tab"))
	s.WriteString(m.styles.Normal.Render(" to switch between fields"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("F1"))
	s.WriteString(m.styles.Normal.Render(" through "))
	s.WriteString(m.styles.Key.Render("F4"))
	s.WriteString(m.styles.Normal.Render(" to select registry type"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Enter"))
	s.WriteString(m.styles.Normal.Render(" to validate credentials"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("• Press "))
	s.WriteString(m.styles.Key.Render("Ctrl+b"))
	s.WriteString(m.styles.Normal.Render(" to go back to registry type selection"))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press Ctrl+q to quit"))

	return s.String()
}

// updateExternalRegistryInputState handles updates in the external registry input state
func (m Model) updateExternalRegistryInputState(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Check if back button was pressed
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+b" {
		// Go back to registry type selection
		m.state = StateRegistryTypeSelection
		return m, m.listenForLogs()
	}

	// Check for registry selection keys
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "f1":
			// Docker Hub
			m.selectedRegistry = 0
		case "f2":
			// GitHub Container Registry
			m.selectedRegistry = 1
		case "f3":
			// Red Hat Quay
			m.selectedRegistry = 2
		case "f4":
			// Custom Registry
			m.selectedRegistry = 3
		}
	}

	// Check if tab was pressed
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "tab" {
		// Toggle focus between inputs
		if m.selectedRegistry == 3 {
			// Custom registry has 3 fields
			if m.registryHostInput.Focused() {
				m.registryHostInput.Blur()
				m.usernameInput.Focus()
			} else if m.usernameInput.Focused() {
				m.usernameInput.Blur()
				m.passwordInput.Focus()
			} else {
				m.passwordInput.Blur()
				m.registryHostInput.Focus()
			}
		} else {
			// Other registries have 2 fields
			if m.usernameInput.Focused() {
				m.usernameInput.Blur()
				m.passwordInput.Focus()
			} else {
				m.passwordInput.Blur()
				m.usernameInput.Focus()
			}
		}
		return m, nil
	}

	// Update the focused input
	if m.registryHostInput.Focused() {
		m.registryHostInput, cmd = m.registryHostInput.Update(msg)
	} else if m.usernameInput.Focused() {
		m.usernameInput, cmd = m.usernameInput.Update(msg)
	} else {
		m.passwordInput, cmd = m.passwordInput.Update(msg)
	}

	// Store the values
	if m.dnsInfo == nil {
		m.dnsInfo = &dnsInfo{}
	}
	m.dnsInfo.RegistryUsername = m.usernameInput.Value()
	m.dnsInfo.RegistryPassword = m.passwordInput.Value()

	// Set registry host based on selection
	switch m.selectedRegistry {
	case 0:
		m.dnsInfo.RegistryHost = "docker.io"
	case 1:
		m.dnsInfo.RegistryHost = "ghcr.io"
	case 2:
		m.dnsInfo.RegistryHost = "quay.io"
	case 3:
		m.dnsInfo.RegistryHost = m.registryHostInput.Value()
	}

	// If Enter was pressed, handle tabbing or submission
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
		if m.selectedRegistry == 3 {
			// Custom registry: 3 fields
			if m.registryHostInput.Focused() {
				// Move to username
				m.registryHostInput.Blur()
				m.usernameInput.Focus()
				return m, nil
			} else if m.usernameInput.Focused() {
				// Move to password
				m.usernameInput.Blur()
				m.passwordInput.Focus()
				return m, nil
			} else if m.passwordInput.Focused() {
				// Only submit if all fields are filled
				if m.dnsInfo.RegistryUsername != "" && m.dnsInfo.RegistryPassword != "" && m.dnsInfo.RegistryHost != "" {
					m.state = StateExternalRegistryValidation
					m.isLoading = true
					m.dnsInfo.TestingStartTime = time.Now()
					return m, tea.Batch(
						m.spinner.Tick,
						m.validateRegistryCredentials(),
						m.listenForLogs(),
					)
				}
				return m, m.listenForLogs()
			}
		} else {
			// Other registries: 2 fields
			if m.usernameInput.Focused() {
				// Move to password
				m.usernameInput.Blur()
				m.passwordInput.Focus()
				return m, nil
			} else if m.passwordInput.Focused() {
				if m.dnsInfo.RegistryUsername != "" && m.dnsInfo.RegistryPassword != "" {
					m.state = StateExternalRegistryValidation
					m.isLoading = true
					m.dnsInfo.TestingStartTime = time.Now()
					return m, tea.Batch(
						m.spinner.Tick,
						m.validateRegistryCredentials(),
						m.listenForLogs(),
					)
				}
				return m, m.listenForLogs()
			}
		}
	}

	if _, ok := msg.(tea.WindowSizeMsg); ok {
		// Handle window size changes
		return m, tea.Batch(cmd, m.listenForLogs())
	}

	// For any other message, keep updating the input and listening for logs
	return m, tea.Batch(cmd, m.listenForLogs())
}

// viewExternalRegistryValidation shows validation of external registry credentials
func viewExternalRegistryValidation(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Validating Registry Credentials..."))
	s.WriteString("\n\n")

	// Display what we're testing
	s.WriteString(m.styles.Bold.Render("Verifying:"))
	s.WriteString("\n")
	s.WriteString(fmt.Sprintf("  • Username: %s\n", m.styles.Normal.Render(m.dnsInfo.RegistryUsername)))
	s.WriteString(fmt.Sprintf("  • Registry: %s\n", m.styles.Normal.Render(getRegistryDisplayName(m.dnsInfo.RegistryHost))))
	s.WriteString("\n")

	// Process logs
	if len(m.logMessages) > 0 {
		s.WriteString(m.styles.Bold.Render("Connection logs:"))
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

// updateExternalRegistryValidationState handles updates in the external registry validation state
func (m Model) updateExternalRegistryValidationState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case registryValidationCompleteMsg:
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
			m.state = StateExternalRegistryInput
			m.isLoading = false
			m.logChan <- "Registry credentials validation failed. Please try again."

			// Focus username field
			m.usernameInput.Focus()

			return m, m.listenForLogs()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// getRegistryDisplayName returns a user-friendly display name for registry hosts
func getRegistryDisplayName(host string) string {
	switch host {
	case "docker.io":
		return "Docker Hub"
	case "ghcr.io":
		return "GitHub Container Registry"
	case "quay.io":
		return "Red Hat Quay"
	default:
		return host
	}
}
