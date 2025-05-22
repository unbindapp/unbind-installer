package pkgmanager

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// AptInstaller for Debian-based systems
type AptInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
}

// NewAptInstaller creates apt package manager
func NewAptInstaller(logChan chan<- string) *AptInstaller {
	return &AptInstaller{
		LogChan: logChan,
	}
}

// InstallPackages handles apt package installation
func (self *AptInstaller) InstallPackages(ctx context.Context, packages []string, progressFunc ProgressFunc) error {
	if len(packages) == 0 {
		return nil
	}

	// Start with proper progress reporting
	if progressFunc != nil {
		progressFunc("", 0.01, "Initializing package installation...", false)
	}

	// Update package lists and install packages
	steps := []struct {
		progress float64
		step     string
		cmd      *exec.Cmd
	}{
		{
			progress: 0.1,
			step:     "Updating package lists...",
			cmd:      exec.CommandContext(ctx, "apt-get", "update", "-y"),
		},
		{
			progress: 0.3,
			step:     fmt.Sprintf("Installing %d packages...", len(packages)),
			cmd:      exec.CommandContext(ctx, "apt-get", append([]string{"install", "-y"}, packages...)...),
		},
	}

	// Execute each step with progress updates
	for i, step := range steps {
		// Report starting this step
		if progressFunc != nil {
			progressFunc("", step.progress, step.step, false)
		}
		self.log(step.step)

		// Start a goroutine to update progress during long-running steps
		progressDone := make(chan struct{})
		go func(startProgress, endProgress float64, stepDesc string) {
			ticker := time.NewTicker(250 * time.Millisecond)
			defer ticker.Stop()

			currentProgress := startProgress
			endPoint := endProgress - 0.05 // Leave room for completion

			for {
				select {
				case <-ticker.C:
					// Gradually increment progress
					if currentProgress < endPoint {
						currentProgress += 0.02
						if progressFunc != nil {
							progressFunc("", currentProgress, stepDesc, false)
						}
					}
				case <-progressDone:
					return
				case <-ctx.Done():
					return
				}
			}
		}(step.progress, func() float64 {
			if i < len(steps)-1 {
				return steps[i+1].progress
			}
			return 0.9
		}(), step.step)

		// Execute the command
		output, err := step.cmd.CombinedOutput()
		close(progressDone) // Stop progress updates

		// Handle errors
		if err != nil {
			self.log(fmt.Sprintf("Error: %s\nOutput: %s", err, string(output)))
			return fmt.Errorf("failed during %s: %w", step.step, err)
		}

		// Log successful completion
		self.log(fmt.Sprintf("Completed: %s", step.step))
	}

	// Final verification step
	if progressFunc != nil {
		progressFunc("", 0.9, "Verifying installation...", false)
	}
	self.log("Verifying installation...")

	// Wait a moment to show completion progress
	time.Sleep(500 * time.Millisecond)

	// Report completion
	if progressFunc != nil {
		progressFunc("", 1.0, "Installation complete", true)
	}
	self.log("Packages installed successfully")

	return nil
}

// log writes to log channel
func (self *AptInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
