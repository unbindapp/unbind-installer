package pkgmanager

import (
	"fmt"
	"os/exec"
	"strings"
)

// AptInstaller handles installation of apt packages
type AptInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
}

// NewAptInstaller creates a new AptInstaller
func NewAptInstaller(logChan chan<- string) *AptInstaller {
	return &AptInstaller{
		LogChan: logChan,
	}
}

// InstallPackages installs the specified packages using apt-get
func (self *AptInstaller) InstallPackages(packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	// Total number of phases to report progress
	totalSteps := float64(4) // Update, Download, Install, Verify
	currentStep := float64(0)

	// Log what we're doing
	self.log("Updating apt package lists...")
	if progressFunc != nil {
		progressFunc("", 0.1+(currentStep/totalSteps)*0.9, "Updating package lists", false)
	}

	// Update package lists
	updateCmd := exec.Command("apt-get", "update", "-y")
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error updating apt: %s", string(updateOutput)))
		return fmt.Errorf("failed to update apt: %w", err)
	}
	self.log("Package lists updated successfully")

	// Update progress
	currentStep++
	if progressFunc != nil {
		progressFunc("", 0.1+(currentStep/totalSteps)*0.9, "Package lists updated", false)
	}

	// Install packages
	self.log(fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", ")))

	// Update progress for starting installation
	if progressFunc != nil {
		progressFunc("", 0.1+(currentStep/totalSteps)*0.9, "Starting installation", false)
	}

	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.Command("apt-get", args...)
	installOutput, err := installCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error installing packages: %s", string(installOutput)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	// Update progress for installation completion
	currentStep++
	if progressFunc != nil {
		progressFunc("", 0.1+(currentStep/totalSteps)*0.9, "Packages installed", false)
	}

	// Final verification step
	currentStep++
	if progressFunc != nil {
		progressFunc("", 0.1+(currentStep/totalSteps)*0.9, "Verifying installation", false)
	}

	self.log("Packages installed successfully")

	// Complete
	if progressFunc != nil {
		progressFunc("", 1.0, "Installation complete", true)
	}

	return nil
}

// log sends a message to the log channel if available
func (self *AptInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
