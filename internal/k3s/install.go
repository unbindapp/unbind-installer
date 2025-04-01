package k3s

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// InstallationStep defines a step in the installation process
type InstallationStep struct {
	Description string
	Progress    float64
	Action      func(context.Context) error
}

// K3SUpdateMessage is sent when a K3S installation status is updated
type K3SUpdateMessage struct {
	Progress    float64   // Progress from 0.0 to 1.0
	Status      string    // Current status: "pending", "installing", "completed", "failed"
	Description string    // Current step description
	Error       error     // Error if any
	StartTime   time.Time // Start time of the installation
	EndTime     time.Time // End time of the installation
	StepHistory []string  // History of steps executed
}

// Installer handles installation of K3S
type Installer struct {
	// Channel to send log messages
	LogChan chan<- string
	// Update message channel
	UpdateChan chan<- K3SUpdateMessage
	// Installation state
	state struct {
		startTime time.Time
		endTime   time.Time
		status    string
		lastMsg   K3SUpdateMessage
	}
}

// NewInstaller creates a new K3S Installer
func NewInstaller(logChan chan<- string, updateChan chan<- K3SUpdateMessage) *Installer {
	inst := &Installer{
		LogChan:    logChan,
		UpdateChan: updateChan,
	}

	// Initialize state
	inst.state.status = "pending"

	return inst
}

// logProgress sends a log message, progress update, and update message
func (self *Installer) logProgress(progress float64, status string, description string, err error) {
	// Send log message
	if description != "" {
		self.log(description)
	}

	// Update state if status changes
	if status != self.state.status {
		if status == "installing" && self.state.startTime.IsZero() {
			self.state.startTime = time.Now()
		} else if status == "completed" || status == "failed" {
			self.state.endTime = time.Now()
		}
		self.state.status = status
	}

	// Send detailed update message
	self.sendUpdateMessage(progress, status, description, err)
}

// log sends a message to the log channel if available
func (self *Installer) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}

// sendUpdateMessage sends a detailed update message
func (self *Installer) sendUpdateMessage(progress float64, status string, description string, err error) {
	msg := K3SUpdateMessage{
		Progress:    progress,
		Status:      status,
		Description: description,
		Error:       err,
		StartTime:   self.state.startTime,
		EndTime:     self.state.endTime,
	}

	self.state.lastMsg = msg

	if self.UpdateChan != nil {
		self.UpdateChan <- msg
	}
}

