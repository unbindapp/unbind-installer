package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/installer"
)

// viewInstallingUnbind shows the Unbind installation screen with progress tracking
func viewInstallingUnbind(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing Unbind and Dependencies..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Unbind Progress bar and status
	s.WriteString(m.styles.Bold.Render("Unbind Installation:"))
	s.WriteString("\n")

	// Status indicator
	switch m.unbindProgress.Status {
	case installer.StatusPending:
		s.WriteString("  [ ] ")
	case installer.StatusInstalling:
		s.WriteString("  [*] ")
	case installer.StatusCompleted:
		s.WriteString("  [✓] ")
	case installer.StatusFailed:
		s.WriteString("  [✗] ")
	}

	// Unbind label
	s.WriteString(m.styles.Bold.Render("Unbind"))
	s.WriteString(": ")

	// Current step description
	if m.unbindProgress.Description != "" {
		s.WriteString(m.styles.Subtle.Render(m.unbindProgress.Description))
		s.WriteString("\n      ")
	} else {
		s.WriteString("\n      ")
	}

	// Progress bar width calculation
	progressBarWidth := m.width - 40
	if progressBarWidth < 20 {
		progressBarWidth = 20
	}

	// Progress bar for installing Unbind
	if m.unbindProgress.Status == installer.StatusInstalling {
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.unbindProgress.Progress))
	} else if m.unbindProgress.Status == installer.StatusCompleted {
		// Show completion progress and time
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(1.0))

		if !m.unbindProgress.StartTime.IsZero() && !m.unbindProgress.EndTime.IsZero() {
			duration := m.unbindProgress.EndTime.Sub(m.unbindProgress.StartTime).Round(time.Millisecond)
			s.WriteString(fmt.Sprintf(" (completed in %s)", duration))
		}
	} else if m.unbindProgress.Status == installer.StatusFailed {
		// Show error message
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.unbindProgress.Progress))
		s.WriteString(" Failed")

		if m.unbindProgress.Error != nil {
			s.WriteString(fmt.Sprintf("\n      %s", m.styles.Error.Render(m.unbindProgress.Error.Error())))
		}
	} else {
		// Show pending progress bar (0%)
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(0.0))
	}

	s.WriteString("\n\n")

	// Always display installation steps section even if empty
	s.WriteString(m.styles.Bold.Render("Installation steps:"))
	s.WriteString("\n")

	if len(m.unbindProgress.StepHistory) > 0 {
		// Show all steps instead of just the last 3
		for i, step := range m.unbindProgress.StepHistory {
			s.WriteString(fmt.Sprintf("  %d. %s\n", i+1, m.styles.Subtle.Render(step)))
		}
	} else {
		// Show a placeholder if no steps are available yet
		s.WriteString("  Waiting for installation steps...\n")
	}
	s.WriteString("\n")

	return s.String()
}

// updateInstallingUnbindState handles updates in the Unbind installation state
func (m Model) updateInstallingUnbindState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m.processStateUpdate(cmd)

	case installer.UnbindInstallUpdateMsg:
		// Update the Unbind progress in the model
		m.unbindProgress = msg

		// Log only significant progress updates to reduce logging overhead
		if msg.Progress == 0 || msg.Progress >= 0.25 && msg.Progress < 0.26 ||
			msg.Progress >= 0.5 && msg.Progress < 0.51 || msg.Progress >= 0.75 && msg.Progress < 0.76 ||
			msg.Progress == 1.0 || msg.Status == installer.StatusCompleted || msg.Status == installer.StatusFailed {
			m.logMessages = append(m.logMessages,
				"Unbind installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
					" - Status: "+string(msg.Status)+
					" - Step: "+msg.Description)
		}

		// If installation completed successfully or failed, send the appropriate message
		if msg.Status == installer.StatusCompleted {
			return m.processStateUpdate(func() tea.Msg {
				return unbindInstallCompleteMsg{}
			})
		} else if msg.Status == installer.StatusFailed {
			return m.processStateUpdate(func() tea.Msg {
				return errMsg{err: msg.Error}
			})
		}

		return m.processStateUpdate(nil)

	case unbindInstallCompleteMsg:
		// Install management script with cluster IP
		if err := installer.InstallManagementScript(m.dnsInfo.InternalIP); err != nil {
			m.logMessages = append(m.logMessages, fmt.Sprintf("Warning: Failed to install management script: %v", err))
		}

		// Move to installation complete state
		m.state = StateInstallationComplete
		m.isLoading = false

		return m, m.listenForLogs()

	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m.processStateUpdate(nil)

	case nil:
		// Handle nil messages from optimized progress listener
		return m.processStateUpdate(nil)
	}

	return m.processStateUpdate(nil)
}
