package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/unbindapp/unbind-installer/internal/errdefs"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
)

// viewLoading shows the loading screen
func viewLoading(m Model) string {
	s := strings.Builder{}
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Normal.Render("Detecting OS information..."))
	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// viewError shows the error screen
func viewError(m Model) string {
	s := strings.Builder{}
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")

	if errors.Is(m.err, errdefs.ErrNotLinux) {
		s.WriteString(m.styles.Error.Render("Sorry, only Linux is supported for now!"))
	} else if errors.Is(m.err, errdefs.ErrUnsupportedDistribution) {
		s.WriteString(m.styles.Error.Render("Sorry, this distribution is not supported!"))
		supportedDistros := strings.Join(osinfo.AllSupportedDistros, ", ")
		s.WriteString("\n")
		s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Supported distributions: %s", supportedDistros)))
	} else if errors.Is(m.err, errdefs.ErrUnsupportedVersion) {
		s.WriteString(m.styles.Error.Render("Sorry, this version is not supported!"))
		if m.osInfo != nil {
			supportedVersions := osinfo.AllSupportedDistrosVersions[m.osInfo.Distribution]
			supportedVersionsStr := strings.Join(supportedVersions, ", ")
			s.WriteString("\n")
			s.WriteString(m.styles.Subtle.Render(fmt.Sprintf("Supported versions: %s", supportedVersionsStr)))
		}
	} else if errors.Is(m.err, errdefs.ErrDistributionDetectionFailed) {
		s.WriteString(m.styles.Error.Render("Sorry, I couldn't detect your Linux distribution!"))
	} else if errors.Is(m.err, errdefs.ErrNotRoot) {
		s.WriteString(m.styles.Error.Render("Sorry, this installer must be run with root privileges!"))
	} else {
		s.WriteString(m.styles.Error.Render(fmt.Sprintf("Unknown error: %v", m.err)))
	}

	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// viewOSInfo shows the OS information
func viewOSInfo(m Model) string {
	s := strings.Builder{}

	// Title
	s.WriteString(m.styles.Title.Render(getTitle()))
	s.WriteString("\n\n")

	// OS Pretty Name (if available)
	if m.osInfo.PrettyName != "" {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Distribution and Version
	if m.osInfo.Distribution != "" {
		s.WriteString(m.styles.Bold.Render("Distribution: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.Distribution))
		s.WriteString("\n")
	}

	if m.osInfo.Version != "" {
		s.WriteString(m.styles.Bold.Render("Version: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.Version))
		s.WriteString("\n")
	}

	// Architecture
	if m.osInfo.Architecture != "" {
		s.WriteString(m.styles.Bold.Render("Architecture: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.Architecture))
		s.WriteString("\n")
	}

	s.WriteString("\n")
	s.WriteString(m.styles.Success.Render("✓ Your system is compatible with Unbind!"))
	s.WriteString("\n\n")

	s.WriteString(m.styles.HighlightButton.Render(" Press Enter to begin installation "))
	s.WriteString("\n\n")

	// Status bar at the bottom
	s.WriteString(m.styles.StatusBar.Render("Press 'ctrl+c' to quit"))

	return s.String()
}
