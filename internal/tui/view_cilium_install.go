package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/k3s"
)

// viewInstallingCilium shows the Cilium installation screen with progress tracking
func viewInstallingCilium(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing Cilium (k3s Networking Layer)..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Cilium Progress bar and status
	s.WriteString(m.styles.Bold.Render("Cilium Installation:"))
	s.WriteString("\n")

	// Status indicator
	switch m.ciliumProgress.Status {
	case "pending":
		s.WriteString("  [ ] ")
	case "installing":
		s.WriteString("  [*] ")
	case "completed":
		s.WriteString("  [✓] ")
	case "failed":
		s.WriteString("  [✗] ")
	}

	// Cilium label
	s.WriteString(m.styles.Bold.Render("Cilium"))
	s.WriteString(": ")

	// Current step description
	if m.ciliumProgress.Description != "" {
		s.WriteString(m.styles.Subtle.Render(m.ciliumProgress.Description))
		s.WriteString("\n      ")
	} else {
		s.WriteString("\n      ")
	}

	// Progress bar width calculation
	progressBarWidth := m.width - 40
	if progressBarWidth < 20 {
		progressBarWidth = 20
	}

	// Progress bar for installing Cilium
	if m.ciliumProgress.Status == "installing" {
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.ciliumProgress.Progress))
	} else if m.ciliumProgress.Status == "completed" {
		// Show completion progress and time
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(1.0))

		if !m.ciliumProgress.StartTime.IsZero() && !m.ciliumProgress.EndTime.IsZero() {
			duration := m.ciliumProgress.EndTime.Sub(m.ciliumProgress.StartTime).Round(time.Millisecond)
			s.WriteString(fmt.Sprintf(" (completed in %s)", duration))
		}
	} else if m.ciliumProgress.Status == "failed" {
		// Show error message
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.ciliumProgress.Progress))
		s.WriteString(" Failed")

		if m.ciliumProgress.Error != nil {
			s.WriteString(fmt.Sprintf("\n      %s", m.styles.Error.Render(m.ciliumProgress.Error.Error())))
		}
	}

	s.WriteString("\n\n")

	// Display installation steps (history)
	if len(m.ciliumProgress.StepHistory) > 0 {
		s.WriteString(m.styles.Bold.Render("Installation steps:"))
		s.WriteString("\n")

		// Show last 3 steps
		startIdx := 0
		if len(m.ciliumProgress.StepHistory) > 3 {
			startIdx = len(m.ciliumProgress.StepHistory) - 3
		}

		for i, step := range m.ciliumProgress.StepHistory[startIdx:] {
			s.WriteString(fmt.Sprintf("  %d. %s\n", startIdx+i+1, m.styles.Subtle.Render(step)))
		}
		s.WriteString("\n")
	}

	return s.String()
}

// updateCiliumInstall updates the Cilium installation state
func (self *Model) updateCiliumInstall(msg k3s.K3SUpdateMessage) {
	self.ciliumProgress.Status = msg.Status
	self.ciliumProgress.Progress = msg.Progress

	// Save the description as the current step
	if msg.Description != "" && msg.Description != self.ciliumProgress.Description {
		self.ciliumProgress.Description = msg.Description

		// Add to steps history
		if !slices.Contains(self.ciliumProgress.StepHistory, msg.Description) {
			self.ciliumProgress.StepHistory = append(self.ciliumProgress.StepHistory, msg.Description)
		}
	}

	self.ciliumProgress.Error = msg.Error
}

// updateInstallingCiliumState handles updates in the Cilium installation state
func (m Model) updateInstallingCiliumState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs(), m.listenForK3SProgress())

	case k3s.K3SUpdateMessage:
		// Log that we received a progress update (for debugging)
		m.logMessages = append(m.logMessages,
			"Cilium installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
				" - Status: "+string(msg.Status)+
				" - Step: "+msg.Description)

		// Update the Cilium installation status
		m.updateCiliumInstall(msg)

		// If installation completed successfully, send the completion message
		if msg.Status == "completed" {
			return m, func() tea.Msg {
				return ciliumInstallCompleteMsg{}
			}
		} else if msg.Status == "failed" {
			return m, func() tea.Msg {
				return errMsg{err: msg.Error}
			}
		}

		return m, tea.Batch(m.listenForLogs(), m.listenForK3SProgress())

	case ciliumInstallCompleteMsg:
		// Move to next state after Cilium is installed
		m.state = StateInstallingUnbind
		m.isLoading = true
		return m, tea.Batch(
			m.spinner.Tick,
			m.installUnbind(),
			m.listenForLogs(),
		)

	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.Batch(m.listenForLogs(), m.listenForK3SProgress())
	}

	return m, tea.Batch(m.listenForLogs(), m.listenForK3SProgress())
}
