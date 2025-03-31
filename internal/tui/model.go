package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/errdefs"
	"github.com/unbindapp/unbind-installer/internal/network"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"github.com/unbindapp/unbind-installer/internal/pkgmanager"
)

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateWelcome ApplicationState = iota
	StateDebugLogs
	StateLoading
	StateRootDetection
	StateOSInfo
	StateInstallingPackages
	StateInstallComplete
	StateError
	StateDetectingIPs
	StateDNSConfig
	StateDNSValidation
	StateDNSSuccess
	StateDNSFailed
)

// Additional model fields for DNS setup
type dnsInfo struct {
	InternalIP         string
	ExternalIP         string
	Domain             string
	ValidationStarted  bool
	ValidationSuccess  bool
	CloudflareDetected bool
	TestingStartTime   time.Time
	ValidationDuration time.Duration
}

// Model represents the application state
type Model struct {
	previousState ApplicationState
	state         ApplicationState
	osInfo        *osinfo.OSInfo
	err           error
	spinner       spinner.Model
	width         int
	height        int
	isLoading     bool
	styles        Styles
	logMessages   []string
	logChan       chan string
	dnsInfo       *dnsInfo
	domainInput   textinput.Model
}

// NewModel initializes a new Model
func NewModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	// Use our new theme-based styles
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
	// We start with the welcome screen, so no commands needed initially
	return tea.Batch(
		m.listenForLogs(),
	)
}

// listenForLogs returns a command that listens for log messages
func (m Model) listenForLogs() tea.Cmd {
	return func() tea.Msg {
		msg := <-m.logChan
		return logMsg{msg}
	}
}

// detectOSInfo is a command that gets OS information
func detectOSInfo() tea.Msg {
	if os.Geteuid() != 0 {
		return errMsg{err: errdefs.ErrNotRoot}
	}

	info, err := osinfo.GetOSInfo()
	if err != nil {
		return errMsg{err}
	}
	return osInfoMsg{info}
}

// Messages
type osInfoMsg struct {
	info *osinfo.OSInfo
}

type errMsg struct {
	err error
}

type logMsg struct {
	message string
}

type installPackagesMsg struct{}

type installCompleteMsg struct{}

// DNS-related messages
type detectIPsMsg struct{}

type detectIPsCompleteMsg struct {
	ipInfo *network.IPInfo
}

type dnsValidationMsg struct{}

type autoAdvanceMsg struct{}

type dnsValidationCompleteMsg struct {
	success    bool
	cloudflare bool
}

type dnsValidationTimeoutMsg struct{}

type manualContinueMsg struct{}

// installRequiredPackages is a command that installs the required packages
func (m Model) installRequiredPackages() tea.Cmd {
	return func() tea.Msg {
		// Packages to install
		packages := []string{
			"curl",
			"wget",
			"ca-certificates",
			"apt-transport-https",
		}

		// Create a new apt installer
		installer := pkgmanager.NewAptInstaller(m.logChan)

		// Install the packages
		err := installer.InstallPackages(packages)
		if err != nil {
			return errMsg{err}
		}

		return installCompleteMsg{}
	}
}

// startDetectingIPs starts the IP detection process
func (m Model) startDetectingIPs() tea.Cmd {
	return func() tea.Msg {
		if m.dnsInfo == nil {
			m.dnsInfo = &dnsInfo{}
		}

		ipInfo, err := network.DetectIPs(func(msg string) {
			m.logChan <- msg
		})

		if err != nil {
			m.logChan <- "Error detecting IPs: " + err.Error()
		}

		return detectIPsCompleteMsg{ipInfo: ipInfo}
	}
}

// startDNSValidation starts the DNS validation process
func (m Model) startDNSValidation() tea.Cmd {
	return func() tea.Msg {
		if m.dnsInfo == nil || m.dnsInfo.Domain == "" {
			return errMsg{err: nil}
		}

		baseDomain := strings.Replace(m.dnsInfo.Domain, ".*", "", 1)

		// Log the validation attempt
		m.logChan <- "Starting DNS validation..."

		// Check for Cloudflare first
		cloudflare := network.CheckCloudflareProxy(baseDomain, func(msg string) {
			m.logChan <- msg
		})

		// If Cloudflare is detected, consider it successful
		if cloudflare {
			return dnsValidationCompleteMsg{
				success:    true,
				cloudflare: true,
			}
		}

		// Otherwise test the wildcard DNS configuration
		success := network.TestWildcardDNS(baseDomain, m.dnsInfo.ExternalIP, func(msg string) {
			m.logChan <- msg
		})

		return dnsValidationCompleteMsg{
			success:    success,
			cloudflare: false,
		}
	}
}

// dnsValidationTimeout creates a timeout for DNS validation
func dnsValidationTimeout(duration time.Duration) tea.Cmd {
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return dnsValidationTimeoutMsg{}
	})
}

