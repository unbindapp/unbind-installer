package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
)

// Model represents the application state
type Model struct {
	// State management
	previousState ApplicationState
	state         ApplicationState

	// Core data
	osInfo *osinfo.OSInfo
	err    error

	// UI components
	spinner   spinner.Model
	width     int
	height    int
	isLoading bool
	styles    Styles

	// Logging
	logMessages []string
	logChan     chan string

	// Feature-specific data
	dnsInfo     *dnsInfo
	domainInput textinput.Model

	// You can add more feature-specific fields here as you add more stages
}

// NewModel initializes a new Model
func NewModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	// Use our theme-based styles
	theme := DefaultTheme()
	styles := NewStyles(theme)

	s.Style = styles.SpinnerStyle

	logChan := make(chan string, 100) // Buffer for log messages

	// Initialize domain input
	domainInput := initializeDomainInput()

	return Model{
		state:       StateWelcome,
		spinner:     s,
		isLoading:   false,
		styles:      styles,
		logMessages: []string{},
		logChan:     logChan,
		domainInput: domainInput,
	}
}

// Init is the Bubble Tea initialization function
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.listenForLogs(),
	)
}

// Update handles messages and user input
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle global key events first
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "d":
			if m.state != StateDNSConfig {
				// Toggle debug logs view
				if m.state != StateDebugLogs {
					m.previousState = m.state
					m.state = StateDebugLogs
				} else {
					m.state = m.previousState
				}
				return m, nil
			}
		}
	}

	// Process spinner ticks (needed for all loading states)
	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(tickMsg)
		return m, tea.Batch(cmd, m.listenForLogs())
	}

	// Process window size messages (applies to all states)
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
	}

	// Process log messages (applies to all states)
	if logMsg, ok := msg.(logMsg); ok {
		m.logMessages = append(m.logMessages, logMsg.message)
		return m, m.listenForLogs()
	}

	// Delegate to state-specific handlers
	switch m.state {
	case StateWelcome:
		return m.updateWelcomeState(msg)
	case StateLoading:
		return m.updateLoadingState(msg)
	case StateOSInfo:
		return m.updateOSInfoState(msg)
	case StateInstallingPackages:
		return m.updateInstallingPackagesState(msg)
	case StateInstallComplete:
		return m.updateInstallCompleteState(msg)
	case StateDetectingIPs:
		return m.updateDetectingIPsState(msg)
	case StateDNSConfig:
		return m.updateDNSConfigState(msg)
	case StateDNSValidation:
		return m.updateDNSValidationState(msg)
	case StateDNSSuccess:
		return m.updateDNSSuccessState(msg)
	case StateDNSFailed:
		return m.updateDNSFailedState(msg)
	case StateDebugLogs:
		return m.updateDebugLogsState(msg)
	case StateError:
		return m.updateErrorState(msg)
	default:
		return m, m.listenForLogs()
	}
}

// View delegates to the appropriate view function based on state
func (m Model) View() string {
	switch m.state {
	case StateWelcome:
		return viewWelcome(m)
	case StateLoading:
		return viewLoading(m)
	case StateError:
		return viewError(m)
	case StateOSInfo:
		return viewOSInfo(m)
	case StateInstallingPackages:
		return viewInstallingPackages(m)
	case StateInstallComplete:
		return viewInstallComplete(m)
	case StateDetectingIPs:
		return viewDetectingIPs(m)
	case StateDNSConfig:
		return viewDNSConfig(m)
	case StateDNSValidation:
		return viewDNSValidation(m)
	case StateDNSSuccess:
		return viewDNSSuccess(m)
	case StateDNSFailed:
		return viewDNSFailed(m)
	case StateDebugLogs:
		return viewDebugLogs(m)
	default:
		return viewWelcome(m)
	}
}

// Helper methods for state transitions and utility functions
func (m *Model) handleGlobalKeyEvents(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return tea.Quit
	case "d":
		// Toggle debug logs view
		if m.state != StateDebugLogs {
			m.previousState = m.state
			m.state = StateDebugLogs
		} else {
			m.state = m.previousState
		}
		return nil
	}
	return nil
}

// UpdateDomain updates the domain in the DNS info
func (m *Model) UpdateDomain(domain string) {
	if m.dnsInfo == nil {
		m.dnsInfo = &dnsInfo{}
	}
	m.dnsInfo.Domain = domain
	m.domainInput.SetValue(domain)
}
