package tui

import (
	"fmt"
	"slices"
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
	case "pending":
		s.WriteString("  [ ] ")
	case "installing":
		s.WriteString("  [*] ")
	case "completed":
		s.WriteString("  [✓] ")
	case "failed":
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
	if m.unbindProgress.Status == "installing" {
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.unbindProgress.Progress))
	} else if m.unbindProgress.Status == "completed" {
		// Show completion progress and time
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(1.0))

		if !m.unbindProgress.StartTime.IsZero() && !m.unbindProgress.EndTime.IsZero() {
			duration := m.unbindProgress.EndTime.Sub(m.unbindProgress.StartTime).Round(time.Millisecond)
			s.WriteString(fmt.Sprintf(" (completed in %s)", duration))
		}
	} else if m.unbindProgress.Status == "failed" {
		// Show error message
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.unbindProgress.Progress))
		s.WriteString(" Failed")

		if m.unbindProgress.Error != nil {
			s.WriteString(fmt.Sprintf("\n      %s", m.styles.Error.Render(m.unbindProgress.Error.Error())))
		}
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

// updateUnbindInstall updates the Unbind installation state
func (self *Model) updateUnbindInstall(msg installer.UnbindInstallUpdateMsg) {
	self.unbindProgress.Status = msg.Status
	self.unbindProgress.Progress = msg.Progress

	// Save the description as the current step
	if msg.Description != "" && msg.Description != self.unbindProgress.Description {
		self.unbindProgress.Description = msg.Description

		// Add to steps history
		if !slices.Contains(self.unbindProgress.StepHistory, msg.Description) {
			self.unbindProgress.StepHistory = append(self.unbindProgress.StepHistory, msg.Description)
		}
	}

	self.unbindProgress.Error = msg.Error
}

// updateInstallingUnbindState handles updates in the Unbind installation state
func (m Model) updateInstallingUnbindState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m.processStateUpdate(cmd)

	case installer.UnbindInstallUpdateMsg:
		// Store the progress update in model
		m.unbindProgress = msg

		// Log only significant progress updates to reduce logging overhead
		if msg.Progress == 0 || msg.Progress >= 0.25 && msg.Progress < 0.26 ||
			msg.Progress >= 0.5 && msg.Progress < 0.51 || msg.Progress >= 0.75 && msg.Progress < 0.76 ||
			msg.Progress == 1.0 || msg.Status == "completed" || msg.Status == "failed" {
			m.logMessages = append(m.logMessages,
				"Unbind installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
					" - Status: "+string(msg.Status)+
					" - Step: "+msg.Description)
		}

		// Update the Unbind installation status
		m.updateUnbindInstall(msg)

		// If installation completed successfully or failed, send the appropriate message
		if msg.Status == "completed" {
			return m.processStateUpdate(func() tea.Msg {
				return unbindInstallCompleteMsg{}
			})
		} else if msg.Status == "failed" {
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

		// Clear progress channel reference to prevent memory leaks
		m.unbindProgressChan = nil

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
