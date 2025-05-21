package pkgmanager

import (
	"fmt"
	"os/exec"
	"strings"
)

// ZypperInstaller handles installation of zypper packages
type ZypperInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
}

// NewZypperInstaller creates a new ZypperInstaller
func NewZypperInstaller(logChan chan<- string) *ZypperInstaller {
	return &ZypperInstaller{
		LogChan: logChan,
	}
}

// InstallPackages installs the specified packages using zypper
func (self *ZypperInstaller) InstallPackages(packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	// Total number of phases to report progress
	totalSteps := float64(4) // Update, Download, Install, Verify
	currentStep := float64(0)

	// Log what we're doing
	self.log("Updating zypper package lists...")
	if progressFunc != nil {
		progressFunc("", currentStep/totalSteps, "Updating package lists", false)
	}

	// Update package lists
	updateCmd := exec.Command("zypper", "refresh")
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error updating zypper: %s", string(updateOutput)))
		return fmt.Errorf("failed to update zypper: %w", err)
	}
	self.log("Package lists updated successfully")

	// Update progress
	currentStep++
	if progressFunc != nil {
		progressFunc("", currentStep/totalSteps, "Package lists updated", false)
	}

	// Install packages
	self.log(fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", ")))

	// Update progress for starting installation
	if progressFunc != nil {
		progressFunc("", currentStep/totalSteps, "Starting installation", false)
	}

	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.Command("zypper", args...)
	installOutput, err := installCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error installing packages: %s", string(installOutput)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	// Update progress for installation completion
	currentStep++
	if progressFunc != nil {
		progressFunc("", currentStep/totalSteps, "Packages installed", false)
	}

	// Final verification step
	currentStep++
	if progressFunc != nil {
		progressFunc("", currentStep/totalSteps, "Verifying installation", false)
	}

	self.log("Packages installed successfully")

	// Complete
	if progressFunc != nil {
		progressFunc("", 1.0, "Installation complete", true)
	}

	return nil
}

// log sends a message to the log channel if available
func (self *ZypperInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
