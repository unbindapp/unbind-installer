package tui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/installer"
	"github.com/unbindapp/unbind-installer/internal/k3s"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"k8s.io/client-go/dynamic"
)

// Model represents the application state
type Model struct {
	// State management
	state         ApplicationState
	showDebugLogs bool // New flag to indicate debug logs view

	// Core data
	osInfo                 *osinfo.OSInfo
	err                    error
	k3sUninstallScriptPath string
	availableDiskSpaceGB   float64
	swapSizeInput          textinput.Model
	swapSizeInputErr       error

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
	dnsInfo           *dnsInfo
	domainInput       textinput.Model
	registryInput     textinput.Model
	usernameInput     textinput.Model
	passwordInput     textinput.Model
	registryHostInput textinput.Model
	selectedRegistry  int // 0=docker.io, 1=ghcr.io, 2=quay.io, 3=custom

	// Kube client
	kubeConfig      string
	kubeClient      *dynamic.DynamicClient
	unbindInstaller *installer.UnbindInstaller

	// Progress statuses
	k3sProgressChan chan k3s.K3SUpdateMessage
	k3sProgress     k3s.K3SUpdateMessage

	// Helmfile progress
	unbindProgressChan chan installer.UnbindInstallUpdateMsg
	unbindProgress     installer.UnbindInstallUpdateMsg

	// Package install progress
	packageProgressChan chan packageInstallProgressMsg
	packageProgress     packageInstallProgressMsg
}

// NewModel initializes a new Model
func NewModel(version string) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	// Use our theme-based styles
	theme := DefaultTheme()
	styles := NewStyles(theme)

	s.Style = styles.SpinnerStyle

	logChan := make(chan string, 100) // Buffer for log messages

	progressChan := make(chan installer.UnbindInstallUpdateMsg)
	packageProgressChan := make(chan packageInstallProgressMsg, 10)

	// Initialize domain input
	domainInput := initializeDomainInput()

	// Initialize registry input
	registryInput := initializeRegistryInput()

	// Initialize username and password inputs
	usernameInput := initializeUsernameInput()
	passwordInput := initializePasswordInput()

	// Initialize registry host input
	registryHostInput := textinput.New()
	registryHostInput.Placeholder = "registry.example.com"
	registryHostInput.Width = 30
	registryHostInput.Prompt = ""

	// Initialize swap input
	swapInput := textinput.New()
	swapInput.Placeholder = "e.g., 4"
	swapInput.CharLimit = 4
	swapInput.Width = 10
	swapInput.Prompt = "Enter Swap Size (GB): "
	swapInput.Validate = func(s string) error {
		if s == "" {
			return nil // Allow empty initially
		}
		_, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a number")
		}
		return nil
	}

	return Model{
		state:              StateWelcome,
		showDebugLogs:      false, // Initialize debug logs flag
		spinner:            s,
		isLoading:          false,
		styles:             styles,
		logMessages:        []string{},
		logChan:            logChan,
		unbindProgressChan: progressChan,
		k3sProgressChan:    make(chan k3s.K3SUpdateMessage),
		k3sProgress: k3s.K3SUpdateMessage{
			Progress:    0.0,
			Status:      "pending",
			Description: "Initializing K3S installation",
		},
		domainInput:         domainInput,
		registryInput:       registryInput,
		usernameInput:       usernameInput,
		passwordInput:       passwordInput,
		registryHostInput:   registryHostInput,
		selectedRegistry:    0, // Default to Docker Hub
		swapSizeInput:       swapInput,
		packageProgressChan: packageProgressChan,
	}
}

// Init is the Bubble Tea initialization function
func (self Model) Init() tea.Cmd {
	return tea.Batch(
		self.listenForLogs(),
		self.listenForUnbindProgress(),
		self.listenForK3SProgress(),
		self.listenForPackageProgress(),
	)
}

