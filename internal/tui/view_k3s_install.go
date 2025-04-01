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

// listenForProgress returns a command that listens for progress updates
func (self Model) listenForK3SProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.k3sProgressChan:
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

// viewInstallingK3S shows the K3S installation screen with progress tracking
func viewInstallingK3S(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing K3S..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// K3S Progress bar and status
	s.WriteString(m.styles.Bold.Render("K3S Installation:"))
	s.WriteString("\n")

	// Status indicator
	switch m.k3sProgress.Status {
	case "pending":
		s.WriteString("  [ ] ")
	case "installing":
		s.WriteString("  [*] ")
	case "completed":
		s.WriteString("  [✓] ")
	case "failed":
		s.WriteString("  [✗] ")
	}

	// K3S label
	s.WriteString(m.styles.Bold.Render("K3S"))
	s.WriteString(": ")

	// Current step description
	if m.k3sProgress.Description != "" {
		s.WriteString(m.styles.Subtle.Render(m.k3sProgress.Description))
		s.WriteString("\n      ")
	} else {
		s.WriteString("\n      ")
	}

	// Progress bar width calculation
	progressBarWidth := m.width - 40
	if progressBarWidth < 20 {
		progressBarWidth = 20
	}

	// Progress bar for installing K3S
	if m.k3sProgress.Status == "installing" {
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.k3sProgress.Progress))
	} else if m.k3sProgress.Status == "completed" {
		// Show completion progress and time
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(1.0))

		if !m.k3sProgress.StartTime.IsZero() && !m.k3sProgress.EndTime.IsZero() {
			duration := m.k3sProgress.EndTime.Sub(m.k3sProgress.StartTime).Round(time.Millisecond)
			s.WriteString(fmt.Sprintf(" (completed in %s)", duration))
		}
	} else if m.k3sProgress.Status == "failed" {
		// Show error message
		prog := m.styles.NewThemedProgress(progressBarWidth)
		s.WriteString(prog.ViewAs(m.k3sProgress.Progress))
		s.WriteString(" Failed")

		if m.k3sProgress.Error != nil {
			s.WriteString(fmt.Sprintf("\n      %s", m.styles.Error.Render(m.k3sProgress.Error.Error())))
		}
	}

	s.WriteString("\n\n")

	// Display installation steps (history)
	if len(m.k3sProgress.StepHistory) > 0 {
		s.WriteString(m.styles.Bold.Render("Installation steps:"))
		s.WriteString("\n")

		// Show last 3 steps
		startIdx := 0
		if len(m.k3sProgress.StepHistory) > 3 {
			startIdx = len(m.k3sProgress.StepHistory) - 3
		}

		for i, step := range m.k3sProgress.StepHistory[startIdx:] {
			s.WriteString(fmt.Sprintf("  %d. %s\n", startIdx+i+1, m.styles.Subtle.Render(step)))
		}
		s.WriteString("\n")
	}

	return s.String()
}

// updateK3SInstall updates the K3S installation state
func (self *Model) updateK3SInstall(msg k3s.K3SUpdateMessage) {
	self.k3sProgress.Status = msg.Status
	self.k3sProgress.Progress = msg.Progress

	// Save the description as the current step
	if msg.Description != "" && msg.Description != self.k3sProgress.Description {
		self.k3sProgress.Description = msg.Description

		// Add to steps history
		if !slices.Contains(self.k3sProgress.StepHistory, msg.Description) {
			self.k3sProgress.StepHistory = append(self.k3sProgress.StepHistory, msg.Description)
		}
	}

	self.k3sProgress.Error = msg.Error
}

// updateInstallingK3SState handles updates in the K3S installation state
func (m Model) updateInstallingK3SState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs(), m.listenForProgress())

	case k3s.K3SUpdateMessage:
		// Log that we received a progress update (for debugging)
		m.logMessages = append(m.logMessages,
			"K3S installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
				" - Status: "+string(msg.Status)+
				" - Step: "+msg.Description)

		// Update the K3S installation status
		m.updateK3SInstall(msg)

		// If installation completed successfully, send the completion message
		if msg.Status == "completed" {
			return m, func() tea.Msg {
				// Replace these with your actual values
				return k3sInstallCompleteMsg{
					kubeClient:          nil,                         // Your actual kubeClient
					kubeConfig:          "/etc/rancher/k3s/k3s.yaml", // Your actual kubeConfig
					dependenciesManager: nil,                         // Your actual dependenciesManager
				}
			}
		} else if msg.Status == "failed" {
			return m, func() tea.Msg {
				return errMsg{err: msg.Error}
			}
		}

		return m, tea.Batch(m.listenForLogs(), m.listenForK3SProgress())

	case k3sInstallCompleteMsg:
		// Install Cilium after K3S
		m.state = StateInstallingCilium
		m.isLoading = true
		m.kubeClient = msg.kubeClient
		m.kubeConfig = msg.kubeConfig
		m.dependenciesManager = msg.dependenciesManager
		return m, tea.Batch(
			m.spinner.Tick,
			m.installCilium(),
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
