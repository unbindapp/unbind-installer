package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/dependencies"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"k8s.io/client-go/dynamic"
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

	// Kube client
	kubeConfig          string
	kubeClient          *dynamic.DynamicClient
	dependenciesManager *dependencies.DependenciesManager

	// Dependencies after foundation is laid (helm charts, etc.)
	dependencies []Dependency
	progressChan chan dependencies.DependencyUpdateMsg
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

	progressChan := make(chan dependencies.DependencyUpdateMsg)

	// Initialize domain input
	domainInput := initializeDomainInput()

	return Model{
		state:        StateWelcome,
		spinner:      s,
		isLoading:    false,
		styles:       styles,
		logMessages:  []string{},
		logChan:      logChan,
		progressChan: progressChan,
		domainInput:  domainInput,
		dependencies: []Dependency{
			{
				Name:        "longhorn",
				Description: "Cloud native distributed storage",
				Status:      dependencies.StatusPending,
				Progress:    0.0,
			},
		},
	}
}

// Init is the Bubble Tea initialization function
func (self Model) Init() tea.Cmd {
	return tea.Batch(
		self.listenForLogs(),
		self.listenForProgress(),
	)
}

// Update handles messages and user input
func (self Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle global key events first
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c", "q", "esc":
			return self, tea.Quit
		case "d":
			if self.state != StateDNSConfig {
				// Toggle debug logs view
				if self.state != StateDebugLogs {
					self.previousState = self.state
					self.state = StateDebugLogs
				} else {
					self.state = self.previousState
				}
				return self, nil
			}
		}
	}

	// Process spinner ticks (needed for all loading states)
	if tickMsg, ok := msg.(spinner.TickMsg); ok {
		var cmd tea.Cmd
		self.spinner, cmd = self.spinner.Update(tickMsg)
		return self, tea.Batch(cmd, self.listenForLogs())
	}

	// Process window size messages (applies to all states)
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		self.width = sizeMsg.Width
		self.height = sizeMsg.Height
	}

	// Process log messages (applies to all states)
	if logMsg, ok := msg.(logMsg); ok {
		self.logMessages = append(self.logMessages, logMsg.message)
		return self, self.listenForLogs()
	}

	// Delegate to state-specific handlers
	switch self.state {
	case StateWelcome:
		return self.updateWelcomeState(msg)
	case StateLoading:
		return self.updateLoadingState(msg)
	case StateOSInfo:
		return self.updateOSInfoState(msg)
	case StateInstallingPackages:
		return self.updateInstallingPackagesState(msg)
	case StateInstallComplete:
		return self.updateInstallCompleteState(msg)
	case StateDetectingIPs:
		return self.updateDetectingIPsState(msg)
	case StateDNSConfig:
		return self.updateDNSConfigState(msg)
	case StateDNSValidation:
		return self.updateDNSValidationState(msg)
	case StateDNSSuccess:
		return self.updateDNSSuccessState(msg)
	case StateDNSFailed:
		return self.updateDNSFailedState(msg)
	case StateDebugLogs:
		return self.updateDebugLogsState(msg)
	case StateError:
		return self.updateErrorState(msg)
	case StateInstallingK3S:
		return self.updateInstallingK3SState(msg)
	case StateInstallingCilium:
		return self.updateInstallingCiliumState(msg)
	case StateInstallingDependencies:
		return self.updateInstallingDependenciesState(msg)
	default:
		return self, self.listenForLogs()
	}
}

// View delegates to the appropriate view function based on state
func (self Model) View() string {
	switch self.state {
	case StateWelcome:
		return viewWelcome(self)
	case StateLoading:
		return viewLoading(self)
	case StateError:
		return viewError(self)
	case StateOSInfo:
		return viewOSInfo(self)
	case StateInstallingPackages:
		return viewInstallingPackages(self)
	case StateInstallComplete:
		return viewInstallComplete(self)
	case StateDetectingIPs:
		return viewDetectingIPs(self)
	case StateDNSConfig:
		return viewDNSConfig(self)
	case StateDNSValidation:
		return viewDNSValidation(self)
	case StateDNSSuccess:
		return viewDNSSuccess(self)
	case StateDNSFailed:
		return viewDNSFailed(self)
	case StateDebugLogs:
		return viewDebugLogs(self)
	case StateInstallingK3S:
		return viewInstallingK3S(self)
	case StateInstallingCilium:
		return viewInstallingCilium(self)
	case StateInstallingDependencies:
		return viewInstallingDependencies(self)
	default:
		return viewWelcome(self)
	}
}

// UpdateDomain updates the domain in the DNS info
func (self *Model) UpdateDomain(domain string) {
	if self.dnsInfo == nil {
		self.dnsInfo = &dnsInfo{}
	}
	self.dnsInfo.Domain = domain
	self.domainInput.SetValue(domain)
}