// Install installs K3S using a step-based approach with progress tracking
// Returns kube config path if successful, or an error if it fails
func (self *Installer) Install(ctx context.Context) (string, error) {
	k3sInstallFlags := "--flannel-backend=none --disable-kube-proxy --disable=servicelb --disable-network-policy --disable=traefik"

	var kubeconfigPath string

	// Start the installation process and initialize state
	self.state.startTime = time.Now()
	self.state.status = "installing"

	// Send initial status update
	self.logProgress(0.01, "installing", "Preparing K3S installation...", nil)

	// Define the installation steps
	steps := []InstallationStep{
		{
			Description: "Downloading K3S installation script",
			Progress:    0.10,
			Action: func(ctx context.Context) error {
				downloadCmd := exec.CommandContext(ctx, "curl", "-sfL", "https://get.k3s.io", "-o", "/tmp/k3s-installer.sh")
				downloadOutput, err := downloadCmd.CombinedOutput()
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading K3S installer: %s", string(downloadOutput))
					self.log(errMsg)
					return fmt.Errorf("failed to download K3S installer: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Setting execution permissions on installer script",
			Progress:    0.15,
			Action: func(ctx context.Context) error {
				chmodCmd := exec.CommandContext(ctx, "chmod", "+x", "/tmp/k3s-installer.sh")
				chmodOutput, err := chmodCmd.CombinedOutput()
				if err != nil {
					errMsg := fmt.Sprintf("Error setting permissions: %s", string(chmodOutput))
					self.log(errMsg)
					return fmt.Errorf("failed to set installer permissions: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Running K3S installer",
			Progress:    0.20,
			Action: func(ctx context.Context) error {
				self.log(fmt.Sprintf("Running K3S installer with flags: %s", k3sInstallFlags))
				installCmd := exec.CommandContext(ctx, "/bin/sh", "/tmp/k3s-installer.sh")
				installCmd.Env = append(os.Environ(), fmt.Sprintf("INSTALL_K3S_EXEC=%s", k3sInstallFlags))

				installStartTime := time.Now()
				installDone := make(chan error, 1)

				go func() {
					currentProgress := 0.20

					ticker := time.NewTicker(2 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.35 {
								currentProgress += 0.03
								elapsed := time.Since(installStartTime).Round(time.Second)
								updatedDescription := fmt.Sprintf("Running K3S installer (elapsed: %v)...", elapsed)
								self.logProgress(currentProgress, "installing", updatedDescription, nil)
							}
						case <-installDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				installOutput, _ := installCmd.CombinedOutput()
				installOutputStr := string(installOutput)

				close(installDone)

				self.log(fmt.Sprintf("Installation output: %s", installOutputStr))

				if strings.Contains(installOutputStr, "Job for k3s.service failed") {
					self.log("Detected k3s.service failure in the installation output")

					serviceError := self.collectServiceDiagnostics()

					if serviceError != nil {
						return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Checking K3S service status",
			Progress:    0.40,
			Action: func(ctx context.Context) error {
				serviceStatus, statusErr := self.checkServiceStatus()
				if statusErr != nil || serviceStatus != "active" {
					errMsg := fmt.Sprintf("K3S service is not active after installation (status: %s)", serviceStatus)
					self.log(errMsg)

					serviceError := self.collectServiceDiagnostics()

					if serviceError != nil {
						return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Waiting for K3S service to become fully active",
			Progress:    0.60,
			Action: func(ctx context.Context) error {
				waitStartTime := time.Now()
				waitDone := make(chan error, 1)

				go func() {
					currentProgress := 0.60

					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.75 {
								currentProgress += 0.03
								elapsed := time.Since(waitStartTime).Round(time.Second)
								updatedDescription := fmt.Sprintf("Waiting for K3S service to start (elapsed: %v)...", elapsed)
								self.logProgress(currentProgress, "installing", updatedDescription, nil)
							}
						case <-waitDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				maxRetries := 6
				for retry := 0; retry < maxRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking K3S service status (attempt %d/%d)...", retry+1, maxRetries))
					status, err := self.checkServiceStatus()

					if status == "active" {
						self.log("K3S service is now active")
						waitDone <- nil
						close(waitDone)
						return nil
					}

					if retry == maxRetries-1 || status == "failed" {
						errMsg := fmt.Sprintf("K3S service failed to become active (status: %s, error: %v)", status, err)
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()
						waitDone <- serviceError
						close(waitDone)
						return fmt.Errorf("K3S service failed to become active after installation: %w", serviceError)
					}
				}

				waitDone <- nil
				close(waitDone)
				return nil
			},
		},
		{
			Description: "Waiting for kubeconfig to be created",
			Progress:    0.75,
			Action: func(ctx context.Context) error {
				kubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

				maxKubeRetries := 6
				for retry := 0; retry < maxKubeRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking for kubeconfig (attempt %d/%d)...", retry+1, maxKubeRetries))
					if _, err := os.Stat(kubeconfigPath); err == nil {
						self.log("Kubeconfig file found")
						return nil
					}

					if retry == maxKubeRetries-1 {
						errMsg := "Kubeconfig file not created after multiple attempts"
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()

						return fmt.Errorf("K3S installed but kubeconfig not created: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Verifying K3S installation by checking nodes",
			Progress:    0.85,
			Action: func(ctx context.Context) error {
				maxNodeRetries := 6
				for retry := 0; retry < maxNodeRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking K3S nodes (attempt %d/%d)...", retry+1, maxNodeRetries))
					kubeCmd := exec.CommandContext(ctx, "k3s", "kubectl", "get", "nodes")
					kubeOutput, err := kubeCmd.CombinedOutput()

					if err == nil {
						successMsg := fmt.Sprintf("K3S nodes: %s", string(kubeOutput))
						self.log(successMsg)
						self.log("K3S installation verified successfully")
						return nil
					}

					if retry == maxNodeRetries-1 {
						errMsg := fmt.Sprintf("Error checking K3S nodes: %s", string(kubeOutput))
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()

						return fmt.Errorf("K3S installed but not functioning correctly: %w", serviceError)
					}
				}
				return nil
			},
		},
	}

	// Execute all installation steps
	for _, step := range steps {
		// Log the current step
		self.logProgress(step.Progress, "installing", step.Description, nil)

		// Execute the step's action
		if err := step.Action(ctx); err != nil {
			// Set end time and send failure update
			self.state.endTime = time.Now()
			self.logProgress(step.Progress, "failed", fmt.Sprintf("Failed: %s", step.Description), err)
			return "", err
		}
	}

	// Set end time and send final progress update
	self.state.endTime = time.Now()
	self.logProgress(1.0, "completed", "K3S installation completed successfully", nil)

	return kubeconfigPath, nil
}

// GetLastUpdateMessage returns the most recent update message
func (self *Installer) GetLastUpdateMessage() K3SUpdateMessage {
	return self.state.lastMsg
}

// GetInstallationState returns the current installation state
func (self *Installer) GetInstallationState() (status string, startTime time.Time, endTime time.Time) {
	return self.state.status, self.state.startTime, self.state.endTime
}

// checkServiceStatus checks if the K3S service is active
func (self *Installer) checkServiceStatus() (string, error) {
	checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
	statusOutput, err := checkCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(statusOutput))

	return statusStr, err
}

// collectServiceDiagnostics collects detailed diagnostics about the K3S service
func (self *Installer) collectServiceDiagnostics() error {
	// Get detailed service status
	statusCmd := exec.Command("systemctl", "status", "-l", "k3s.service")
	statusOutput, _ := statusCmd.CombinedOutput()
	self.log(fmt.Sprintf("K3S service status: %s", string(statusOutput)))

	// Check if the service is active
	isActiveCmd := exec.Command("systemctl", "is-active", "k3s.service")
	isActiveOutput, _ := isActiveCmd.CombinedOutput()
	isActiveStr := strings.TrimSpace(string(isActiveOutput))

	// If the service is active, it's not in a failed state regardless of journal messages
	if isActiveStr == "active" {
		// Look for a message indicating k3s is running in the status output
		if strings.Contains(string(statusOutput), "k3s is up and running") {
			self.log("K3S service is active and running properly")
			return nil
		}
	}

	// Get journal logs with priority (error and above)
	journalCmd := exec.Command("journalctl", "-n", "50", "-p", "err", "-u", "k3s.service", "-l")
	journalOutput, _ := journalCmd.CombinedOutput()
	self.log(fmt.Sprintf("K3S service errors from journal: %s", string(journalOutput)))

	// Check if the service is failed
	isFailedCmd := exec.Command("systemctl", "is-failed", "k3s.service")
	isFailedOutput, _ := isFailedCmd.CombinedOutput()
	isFailedStr := strings.TrimSpace(string(isFailedOutput))

	if isFailedStr == "failed" {
		return fmt.Errorf("K3S service is in failed state")
	} else if isActiveStr != "active" {
		return fmt.Errorf("K3S service is in abnormal state: %s", isActiveStr)
	}

	// If we got here and the service is active, it's probably fine
	return nil
}
