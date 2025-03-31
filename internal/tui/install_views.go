package tui

import (
	"fmt"
	"strings"
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

	// Status bar at the bottom
	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
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
	s.WriteString(m.styles.Normal.Render("Now I will proceed to DNS configuration."))
	s.WriteString("\n\n")

	return s.String()
}
