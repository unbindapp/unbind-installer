package installer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SyncHelmfileOptions defines options for the Helmfile sync operation
type SyncHelmfileOptions struct {
	BaseDomain           string
	UnbindDomain         string
	UnbindRegistryDomain string
	AdditionalValues     map[string]interface{}
	RepoURL              string
}

// SyncHelmfileWithSteps performs a helmfile sync operation using the unbind-charts repository
func (self *UnbindInstaller) SyncHelmfileWithSteps(ctx context.Context, opts SyncHelmfileOptions) error {
	var repoDir string
	dependencyName := "helmfile-sync"

	// Set defaults if not provided
	if opts.RepoURL == "" {
		opts.RepoURL = "https://github.com/unbindapp/unbind-charts.git"
	}

	// Initialize state for this dependency
	self.ensureStateInitialized(dependencyName)

	// Mark the beginning of installation
	self.logProgress(dependencyName, 0.0, fmt.Sprintf("Starting helmfile sync with base domain %s", opts.BaseDomain), nil, StatusInstalling)

	return self.InstallDependencyWithSteps(ctx, dependencyName, []InstallationStep{
		{
			Description: "Creating temporary directory",
			Progress:    0.15,
			Action: func(ctx context.Context) error {
				var err error
				repoDir, err = os.MkdirTemp("", "unbind-charts-*")
				if err != nil {
					return fmt.Errorf("failed to create temp directory: %w", err)
				}
				return nil
			},
		},
		{
			Description: "Cloning repository",
			Progress:    0.20,
			Action: func(ctx context.Context) error {
				self.logProgress(dependencyName, 0.21, fmt.Sprintf("Cloning from %s", opts.RepoURL), nil, StatusInstalling)

				cmd := exec.CommandContext(ctx, "git", "clone", opts.RepoURL, repoDir)

				// Stream the output
				stdoutPipe, err := cmd.StdoutPipe()
				if err != nil {
					return fmt.Errorf("failed to create git stdout pipe: %w", err)
				}

				stderrPipe, err := cmd.StderrPipe()
				if err != nil {
					return fmt.Errorf("failed to create git stderr pipe: %w", err)
				}

				if err := cmd.Start(); err != nil {
					return fmt.Errorf("failed to start git clone: %w", err)
				}

				// Stream stdout
				go func() {
					scanner := bufio.NewScanner(stdoutPipe)
					for scanner.Scan() {
						self.sendLog("git: " + scanner.Text())
					}
				}()

				// Stream stderr
				go func() {
					scanner := bufio.NewScanner(stderrPipe)
					for scanner.Scan() {
						self.sendLog("git: " + scanner.Text())
					}
				}()

				if err := cmd.Wait(); err != nil {
					return fmt.Errorf("failed to clone repository: %w", err)
				}

				self.logProgress(dependencyName, 0.29, fmt.Sprintf("Repository cloned successfully to %s", repoDir), nil, StatusInstalling)
				return nil
			},
		},
		{
			Description: "Running helmfile sync",
			Progress:    0.30,
			Action: func(ctx context.Context) error {
				// Construct arguments for helmfile command
				args := []string{
					"--file", filepath.Join(repoDir, "helmfile.yaml"),
					"--state-values-set", "unbindDomain=" + opts.UnbindDomain,
					"--state-values-set", "unbindRegistryDomain=" + opts.UnbindRegistryDomain,
					"--state-values-set", "wildcardBaseDomain=" + opts.BaseDomain,
				}

				// Add any additional values if present
				for key, value := range opts.AdditionalValues {
					args = append(args, "--state-values-set", fmt.Sprintf("%s=%v", key, value))
				}

				// Add final "sync" command
				args = append(args, "sync")

				self.logProgress(dependencyName, 0.31, fmt.Sprintf("Starting installation with %s", strings.Join(args, "|")), nil, StatusInstalling)

				// Set up the command to run helmfile sync
				cmd := exec.CommandContext(ctx, "helmfile", args...)
				cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", self.kubeConfigPath))

				// Start progress updates during wait
				waitDone := make(chan error, 1)

				// Tick progress slowly
				go func() {
					currentProgress := 0.35

					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.85 {
								currentProgress += 0.005
								self.logProgress(dependencyName, currentProgress, self.state[dependencyName].description, nil, StatusInstalling)
							}
						case <-waitDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				// Set the current working directory
				cmd.Dir = repoDir

				// Create pipes for stdout/stderr
				stdoutPipe, err := cmd.StdoutPipe()
				if err != nil {
					return fmt.Errorf("failed to create stdout pipe: %w", err)
				}

				stderrPipe, err := cmd.StderrPipe()
				if err != nil {
					return fmt.Errorf("failed to create stderr pipe: %w", err)
				}

				// Start the command
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("failed to start helmfile sync: %w", err)
				}

				// Set up scanner for stdout
				stdoutScanner := bufio.NewScanner(stdoutPipe)
				stdoutScanner.Buffer(make([]byte, 4096), 1024*1024) // Increase buffer size

				// Set up scanner for stderr
				stderrScanner := bufio.NewScanner(stderrPipe)
				stderrScanner.Buffer(make([]byte, 4096), 1024*1024) // Increase buffer size

				// Sync start time
				syncStartTime := time.Now()
				self.sendLog(fmt.Sprintf("Helmfile sync started at %v", syncStartTime.Format(time.RFC3339)))

				// Run scanners in separate goroutines
				go func() {
					for stdoutScanner.Scan() {
						line := stdoutScanner.Text()
						self.sendLog(line)
						self.updateProgressBasedOnOutput(dependencyName, line)
					}
				}()

				go func() {
					for stderrScanner.Scan() {
						line := stderrScanner.Text()
						// Only log as error if it actually contains error-related words
						if isErrorMessage(line) {
							self.sendLog("ERROR: " + line)
						} else {
							self.sendLog(line)
						}
						self.updateProgressBasedOnOutput(dependencyName, line)
					}
				}()

				// Wait for the command to complete
				err = cmd.Wait()
				syncDuration := time.Since(syncStartTime).Round(time.Second)
				close(waitDone)

				if err != nil {
					failMsg := fmt.Sprintf("Helmfile sync failed after %v: %v", syncDuration, err)
					self.logProgress(dependencyName, 0.30, failMsg, err, StatusFailed)
					return fmt.Errorf("helmfile sync failed after %v: %w", syncDuration, err)
				}

				self.logProgress(dependencyName, 0.90, fmt.Sprintf("Helmfile sync completed successfully in %v", syncDuration), nil, StatusInstalling)
				return nil
			},
		},
		{
			Description: "Cleaning up temporary files",
			Progress:    0.95,
			Action: func(ctx context.Context) error {
				// Clean up the temporary directory
				if repoDir != "" {
					if err := os.RemoveAll(repoDir); err != nil {
						warningMsg := fmt.Sprintf("Warning: failed to clean up repository directory %s: %v", repoDir, err)
						self.sendLog(warningMsg)
						// We'll consider this a non-fatal error
					} else {
						self.sendLog("Cleaned up temporary repository directory")
					}
				}

				return nil
			},
		},
	})
}

