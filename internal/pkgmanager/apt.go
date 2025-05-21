package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
	"time"
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
func (self *AptInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	// Last time we sent a progress update
	lastProgressUpdate := time.Now()
	minProgressInterval := 500 * time.Millisecond

	sendProgress := func(progress float64, step string) {
		if progressFunc != nil {
			// Throttle progress updates to reduce UI thrashing
			now := time.Now()
			if now.Sub(lastProgressUpdate) >= minProgressInterval {
				progressFunc("", progress, step, false)
				lastProgressUpdate = now
			}
		}
	}

	// Create a channel to signal completion
	done := make(chan error, 1)

	// Start the installation in a goroutine
	go func() {
		// Update package lists
		sendProgress(0.1, "Updating package lists...")
		updateCmd := exec.CommandContext(ctx, "apt-get", "update", "-y")
		if output, err := updateCmd.CombinedOutput(); err != nil {
			self.log(fmt.Sprintf("Error updating apt: %s", string(output)))
			done <- fmt.Errorf("failed to update apt: %w", err)
			return
		}

		// Install packages
		sendProgress(0.3, "Starting package installation...")
		args := append([]string{"install", "-y"}, packages...)
		installCmd := exec.CommandContext(ctx, "apt-get", args...)
		if output, err := installCmd.CombinedOutput(); err != nil {
			self.log(fmt.Sprintf("Error installing packages: %s", string(output)))
			done <- fmt.Errorf("failed to install packages: %w", err)
			return
		}

		sendProgress(0.9, "Verifying installation...")

		// Add a small delay to prevent UI thrashing during transition
		time.Sleep(300 * time.Millisecond)

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
				// Ensure we send the final completion status
				progressFunc("", 1.0, "Installation complete", true)
			}
			self.log("Packages installed successfully")
		}
		return err
	}
}

// log sends a message to the log channel if available
func (self *AptInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
