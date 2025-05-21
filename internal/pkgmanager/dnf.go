package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
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
func (self *DNFInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	sendProgress := func(progress float64, step string) {
		if progressFunc != nil {
			progressFunc("", progress, step, false)
		}
	}

	// Create a channel to signal completion
	done := make(chan error, 1)

	// Start the installation in a goroutine
	go func() {
		// Update package lists
		sendProgress(0.1, "Updating DNF package lists...")
		updateCmd := exec.CommandContext(ctx, "dnf", "makecache", "--refresh", "-y")
		if output, err := updateCmd.CombinedOutput(); err != nil {
			self.log(fmt.Sprintf("Error updating dnf: %s", string(output)))
			done <- fmt.Errorf("failed to update dnf: %w", err)
			return
		}

		// Install packages
		sendProgress(0.3, "Starting package installation...")
		args := append([]string{"install", "-y"}, packages...)
		installCmd := exec.CommandContext(ctx, "dnf", args...)
		if output, err := installCmd.CombinedOutput(); err != nil {
			self.log(fmt.Sprintf("Error installing packages: %s", string(output)))
			done <- fmt.Errorf("failed to install packages: %w", err)
			return
		}

		sendProgress(0.9, "Verifying installation...")
		done <- nil
	}()

	// Wait for completion or cancellation
	select {
	case <-ctx.Done():
		self.log("Installation cancelled by user")
		return ctx.Err()
	case err := <-done:
		if err == nil {
			if progressFunc != nil {
				progressFunc("", 1.0, "Installation complete", true)
			}
			self.log("Packages installed successfully")
		}
		return err
	}
}

// log sends a message to the log channel if available
func (self *DNFInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