// Helper function to download a file with progress updates
func (self *UnbindInstaller) downloadFileWithProgress(url, filepath string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Get file size for progress reporting
	fileSize := resp.ContentLength

	// Create a progress tracker
	counter := &writeCounter{
		Total:         fileSize,
		Downloaded:    int64(0),
		Installer:     self,
		Dependency:    "helmfile-sync",
		LastProgress:  float64(0),
		StartProgress: 0.06,
		EndProgress:   0.07,
	}

	// Copy while counting progress
	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	return err
}

// writeCounter counts bytes written and updates progress
type writeCounter struct {
	Total         int64
	Downloaded    int64
	Installer     *UnbindInstaller
	Dependency    string
	LastProgress  float64
	StartProgress float64
	EndProgress   float64
}

func (wc *writeCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Downloaded += int64(n)

	// Calculate percentage
	var percentage float64
	if wc.Total > 0 {
		percentage = float64(wc.Downloaded) / float64(wc.Total)
	} else {
		percentage = 0
	}

	// Scale the progress to our range
	scaledProgress := wc.StartProgress + (percentage * (wc.EndProgress - wc.StartProgress))

	// Only update every 1% to avoid flooding
	if scaledProgress-wc.LastProgress >= 0.01 {
		wc.Installer.logProgress(wc.Dependency, scaledProgress, "Downloading Helmfile", nil, StatusInstalling)
		wc.LastProgress = scaledProgress
	}

	return n, nil
}

// Helper function to check if we can write to a directory
func canWriteToDir(dir string) bool {
	testFile := filepath.Join(dir, ".helmfile_write_test")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return false
	}
	os.Remove(testFile)
	return true
}

// isErrorMessage checks if a line is likely an error message
func isErrorMessage(message string) bool {
	lowercaseMsg := strings.ToLower(message)
	errorKeywords := []string{"error", "fail", "fatal", "exception", "panic"}

	for _, keyword := range errorKeywords {
		if strings.Contains(lowercaseMsg, keyword) {
			return true
		}
	}

	return false
}

// updateProgressBasedOnOutput updates the progress based on the output from helmfile
func (self *UnbindInstaller) updateProgressBasedOnOutput(dependency string, output string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return
	}

	var progress float64
	var description string

	if containsString(output, "Building dependency release") {
		progress = 0.35
		description = "Building dependencies"
	} else if containsString(output, "Processing releases") {
		progress = 0.45
		description = "Processing releases"
	} else if containsString(output, "Fetching chart") {
		progress = 0.50
		description = "Fetching charts"
	} else if containsString(output, "Preparing chart") || containsString(output, "Preparing values") {
		progress = 0.55
		description = "Preparing charts"
	} else if containsString(output, "Starting diff") {
		progress = 0.60
		description = "Comparing differences"
	} else if containsString(output, "release=") && containsString(output, "installed") {
		progress = 0.70
		description = "Installing releases"
	} else if containsString(output, "release=") && containsString(output, "upgraded") {
		progress = 0.75
		description = "Upgrading releases"
	} else if containsString(output, "Release was successful") {
		progress = 0.80
		description = "Release successful"
	} else if containsString(output, "release=") && containsString(output, "skipped") {
		progress = 0.85
		description = "Skipping unchanged releases"
	} else {
		// No recognized pattern, don't update progress
		return
	}

	self.logProgress(dependency, progress, description, nil, StatusInstalling)
}

// containsString is a helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
