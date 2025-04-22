package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func viewCheckK3s(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner()) // Assuming getBanner() is available
	s.WriteString("\n\n")

	// Spinner and Action Text
	if m.isLoading {
		s.WriteString(m.spinner.View())
		s.WriteString(" ")
	}
	s.WriteString(m.styles.Bold.Render("Checking for existing K3s installation..."))
	s.WriteString("\n\n")

	// Footer / Quit message
	s.WriteString(m.styles.Subtle.Render("Press 'ctrl+c' to quit"))

	return s.String()
}

func viewConfirmUninstallK3s(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Title using Warning or Error style
	title := m.styles.Error.Render("! Existing K3s Installation Found!") // Using Warning style
	s.WriteString(title)
	s.WriteString("\n\n")

	// Description
	s.WriteString(m.styles.Normal.Render("An existing K3s installation (or remnants) was detected."))
	s.WriteString("\n")
	s.WriteString(m.styles.Normal.Render("To ensure a clean setup for unbind, it's recommended to uninstall the existing K3s first."))
	s.WriteString("\n\n")

	// Question
	s.WriteString(m.styles.Bold.Render("Do you want to run the K3s uninstall script?"))
	s.WriteString("\n\n")

	// Buttons
	// Center the buttons horizontally
	yesButton := m.styles.HighlightButton.Render(" Yes (y) ")
	noButton := m.styles.Subtle.Render(" No (n - Quit) ") // Assuming SubtleButton exists or use Subtle
	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, yesButton, "  ", noButton)

	s.WriteString(buttonRow)
	s.WriteString("\n\n")

	// Instructions / Footer
	s.WriteString(m.styles.Subtle.Render("Press 'y' to uninstall and continue, or 'n'/'q'/'esc'/'ctrl+c' to quit."))

	return s.String()
}

func viewUninstallingK3s(m Model) string {
	s := strings.Builder{}

	// Banner
	s.WriteString(getBanner())
	s.WriteString("\n\n")

	// Spinner and Action Text
	if m.isLoading {
		s.WriteString(m.spinner.View())
		s.WriteString(" ")
	}
	s.WriteString(m.styles.Bold.Render("Uninstalling existing K3s installation..."))
	s.WriteString("\n\n")

	// Footer / Quit message
	s.WriteString(m.styles.Subtle.Render("Uninstall process started. Pressing 'ctrl+c' will attempt to quit, but the script might continue running in the background."))

	return s.String()
}

func (m Model) updateCheckK3sState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case k3sCheckResultMsg:
		m.isLoading = false // Stop loading spinner for this phase
		if msg.err != nil {
			m.err = msg.err
			m.state = StateError
			return m, m.listenForLogs()
		}

		if msg.checkResult.IsInstalled {
			// K3s found, ask for confirmation
			m.state = StateConfirmUninstallK3s
			m.k3sUninstallScriptPath = msg.checkResult.UninstallScript
		} else {
			// K3s not found, proceed to OS detection
			m.state = StateLoading // Re-use loading state for OS info
			m.isLoading = true
			return m, tea.Batch(m.spinner.Tick, detectOSInfo, m.listenForLogs())
		}
		return m, m.listenForLogs()

	case errMsg: // Handle potential errors from checkK3sCommand (like not root)
		m.isLoading = false
		m.err = msg.err
		m.state = StateError
		return m, m.listenForLogs()

	case spinner.TickMsg: // Handle spinner if still loading
		var cmd tea.Cmd
		if m.isLoading {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil // Ignore tick if not loading

	case tea.KeyMsg: // Handle quit keys during check
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		}
	}
	return m, m.listenForLogs() // Keep listening for logs
}

func (m Model) updateConfirmUninstallK3sState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch strings.ToLower(keyMsg.String()) {
		case "y":
			// User wants to uninstall
			m.state = StateUninstallingK3s
			m.isLoading = true // Show spinner during uninstall
			// Ensure k3sUninstallScriptPath was set in the previous step
			if m.k3sUninstallScriptPath == "" {
				// Handle error: path wasn't stored or found
				m.err = fmt.Errorf("internal error: K3s uninstall path not found")
				m.state = StateError
				return m, m.listenForLogs()
			}
			return m, tea.Batch(
				m.spinner.Tick,
				m.uninstallK3sCommand(m.k3sUninstallScriptPath), // Start uninstall
				m.listenForLogs(),
			)
		case "n", "ctrl+c", "q", "esc":
			// User wants to quit
			return m, tea.Quit
		}
	}
	return m, m.listenForLogs()
}

func (m Model) updateUninstallingK3sState(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case k3sUninstallCompleteMsg:
		m.isLoading = false // Stop spinner
		if msg.err != nil {
			// Uninstall failed
			m.err = msg.err // Display the specific uninstall error
			m.state = StateError
			return m, m.listenForLogs()
		}
		// Uninstall succeeded, proceed to OS detection
		m.state = StateLoading // Re-use loading state for OS info
		m.isLoading = true
		return m, tea.Batch(m.spinner.Tick, detectOSInfo, m.listenForLogs())

	case errMsg: // Handle potential errors during uninstall command setup
		m.isLoading = false
		m.err = msg.err
		m.state = StateError
		return m, m.listenForLogs()

	case spinner.TickMsg: // Handle spinner
		var cmd tea.Cmd
		if m.isLoading {
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg: // Handle quit keys during uninstall
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		}
	}
	return m, m.listenForLogs()
}
