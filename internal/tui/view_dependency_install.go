package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/dependencies"
)

// listenForProgress returns a command that listens for progress updates
func (self Model) listenForProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.progressChan:
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

// Dependency represents a dependency to be installed
type Dependency struct {
	Name        string
	Status      dependencies.DependencyStatus
	Progress    float64
	Description string
	Error       error
	StartTime   time.Time
	EndTime     time.Time
}

// UpdateDependency updates the status of a dependency
func (self *Model) UpdateDependency(name string, status dependencies.DependencyStatus, progress float64, err error) {
	for i, dep := range self.dependencies {
		if dep.Name == name {
			self.dependencies[i].Status = status
			self.dependencies[i].Progress = progress

			if status == dependencies.StatusCompleted && self.dependencies[i].EndTime.IsZero() {
				self.dependencies[i].EndTime = time.Now()
			} else if status == dependencies.StatusFailed {
				self.dependencies[i].Error = err
				self.dependencies[i].EndTime = time.Now()
			} else if status == dependencies.StatusInstalling && self.dependencies[i].StartTime.IsZero() {
				self.dependencies[i].StartTime = time.Now()
			}

			self.dependencies[i].Error = err
			break
		}
	}
}

// AllDependenciesComplete checks if all dependencies are completed
func (self Model) AllDependenciesComplete() bool {
	for _, dep := range self.dependencies {
		if dep.Status != dependencies.StatusCompleted {
			return false
		}
	}
	return true
}

// viewInstallingDependencies shows the dependencies installation screen
func viewInstallingDependencies(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Show current action
	s.WriteString(m.spinner.View())
	s.WriteString(" ")
	s.WriteString(m.styles.Bold.Render("Installing Dependencies..."))
	s.WriteString("\n\n")

	// OS info if available
	if m.osInfo != nil {
		s.WriteString(m.styles.Bold.Render("OS: "))
		s.WriteString(m.styles.Normal.Render(m.osInfo.PrettyName))
		s.WriteString("\n\n")
	}

	// Dependencies list with status and progress bars
	s.WriteString(m.styles.Bold.Render("Dependencies:"))
	s.WriteString("\n")

	progressBarWidth := m.width - 40
	if progressBarWidth < 20 {
		progressBarWidth = 20
	}

	for _, dep := range m.dependencies {
		// Status indicator
		switch dep.Status {
		case dependencies.StatusPending:
			s.WriteString("  [ ] ")
		case dependencies.StatusInstalling:
			s.WriteString("  [*] ")
		case dependencies.StatusCompleted:
			s.WriteString("  [✓] ")
		case dependencies.StatusFailed:
			s.WriteString("  [✗] ")
		}

		// Dependency name
		s.WriteString(m.styles.Bold.Render(dep.Name))
		s.WriteString(": ")

		// Description
		if dep.Description != "" {
			s.WriteString(m.styles.Subtle.Render(dep.Description))
			s.WriteString("\n      ")
		} else {
			s.WriteString("\n      ")
		}

		// Progress bar for installing dependencies
		if dep.Status == dependencies.StatusInstalling {
			prog := progress.New(progress.WithWidth(progressBarWidth))
			s.WriteString(prog.ViewAs(dep.Progress))
		} else if dep.Status == dependencies.StatusCompleted {
			// Show completion time
			prog := progress.New(progress.WithWidth(progressBarWidth))
			s.WriteString(prog.ViewAs(1.0))

			if !dep.StartTime.IsZero() && !dep.EndTime.IsZero() {
				duration := dep.EndTime.Sub(dep.StartTime).Round(time.Millisecond)
				s.WriteString(fmt.Sprintf(" (%s)", duration))
			}
		} else if dep.Status == dependencies.StatusFailed {
			// Show error message
			prog := progress.New(progress.WithWidth(progressBarWidth))
			s.WriteString(prog.ViewAs(dep.Progress))
			s.WriteString(" Failed")

			if dep.Error != nil {
				s.WriteString(fmt.Sprintf("\n      %s", m.styles.Error.Render(dep.Error.Error())))
			}
		}

		s.WriteString("\n\n")
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
			// Truncate the message if it's too long
			const maxLength = 80 // Reasonable terminal width
			displayMsg := msg
			if len(msg) > maxLength {
				displayMsg = msg[:maxLength-3] + "..."
			}
			s.WriteString(fmt.Sprintf(" %s\n", m.styles.Subtle.Render(displayMsg)))
		}
	}

	return s.String()
}

// updateInstallingDependenciesState handles updates in the dependencies installation state
func (self Model) updateInstallingDependenciesState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		self.spinner, cmd = self.spinner.Update(msg)
		return self, tea.Batch(cmd, self.listenForLogs(), self.listenForProgress())

	case dependencies.DependencyUpdateMsg:
		// Log that we received a progress update (for debugging)
		self.logMessages = append(self.logMessages,
			"Received progress update for "+msg.Name+
				" - Status: "+string(msg.Status)+
				" - Progress: "+fmt.Sprintf("%.1f%%", msg.Progress*100))

		// Update the dependency status
		self.UpdateDependency(msg.Name, msg.Status, msg.Progress, msg.Error)

		// Check if all dependencies are installed
		if self.AllDependenciesComplete() {
			return self, func() tea.Msg {
				return dependencyInstallCompleteMsg{}
			}
		}
		return self, tea.Batch(self.listenForLogs(), self.listenForProgress())

	case dependencyInstallCompleteMsg:
		// Move to the next state after all dependencies are installed
		// You can decide which state to go to next, depending on your flow
		// self.state = StateNextStep
		// self.isLoading = false
		return self, tea.Batch(
			self.listenForLogs(),
			self.listenForProgress(),
		)

	case errMsg:
		self.err = msg.err
		self.state = StateError
		self.isLoading = false
		return self, self.listenForLogs()

	case tea.WindowSizeMsg:
		self.width = msg.Width
		self.height = msg.Height
		return self, tea.Batch(self.listenForLogs(), self.listenForProgress())
	}

	return self, tea.Batch(self.listenForLogs(), self.listenForProgress())
}