// Update handles messages and user input
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle quit messages
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		}
	}

	// Handle text input for domain entry when in DNS config state
	if m.state == StateDNSConfig {
		var cmd tea.Cmd
		m.domainInput, cmd = m.domainInput.Update(msg)

		// Store the domain value
		if m.dnsInfo == nil {
			m.dnsInfo = &dnsInfo{}
		}
		m.dnsInfo.Domain = m.domainInput.Value()

		// If Enter was pressed, handle it separately
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "enter" {
			if m.dnsInfo.Domain != "" {
				m.state = StateDNSValidation
				m.isLoading = true
				m.dnsInfo.ValidationStarted = true
				m.dnsInfo.TestingStartTime = time.Now()

				return m, tea.Batch(
					m.spinner.Tick,
					m.startDNSValidation(),
					dnsValidationTimeout(30*time.Second),
					m.listenForLogs(),
				)
			}
		}

		cmds := []tea.Cmd{cmd, m.listenForLogs()}
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "d":
			// Toggle debug logs view
			if m.state != StateDebugLogs {
				m.previousState = m.state
				m.state = StateDebugLogs
			} else {
				m.state = m.previousState
			}
			return m, nil
		case "enter":
			if m.state == StateWelcome {
				// When Enter is pressed on the welcome screen,
				// transition to the loading state and start OS detection
				m.state = StateLoading
				m.isLoading = true
				return m, tea.Batch(
					m.spinner.Tick,
					detectOSInfo,
					m.listenForLogs(),
				)
			} else if m.state == StateDNSSuccess {
				// Continue to the next step after successful DNS validation
				return m, tea.Quit
			} else if m.state == StateDNSFailed {
				// Continue anyway despite DNS validation failure
				m.logChan <- "Continuing without validated DNS configuration..."
				return m, tea.Quit
			}
		case "r":
			if m.state == StateDNSFailed {
				// Add feedback message
				m.logChan <- "Retrying DNS validation..."

				// Retry DNS validation
				m.state = StateDNSValidation
				m.isLoading = true
				m.dnsInfo.ValidationStarted = true
				m.dnsInfo.TestingStartTime = time.Now()

				return m, tea.Batch(
					m.spinner.Tick,
					m.startDNSValidation(),
					dnsValidationTimeout(30*time.Second),
					m.listenForLogs(),
				)
			}
		case "c":
			if m.state == StateDNSFailed {
				// Go back to DNS configuration
				m.state = StateDNSConfig
				m.isLoading = false

				// Reset domain input
				m.domainInput.SetValue("")
				m.domainInput.Focus()

				return m, m.listenForLogs()
			}
		}

	case osInfoMsg:
		m.osInfo = msg.info
		m.state = StateOSInfo
		m.isLoading = false

		// Schedule automatic advancement after 3 seconds
		return m, tea.Batch(
			m.listenForLogs(),
			tea.Tick(1*time.Second, func(time.Time) tea.Msg {
				return installPackagesMsg{}
			}),
		)
	case errMsg:
		m.err = msg.err
		m.state = StateError
		m.isLoading = false
		return m, m.listenForLogs()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, tea.Batch(cmd, m.listenForLogs())

	case logMsg:
		m.logMessages = append(m.logMessages, msg.message)
		return m, m.listenForLogs()

	case installPackagesMsg:
		m.state = StateInstallingPackages
		m.isLoading = true
		return m, tea.Batch(
			m.spinner.Tick,
			m.installRequiredPackages(),
			m.listenForLogs(),
		)

	// Then in your Update function, modify the installCompleteMsg case:
	case installCompleteMsg:
		m.state = StateInstallComplete
		m.isLoading = false

		// Schedule automatic advancement after 3 seconds
		return m, tea.Batch(
			m.listenForLogs(),
			tea.Tick(1*time.Second, func(time.Time) tea.Msg {
				return autoAdvanceMsg{}
			}),
		)

	// And add a new case to handle the automatic advancement:
	case autoAdvanceMsg:
		if m.state == StateInstallComplete {
			// Start IP detection for DNS configuration
			m.state = StateDetectingIPs
			m.isLoading = true
			return m, tea.Batch(
				m.spinner.Tick,
				m.startDetectingIPs(),
				m.listenForLogs(),
			)
		}
		return m, nil

	case detectIPsCompleteMsg:
		m.state = StateDNSConfig
		m.isLoading = false

		// Initialize DNS info if needed
		if m.dnsInfo == nil {
			m.dnsInfo = &dnsInfo{}
		}

		// Store the detected IPs
		if msg.ipInfo != nil {
			m.dnsInfo.InternalIP = msg.ipInfo.InternalIP
			m.dnsInfo.ExternalIP = msg.ipInfo.ExternalIP
		}

		// Focus the domain input field
		m.domainInput.Focus()

		return m, m.listenForLogs()

	case dnsValidationCompleteMsg:
		m.dnsInfo.ValidationSuccess = msg.success
		m.dnsInfo.CloudflareDetected = msg.cloudflare
		m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)

		if msg.success {
			m.state = StateDNSSuccess
		} else {
			m.state = StateDNSFailed
		}

		m.isLoading = false
		return m, m.listenForLogs()

	case dnsValidationTimeoutMsg:
		if m.state == StateDNSValidation {
			m.dnsInfo.ValidationDuration = time.Since(m.dnsInfo.TestingStartTime)
			m.state = StateDNSFailed
			m.isLoading = false
			m.logChan <- "DNS validation timed out after 30 seconds"
		}
		return m, m.listenForLogs()
	}

	return m, m.listenForLogs()
}

// View renders the UI
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
	default:
		return viewWelcome(m)
	}
}

// UpdateDomain updates the domain in the DNS info
func (m *Model) UpdateDomain(domain string) {
	if m.dnsInfo == nil {
		m.dnsInfo = &dnsInfo{}
	}
	m.dnsInfo.Domain = domain
	m.domainInput.SetValue(domain)
}

// Add debug view
func viewDebugLogs(m Model) string {
	s := strings.Builder{}
	s.WriteString(m.styles.Title.Render("Debug Logs"))
	s.WriteString("\n\n")

	if len(m.logMessages) == 0 {
		s.WriteString("No logs available")
	} else {
		for i, msg := range m.logMessages {
			s.WriteString(fmt.Sprintf("%d: %s\n", i, msg))
		}
	}

	s.WriteString("\n")
	s.WriteString(m.styles.StatusBar.Render("Press 'd' to return, 'ctrl+c' to quit"))
	return s.String()
}
