package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
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
func (self *ZypperInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
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
		// Refresh repositories
		sendProgress(0.1, "Refreshing zypper repositories...")
		refreshCmd := exec.CommandContext(ctx, "zypper", "--non-interactive", "--no-gpg-checks", "refresh")
		if output, err := refreshCmd.CombinedOutput(); err != nil {
			self.log(fmt.Sprintf("Error refreshing zypper: %s", string(output)))
			done <- fmt.Errorf("failed to refresh zypper: %w", err)
			return
		}

		// Install packages
		sendProgress(0.3, "Starting package installation...")
		args := append([]string{"--non-interactive", "--no-gpg-checks", "install"}, packages...)
		installCmd := exec.CommandContext(ctx, "zypper", args...)
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
func (self *ZypperInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
