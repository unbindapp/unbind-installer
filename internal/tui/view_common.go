package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/errdefs"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
)

// viewLoading shows the loading screen
func viewLoading(m Model) string {
	s := strings.Builder{}
	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Normal.Render("Detecting OS information..."))
	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// updateLoadingState handles updates in the loading state
func (m Model) updateLoadingState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case osInfoMsg:
		m.osInfo = msg.info
		m.state = StateOSInfo
		m.isLoading = false

		// Schedule automatic advancement after 1 second
		return m, tea.Batch(
			m.listenForLogs(),
			tea.Tick(1*time.Second, func(time.Time) tea.Msg {
				return installPackagesMsg{}
			}),
		)

	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// viewError shows the error screen
func viewError(m Model) string {
	s := strings.Builder{}
	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	if errors.Is(m.err, errdefs.ErrNotLinux) {
		s.WriteString(m.styles.Error.Render("Sorry, only Linux is supported for now!"))
	} else if errors.Is(m.err, errdefs.ErrInvalidArchitecture) {
		s.WriteString(m.styles.Error.Render("Sorry, this installer only supports amd64 and arm64 architectures!"))
		s.WriteString("\n")
		s.WriteString(m.styles.Subtle.Render("(this is a limitation of k3s)"))
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
	} else if errors.Is(m.err, errdefs.ErrK3sInstallFailed) {
		s.WriteString(m.styles.Error.Render("Sorry, the K3s installation failed!"))
		s.WriteString("\n")
		s.WriteString(m.styles.Subtle.Render("Please check the logs for more details by pressing 'd'."))
	} else if errors.Is(m.err, errdefs.ErrNetworkDetectionFailed) {
		s.WriteString(m.styles.Error.Render("Sorry, I couldn't detect your network interfaces!"))
		s.WriteString("\n")
		s.WriteString(m.styles.Subtle.Render("Please check the logs for more details by pressing 'd'."))
	} else if errors.Is(m.err, errdefs.ErrUnbindInstallFailed) {
		s.WriteString(m.styles.Error.Render("Sorry, the installation of unbind failed!"))
		s.WriteString("\n")
		s.WriteString(m.styles.Subtle.Render("Please check the logs for more details by pressing 'd'."))
	} else if m.err != nil {
		s.WriteString(m.styles.Error.Render(fmt.Sprintf("An error occurred: %v", m.err)))
	} else {
		s.WriteString(m.styles.Error.Render(fmt.Sprintf("Unknown error: %v", m.err)))
	}

	s.WriteString("\n\n")
	s.WriteString(m.styles.Subtle.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

// updateErrorState handles updates in the error state
func (m Model) updateErrorState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, m.listenForLogs()
}

// viewOSInfo shows the OS information
func viewOSInfo(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
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

	return s.String()
}

// updateOSInfoState handles updates in the OS info state
func (m Model) updateOSInfoState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case installPackagesMsg:
		m.state = StateCheckingSwap
		m.isLoading = true
		return m, tea.Batch(
			m.spinner.Tick,
			m.checkSwapCommand(),
			m.listenForLogs(),
		)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}
