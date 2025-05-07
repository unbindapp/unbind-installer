package pkgmanager

import (
	"fmt"
	"os/exec"
	"strings"
)

// DNFInstaller handles installation of dnf packages
type DNFInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
}

// NewDNFInstaller creates a new DNFInstaller
func NewDNFInstaller(logChan chan<- string) *DNFInstaller {
	return &DNFInstaller{
		LogChan: logChan,
	}
}

// InstallPackages installs the specified packages using dnf
func (self *DNFInstaller) InstallPackages(packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	// Log what we're doing
	self.log(fmt.Sprintf("Updating dnf package lists..."))

	// Update package lists
	updateCmd := exec.Command("dnf", "update", "-y")
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error updating dnf: %s", string(updateOutput)))
		return fmt.Errorf("failed to update dnf: %w", err)
	}
	self.log("Package lists updated successfully")

	// Install packages
	self.log(fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", ")))
	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.Command("dnf", args...)
	installOutput, err := installCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error installing packages: %s", string(installOutput)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	self.log("Packages installed successfully")
	return nil
}

// log sends a message to the log channel if available
func (self *DNFInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
