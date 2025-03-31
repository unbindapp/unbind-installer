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
func (a *AptInstaller) InstallPackages(packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	// Log what we're doing
	a.log(fmt.Sprintf("Updating apt package lists..."))

	// Update package lists
	updateCmd := exec.Command("apt-get", "update", "-y")
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		a.log(fmt.Sprintf("Error updating apt: %s", string(updateOutput)))
		return fmt.Errorf("failed to update apt: %w", err)
	}
	a.log("Package lists updated successfully")

	// Install packages
	a.log(fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", ")))
	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.Command("apt-get", args...)
	installOutput, err := installCmd.CombinedOutput()
	if err != nil {
		a.log(fmt.Sprintf("Error installing packages: %s", string(installOutput)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	a.log("Packages installed successfully")
	return nil
}

// log sends a message to the log channel if available
func (a *AptInstaller) log(message string) {
	if a.LogChan != nil {
		a.LogChan <- message
	}
}
