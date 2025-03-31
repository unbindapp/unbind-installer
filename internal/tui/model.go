package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/errdefs"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"github.com/unbindapp/unbind-installer/internal/pkgmanager"
)

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateWelcome ApplicationState = iota
	StateLoading
	StateRootDetection
	StateOSInfo
	StateInstallingPackages
	StateInstallComplete
	StateError
)

// Model represents the application state
type Model struct {
	state       ApplicationState
	osInfo      *osinfo.OSInfo
	err         error
	spinner     spinner.Model
	width       int
	height      int
	isLoading   bool
	styles      Styles
	logMessages []string
	logChan     chan string
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

	return Model{
		state:       StateWelcome,
		spinner:     s,
		isLoading:   false,
		styles:      styles,
		logMessages: []string{},
		logChan:     logChan,
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

// Update handles messages and user input
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
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
			} else if m.state == StateOSInfo {
				// When Enter is pressed on the OS info screen,
				// start installing packages
				m.state = StateInstallingPackages
				m.isLoading = true
				return m, tea.Batch(
					m.spinner.Tick,
					m.installRequiredPackages(),
					m.listenForLogs(),
				)
			} else if m.state == StateInstallComplete {
				// When Enter is pressed on the install complete screen, exit
				return m, tea.Quit
			}
		}

	case osInfoMsg:
		m.osInfo = msg.info
		m.state = StateOSInfo
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

	case installCompleteMsg:
		m.state = StateInstallComplete
		m.isLoading = false
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
	default:
		return viewWelcome(m)
	}
}
