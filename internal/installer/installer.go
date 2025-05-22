package installer

import (
	"context"
	"fmt"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type UnbindInstaller struct {
	progressChan   chan<- UnbindInstallUpdateMsg
	kubeClient     *kubernetes.Clientset
	LogChan        chan<- string
	helmEnv        *cli.EnvSettings
	state          map[string]*dependencyState
	kubeConfigPath string
}

// dependencyState tracks status info for each component
type dependencyState struct {
	name        string
	startTime   time.Time
	endTime     time.Time
	status      InstallerStatus
	progress    float64
	description string
	error       error
	stepHistory []string // History of steps executed
}

// InstallationStep represents a single installation task
type InstallationStep struct {
	Description string
	Progress    float64
	Action      func(context.Context) error
}

func NewUnbindInstaller(kubeConfig string, logChan chan<- string, progressChan chan<- UnbindInstallUpdateMsg) (*UnbindInstaller, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
	if err != nil {
		logChan <- "Error creating kubeconfig: " + err.Error()
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		logChan <- "Error creating Kubernetes client: " + err.Error()
		return nil, err
	}

	return &UnbindInstaller{
		progressChan:   progressChan,
		kubeConfigPath: kubeConfig,
		kubeClient:     clientset,
		LogChan:        logChan,
		helmEnv:        cli.New(),
		state:          make(map[string]*dependencyState),
	}, nil
}

// InstallDependencyWithSteps runs the installation sequence
func (self *UnbindInstaller) InstallDependencyWithSteps(
	ctx context.Context,
	dependencyName string,
	steps []InstallationStep,
) error {
	// Initialize dependency state
	self.ensureStateInitialized(dependencyName)

	// Mark the start of installation
	self.logProgress(dependencyName, 0.0, fmt.Sprintf("Starting installation of %s", dependencyName), nil, StatusInstalling)

	totalSteps := len(steps)

	// Execute each step
	for i, step := range steps {
		select {
		case <-ctx.Done():
			self.logProgress(dependencyName, step.Progress,
				fmt.Sprintf("Installation canceled: %s", step.Description), ctx.Err(), StatusFailed)
			return ctx.Err()
		default:
			// Progress through the steps
			stepDescription := fmt.Sprintf("Step %d/%d: %s", i+1, totalSteps, step.Description)
			self.logProgress(dependencyName, step.Progress, stepDescription, nil, StatusInstalling)

			startTime := time.Now()

			if err := step.Action(ctx); err != nil {
				failMsg := fmt.Sprintf("Step %d/%d failed: %s - %v", i+1, totalSteps, step.Description, err)
				self.logProgress(dependencyName, step.Progress, failMsg, err, StatusFailed)
				return err
			}

			duration := time.Since(startTime).Round(time.Millisecond)
			self.sendLog(fmt.Sprintf("Step %d/%d completed in %v", i+1, totalSteps, duration))
		}
	}

	// Mark installation as complete
	self.logProgress(dependencyName, 1.0, fmt.Sprintf("Installation of %s completed successfully", dependencyName), nil, StatusCompleted)
	return nil
}

// ensureStateInitialized sets up tracking if needed
func (self *UnbindInstaller) ensureStateInitialized(name string) {
	if _, exists := self.state[name]; !exists {
		self.state[name] = &dependencyState{
			name:        name,
			status:      StatusPending,
			progress:    0.0,
			stepHistory: []string{},
		}
	}
}

// InstallerStatus - possible status values
type InstallerStatus string

const (
	StatusPending    InstallerStatus = "pending"
	StatusInstalling InstallerStatus = "installing"
	StatusCompleted  InstallerStatus = "completed"
	StatusFailed     InstallerStatus = "failed"
)

// UnbindInstallUpdateMsg for UI progress updates
type UnbindInstallUpdateMsg struct {
	Name        string
	Status      InstallerStatus
	Description string
	Progress    float64
	Error       error
	StartTime   time.Time // Start time of the installation
	EndTime     time.Time // End time of the installation
	StepHistory []string  // History of steps executed
}

// DependencyInstallCompleteMsg signals installation finished
type DependencyInstallCompleteMsg struct{}

// Last time we sent a progress update for each dependency
var lastProgressUpdateTimes = make(map[string]time.Time)
var minProgressInterval = 50 * time.Millisecond // Reduced interval for smoother updates

// logProgress handles all state/progress tracking
func (self *UnbindInstaller) logProgress(name string, progress float64, description string, err error, status InstallerStatus) {
	// Ensure state is initialized
	self.ensureStateInitialized(name)

	// Send log message
	if description != "" {
		self.sendLog(description)
	}

	// Update the state
	state := self.state[name]

	// Update status if changed
	if status != state.status {
		if status == StatusInstalling && state.startTime.IsZero() {
			state.startTime = time.Now()
		} else if status == StatusCompleted || status == StatusFailed {
			state.endTime = time.Now()
		}
		state.status = status
	}

	// Update progress and description
	state.progress = progress
	state.description = description
	state.error = err

	// Always add to step history if it's a new step
	if description != "" && (len(state.stepHistory) == 0 || state.stepHistory[len(state.stepHistory)-1] != description) {
		state.stepHistory = append(state.stepHistory, description)
		// Force an update when step history changes
		self.sendUpdateMessage(name)
	} else {
		// For progress updates without description changes
		self.sendUpdateMessage(name)
	}
}

// sendUpdateMessage pushes updates to the UI
func (self *UnbindInstaller) sendUpdateMessage(name string) {
	if self.progressChan == nil {
		return
	}

	state := self.state[name]
	if state == nil {
		return
	}

	// Create message from current state
	msg := UnbindInstallUpdateMsg{
		Name:        name,
		Status:      state.status,
		Description: state.description,
		Progress:    state.progress,
		Error:       state.error,
		StartTime:   state.startTime,
		EndTime:     state.endTime,
		StepHistory: make([]string, len(state.stepHistory)), // Make a copy to avoid mutation issues
	}
	copy(msg.StepHistory, state.stepHistory)

	// Throttle update sending to reduce UI thrashing
	now := time.Now()
	lastUpdate, exists := lastProgressUpdateTimes[name]

	// Check if description has changed from the last update
	descriptionChanged := state.description != "" && 
		(len(state.stepHistory) > 0 && state.stepHistory[len(state.stepHistory)-1] != state.description)

	// Always send messages when:
	// 1. It's the first message for this dependency
	// 2. Status has changed (especially to completed or failed)
	// 3. There's an error
	// 4. Description has changed (ensures all steps are shown)
	// 5. It's a significant progress threshold (0%, 25%, 50%, 75%, 100%)
	// 6. Progress has changed significantly
	// 7. It's been at least minProgressInterval since the last update
	shouldSendUpdate := !exists ||
		state.status != StatusInstalling ||
		state.error != nil ||
		descriptionChanged ||
		state.progress == 0.0 || state.progress == 0.25 || state.progress == 0.5 || state.progress == 0.75 || state.progress == 1.0 ||
		(exists && now.Sub(lastUpdate) >= 500*time.Millisecond)

	// For long-running operations like Helmfile sync, use less frequent updates
	if state.description != "" &&
		(strings.Contains(state.description, "Running helmfile sync") ||
		 strings.Contains(state.description, "Installing Unbind components")) {
		shouldSendUpdate = !exists || now.Sub(lastUpdate) >= 2*time.Second
	}

	if shouldSendUpdate {
		lastProgressUpdateTimes[name] = now
		select {
		case self.progressChan <- msg:
			// Message sent successfully
		default:
			// Channel is full, log it but don't block
			self.sendLog(fmt.Sprintf("Warning: Progress channel for %s is full", name))
		}
	}
}

// sendLog outputs messages to the log channel
func (self *UnbindInstaller) sendLog(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}

// GetDependencyState grabs status info for a component
func (self *UnbindInstaller) GetDependencyState(name string) (InstallerStatus, time.Time, time.Time, []string) {
	if state, exists := self.state[name]; exists {
		return state.status, state.startTime, state.endTime, append([]string{}, state.stepHistory...)
	}
	return StatusPending, time.Time{}, time.Time{}, []string{}
}

// GetLastUpdateMessage grabs latest status
func (self *UnbindInstaller) GetLastUpdateMessage(name string) UnbindInstallUpdateMsg {
	if state, exists := self.state[name]; exists {
		return UnbindInstallUpdateMsg{
			Name:        state.name,
			Status:      state.status,
			Description: state.description,
			Progress:    state.progress,
			Error:       state.error,
			StartTime:   state.startTime,
			EndTime:     state.endTime,
			StepHistory: append([]string{}, state.stepHistory...),
		}
	}
	return UnbindInstallUpdateMsg{Name: name, Status: StatusPending}
}