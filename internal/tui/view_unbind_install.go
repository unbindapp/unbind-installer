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

// listenForProgress returns a command that listens for progress updates
func (self Model) listenForUnbindProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.unbindProgressChan:
			if !ok {
				// Channel closed
				return nil
			}
			return msg
		default:
			// Don't block if no message is available
			return nil
		}
	}
}

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

	// Display installation steps (history)
	if len(m.unbindProgress.StepHistory) > 0 {
		s.WriteString(m.styles.Bold.Render("Installation steps:"))
		s.WriteString("\n")

		// Show last 3 steps
		startIdx := 0
		if len(m.unbindProgress.StepHistory) > 3 {
			startIdx = len(m.unbindProgress.StepHistory) - 3
		}

		for i, step := range m.unbindProgress.StepHistory[startIdx:] {
			s.WriteString(fmt.Sprintf("  %d. %s\n", startIdx+i+1, m.styles.Subtle.Render(step)))
		}
		s.WriteString("\n")
	}

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
		return m, tea.Batch(cmd, m.listenForLogs(), m.listenForUnbindProgress())

	case installer.UnbindInstallUpdateMsg:
		// Log that we received a progress update (for debugging)
		m.logMessages = append(m.logMessages,
			"Unbind installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
				" - Status: "+string(msg.Status)+
				" - Step: "+msg.Description)

		// Update the Unbind installation status
		m.updateUnbindInstall(msg)

		// If installation completed successfully, send the completion message
		if msg.Status == "completed" {
			return m, func() tea.Msg {
				return unbindInstallCompleteMsg{}
			}
		} else if msg.Status == "failed" {
			return m, func() tea.Msg {
				return errMsg{err: msg.Error}
			}
		}

		return m, tea.Batch(m.listenForLogs(), m.listenForUnbindProgress())

	case unbindInstallCompleteMsg:
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
		return m, tea.Batch(m.listenForLogs(), m.listenForUnbindProgress())
	}

	return m, tea.Batch(m.listenForLogs(), m.listenForUnbindProgress())
}