// Update handles messages and user input
func (self Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle global key events first
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c", "ctrl+q", "esc":
			return self, tea.Quit
		case "d":
			if self.state != StateDNSConfig {
				// Toggle debug logs view
				self.showDebugLogs = !self.showDebugLogs
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
	var model tea.Model
	var cmd tea.Cmd

	switch self.state {
	case StateWelcome:
		model, cmd = self.updateWelcomeState(msg)
	case StateCheckK3s:
		model, cmd = self.updateCheckK3sState(msg)
	case StateConfirmUninstallK3s:
		model, cmd = self.updateConfirmUninstallK3sState(msg)
	case StateUninstallingK3s:
		model, cmd = self.updateUninstallingK3sState(msg)
	case StateLoading:
		model, cmd = self.updateLoadingState(msg)
	case StateOSInfo:
		model, cmd = self.updateOSInfoState(msg)
	case StateCheckingSwap:
		model, cmd = self.updateCheckingSwapState(msg)
	case StateConfirmCreateSwap:
		model, cmd = self.updateConfirmCreateSwapState(msg)
	case StateEnterSwapSize:
		model, cmd = self.updateEnterSwapSizeState(msg)
	case StateCreatingSwap:
		model, cmd = self.updateCreatingSwapState(msg)
	case StateSwapCreated:
		model, cmd = self.updateSwapCreatedState(msg)
	case StateInstallingPackages:
		model, cmd = self.updateInstallingPackagesState(msg)
	case StateInstallComplete:
		model, cmd = self.updateInstallCompleteState(msg)
	case StateDetectingIPs:
		model, cmd = self.updateDetectingIPsState(msg)
	case StateDNSConfig:
		model, cmd = self.updateDNSConfigState(msg)
	case StateDNSValidation:
		model, cmd = self.updateDNSValidationState(msg)
	case StateDNSSuccess:
		model, cmd = self.updateDNSSuccessState(msg)
	case StateDNSFailed:
		model, cmd = self.updateDNSFailedState(msg)
	case StateRegistryTypeSelection:
		model, cmd = self.updateRegistryTypeSelectionState(msg)
	case StateRegistryDomainInput:
		model, cmd = self.updateRegistryDomainInputState(msg)
	case StateRegistryDNSValidation:
		model, cmd = self.updateRegistryDNSValidationState(msg)
	case StateExternalRegistryInput:
		model, cmd = self.updateExternalRegistryInputState(msg)
	case StateExternalRegistryValidation:
		model, cmd = self.updateExternalRegistryValidationState(msg)
	case StateError:
		model, cmd = self.updateErrorState(msg)
	case StateInstallingK3S:
		model, cmd = self.updateInstallingK3SState(msg)
	case StateInstallingUnbind:
		model, cmd = self.updateInstallingUnbindState(msg)
	case StateInstallationComplete:
		model, cmd = self.updateInstallationCompleteState(msg)
	default:
		return self, self.listenForLogs()
	}

	// Type assert the model back to our Model type
	newModel, _ := model.(Model)

	// Preserve the debug logs flag
	newModel.showDebugLogs = self.showDebugLogs
	return newModel, cmd
}

// View delegates to the appropriate view function based on state
func (self Model) View() string {
	// If debug logs are shown, show that view regardless of state
	if self.showDebugLogs {
		return viewDebugLogs(self)
	}

	// Otherwise show the current state's view
	switch self.state {
	case StateWelcome:
		return viewWelcome(self)
	case StateCheckK3s:
		return viewCheckK3s(self)
	case StateConfirmUninstallK3s:
		return viewConfirmUninstallK3s(self)
	case StateUninstallingK3s:
		return viewUninstallingK3s(self)
	case StateLoading:
		return viewLoading(self)
	case StateError:
		return viewError(self)
	case StateOSInfo:
		return viewOSInfo(self)
	case StateCheckingSwap:
		return viewCheckingSwap(self)
	case StateConfirmCreateSwap:
		return viewConfirmCreateSwap(self)
	case StateEnterSwapSize:
		return viewEnterSwapSize(self)
	case StateCreatingSwap:
		return viewCreatingSwap(self)
	case StateSwapCreated:
		return viewSwapCreated(self)
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
	case StateInstallingK3S:
		return viewInstallingK3S(self)
	case StateInstallingUnbind:
		return viewInstallingUnbind(self)
	case StateInstallationComplete:
		return viewInstallationComplete(self)
	case StateRegistryTypeSelection:
		return viewRegistryTypeSelection(self)
	case StateRegistryDomainInput:
		return viewRegistryDomainInput(self)
	case StateRegistryDNSValidation:
		return viewRegistryDNSValidation(self)
	case StateExternalRegistryInput:
		return viewExternalRegistryInput(self)
	case StateExternalRegistryValidation:
		return viewExternalRegistryValidation(self)
	default:
		return viewWelcome(self)
	}
}

// listenForK3SProgress returns a command that listens for K3S progress messages
func (self Model) listenForK3SProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.k3sProgressChan:
			if !ok {
				// Channel closed
				return nil
			}
			self.k3sProgress = msg
			return msg
		default:
			// Don't block if no message is available
			return tickMsg{} // A dummy message to keep the command running
		}
	}
}

// listenForPackageProgress returns a command that listens for package installation progress
func (self Model) listenForPackageProgress() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.packageProgressChan:
			if !ok {
				// Channel closed
				return nil
			}
			self.packageProgress = msg
			return msg
		default:
			// Don't block if no message is available
			return tickMsg{} // A dummy message to keep the command running
		}
	}
}
