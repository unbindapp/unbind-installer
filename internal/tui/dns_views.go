package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// viewDetectingIPs shows the IP detection screen
func viewDetectingIPs(m Model) string {
	s := strings.Builder{}

	// Title
	s.WriteString(m.styles.Title.Render(getTitle()))
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
			s.WriteString(fmt.Sprintf("  %s\n", m.styles.Subtle.Render(msg)))
		}
	}

	// Status bar at the bottom
	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// viewDNSConfig shows the DNS configuration screen
func viewDNSConfig(m Model) string {
	s := strings.Builder{}

	// Title
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")

	// Instructions
	s.WriteString(m.styles.Bold.Render("Configure Wildcard DNS"))
	s.WriteString("\n\n")

	// Display detected IP addresses
	if m.dnsInfo != nil {
		if m.dnsInfo.InternalIP != "" {
			s.WriteString(m.styles.Bold.Render("Internal IP: "))
			s.WriteString(m.styles.Normal.Render(m.dnsInfo.InternalIP))
			s.WriteString("\n")
		}

		if m.dnsInfo.ExternalIP != "" {
			s.WriteString(m.styles.Bold.Render("External IP: "))
			s.WriteString(m.styles.Key.Render(m.dnsInfo.ExternalIP))
			s.WriteString("\n\n")
		}
	}

	// DNS Configuration instructions
	s.WriteString(m.styles.Normal.Render("For Unbind to work properly, you need to configure a wildcard DNS entry that points to your external IP address."))
	s.WriteString("\n\n")

	s.WriteString(m.styles.Bold.Render("How to configure:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Register a domain or use a subdomain of a domain you own"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("2. Create an 'A' record for *.yourdomain.com pointing to your external IP"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("3. Enter your domain below and press Enter to validate"))
	s.WriteString("\n\n")

	// Cloudflare note - REMOVED the DNS only message
	s.WriteString(m.styles.Bold.Render("Note: "))
	s.WriteString(m.styles.Normal.Render("This will also work with Cloudflare and other proxy services."))
	s.WriteString("\n\n")

	// Domain input field
	domainInput := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#009900")).
		Padding(0, 1).
		Render(fmt.Sprintf("Domain: %s", m.domainInput.View()))

	s.WriteString(domainInput)
	s.WriteString("\n\n")

	// Continue button
	continuePrompt := m.styles.HighlightButton.Render(" Press Enter to validate DNS ")
	s.WriteString(continuePrompt)
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// viewDNSValidation shows the DNS validation screen
func viewDNSValidation(m Model) string {
	s := strings.Builder{}

	// Title
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render(fmt.Sprintf("Validating DNS configuration for %s...", m.dnsInfo.Domain)))
	s.WriteString("\n\n")

	// Display what we're testing
	s.WriteString(m.styles.Bold.Render("Testing:"))
	s.WriteString("\n")
	s.WriteString(fmt.Sprintf("  • Domain: %s\n", m.styles.Normal.Render(m.dnsInfo.Domain)))
	s.WriteString(fmt.Sprintf("  • Wildcard: %s\n", m.styles.Normal.Render("*."+m.dnsInfo.Domain)))
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
			s.WriteString(fmt.Sprintf("  %s\n", m.styles.Subtle.Render(msg)))
		}
	}

	// Status bar at the bottom
	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
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
		s.WriteString(m.styles.Normal.Render("Your domain is configured with Cloudflare which works correctly with Unbind."))
	} else {
		s.WriteString(m.styles.Bold.Render("Domain: "))
		s.WriteString(m.styles.Normal.Render(m.dnsInfo.Domain))
		s.WriteString("\n")
		s.WriteString(m.styles.Bold.Render("Wildcard DNS: "))
		s.WriteString(m.styles.Normal.Render("*." + m.dnsInfo.Domain))
		s.WriteString("\n")
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

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// viewDNSFailed shows the DNS failure screen
func viewDNSFailed(m Model) string {
	s := strings.Builder{}

	// Title
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")

	// Error message
	s.WriteString(m.styles.Error.Render("! DNS Configuration Validation Failed"))
	s.WriteString("\n\n")

	// Failure details
	s.WriteString(m.styles.Bold.Render("Domain: "))
	s.WriteString(m.styles.Normal.Render(m.dnsInfo.Domain))
	s.WriteString("\n")
	s.WriteString(m.styles.Bold.Render("Expected to point to: "))
	s.WriteString(m.styles.Normal.Render(m.dnsInfo.ExternalIP))
	s.WriteString("\n\n")

	// Validation details
	s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Validation attempted for %.1f seconds", m.dnsInfo.ValidationDuration.Seconds())))
	s.WriteString("\n\n")

	// Troubleshooting tips
	s.WriteString(m.styles.Bold.Render("Troubleshooting Tips:"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("1. Verify you created an 'A' record with a wildcard (*) subdomain"))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("2. Ensure the A record points to your external IP: " + m.dnsInfo.ExternalIP))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("3. DNS changes can take time to propagate (sometimes up to 24-48 hours)"))
	s.WriteString("\n\n")

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

// initializeDomainInput initializes the text input for domain entry
func initializeDomainInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "*.yourdomain.com"
	ti.Focus()
	ti.Width = 30
	ti.Validate = func(s string) error {
		baseDomain := strings.Replace(s, "*.", "", 1)
		_, err := url.Parse(baseDomain)
		if err != nil {
			return fmt.Errorf("%s is not a valid URL", s)
		}
		return nil
	}
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#009900"))
	return ti
}
