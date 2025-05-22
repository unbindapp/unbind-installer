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

	// Always display installation steps section even if empty
	s.WriteString(m.styles.Bold.Render("Installation steps:"))
	s.WriteString("\n")

	if len(m.k3sProgress.StepHistory) > 0 {
		// Show all steps instead of just the last 3
		for i, step := range m.k3sProgress.StepHistory {
			s.WriteString(fmt.Sprintf("  %d. %s\n", i+1, m.styles.Subtle.Render(step)))
		}
	} else {
		// Show a placeholder if no steps are available yet
		s.WriteString("  Waiting for installation steps...\n")
	}
	s.WriteString("\n")

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
		return m.processStateUpdate(cmd)

	case k3s.K3SUpdateMessage:
		// Store the progress update in model
		m.k3sProgress = msg

		// Log only significant progress updates to reduce logging overhead
		if msg.Progress == 0 || msg.Progress >= 0.25 && msg.Progress < 0.26 ||
			msg.Progress >= 0.5 && msg.Progress < 0.51 || msg.Progress >= 0.75 && msg.Progress < 0.76 ||
			msg.Progress == 1.0 || msg.Status == "completed" || msg.Status == "failed" {
			m.logMessages = append(m.logMessages,
				"K3S installation progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100)+
					" - Status: "+string(msg.Status)+
					" - Step: "+msg.Description)
		}

		// Update the K3S installation status
		m.updateK3SInstall(msg)

		// If installation completed successfully or failed, send the appropriate message
		if msg.Status == "completed" {
			return m.processStateUpdate(func() tea.Msg {
				return k3sInstallCompleteMsg{
					kubeClient:      nil,                         // Your actual kubeClient
					kubeConfig:      "/etc/rancher/k3s/k3s.yaml", // Your actual kubeConfig
					unbindInstaller: nil,                         // Your actual unbindInstaller
				}
			})
		} else if msg.Status == "failed" {
			return m.processStateUpdate(func() tea.Msg {
				return errMsg{err: msg.Error}
			})
		}

		return m.processStateUpdate(nil)

	case k3sInstallCompleteMsg:
		// Install Unbind after K3S
		m.state = StateInstallingUnbind
		m.isLoading = true
		m.kubeClient = msg.kubeClient
		m.kubeConfig = msg.kubeConfig
		m.unbindInstaller = msg.unbindInstaller

		// Clear progress channel reference to prevent memory leaks
		m.k3sProgressChan = nil

		return m.processStateUpdate(
			m.spinner.Tick,
			m.installUnbind(),
		)

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
