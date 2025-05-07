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
func (self *ZypperInstaller) InstallPackages(packages []string) error {
	if len(packages) == 0 {
		return nil
	}

	// Log what we're doing
	self.log(fmt.Sprintf("Updating zypper package lists..."))

	// Update package lists
	updateCmd := exec.Command("zypper", "refresh")
	updateOutput, err := updateCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error updating zypper: %s", string(updateOutput)))
		return fmt.Errorf("failed to update zypper: %w", err)
	}
	self.log("Package lists updated successfully")

	// Install packages
	self.log(fmt.Sprintf("Installing packages: %s", strings.Join(packages, ", ")))
	args := append([]string{"install", "-y"}, packages...)
	installCmd := exec.Command("zypper", args...)
	installOutput, err := installCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error installing packages: %s", string(installOutput)))
		return fmt.Errorf("failed to install packages: %w", err)
	}

	self.log("Packages installed successfully")
	return nil
}

// log sends a message to the log channel if available
func (self *ZypperInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
