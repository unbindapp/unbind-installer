package dependencies

import (
	"context"
	"fmt"
	"time"

	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type DependenciesManager struct {
	progressChan chan<- DependencyUpdateMsg
	kubeClient   *kubernetes.Clientset
	LogChan      chan<- string
	helmEnv      *cli.EnvSettings
}

func NewDependenciesManager(kubeConfig string, logChan chan<- string, progressChan chan<- DependencyUpdateMsg) (*DependenciesManager, error) {
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

	return &DependenciesManager{
		progressChan: progressChan,
		kubeClient:   clientset,
		LogChan:      logChan,
		helmEnv:      cli.New(),
	}, nil
}

// InstallDependencyWithSteps installs a dependency using a series of installation steps
func (self *DependenciesManager) InstallDependencyWithSteps(
	ctx context.Context,
	dependencyName string,
	steps []InstallationStep,
) error {
	// Mark the start of installation
	self.startInstallation(dependencyName)

	totalSteps := len(steps)

	// Execute each step
	for i, step := range steps {
		select {
		case <-ctx.Done():
			self.logProgress(dependencyName, step.Progress, "Installation canceled: %s", step.Description)
			self.failInstallation(dependencyName, ctx.Err())
			return ctx.Err()
		default:
			// Progress through the steps
			self.logProgress(dependencyName, step.Progress,
				"Step %d/%d: %s", i+1, totalSteps, step.Description)

			startTime := time.Now()

			if err := step.Action(ctx); err != nil {
				self.logProgress(dependencyName, step.Progress,
					"Step %d/%d failed: %s - %v", i+1, totalSteps, step.Description, err)
				self.failInstallation(dependencyName, err)
				return err
			}

			duration := time.Since(startTime).Round(time.Millisecond)
			self.sendLog(fmt.Sprintf("Step %d/%d completed in %v", i+1, totalSteps, duration))
		}
	}

	// Mark installation as complete
	self.completeInstallation(dependencyName)
	return nil
}

// log sends a message to the log channel if available
func (self *DependenciesManager) logf(message string, args ...interface{}) {
	if self.LogChan != nil {
		self.LogChan <- fmt.Sprintf(message, args...)
	}
}

// *Progress methods for tea UI

// DependencyStatus represents the state of a dependency installation
type DependencyStatus string

const (
	StatusPending    DependencyStatus = "pending"
	StatusInstalling DependencyStatus = "installing"
	StatusCompleted  DependencyStatus = "completed"
	StatusFailed     DependencyStatus = "failed"
)

// DependencyUpdateMsg is sent when a dependency status changes
type DependencyUpdateMsg struct {
	Name        string
	Status      DependencyStatus
	Description string
	Progress    float64
	Error       error
}

// DependencyInstallCompleteMsg is sent when all dependencies are installed
type DependencyInstallCompleteMsg struct{}

// updateProgress updates the progress of a dependency
func (self *DependenciesManager) updateProgress(name string, description string, progress float64) {
	self.progressChan <- DependencyUpdateMsg{
		Name:        name,
		Status:      StatusInstalling,
		Description: description,
		Progress:    progress,
	}
}

// startInstallation marks a dependency as being installed
func (self *DependenciesManager) startInstallation(name string) {
	self.progressChan <- DependencyUpdateMsg{
		Name:     name,
		Status:   StatusInstalling,
		Progress: 0.0,
	}
	self.logf("Starting installation of %s", name)
}

// completeInstallation marks a dependency as completed
func (self *DependenciesManager) completeInstallation(name string) {
	self.progressChan <- DependencyUpdateMsg{
		Name:     name,
		Status:   StatusCompleted,
		Progress: 1.0,
	}
	self.logf("Installation of %s completed successfully", name)
}

// failInstallation marks a dependency as failed
func (self *DependenciesManager) failInstallation(name string, err error) {
	self.progressChan <- DependencyUpdateMsg{
		Name:     name,
		Status:   StatusFailed,
		Progress: 0.0,
		Error:    err,
	}
	self.logf("Failed to install %s: %v", name, err)
}

// sendLog sends a log message to both the LogChan and the BubbleTea program
func (self *DependenciesManager) sendLog(message string) {
	// Send to the log channel
	if self.LogChan != nil {
		self.LogChan <- message
	}
}

// logProgress logs a message and updates progress at the same time
func (self *DependenciesManager) logProgress(name string, progress float64, message string, args ...interface{}) {
	formattedMessage := fmt.Sprintf(message, args...)

	// Send the log message
	self.sendLog(formattedMessage)

	// Update the progress
	self.updateProgress(name, formattedMessage, progress)
}
