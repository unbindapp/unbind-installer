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
	"runtime"
	"strings"
	"time"
)

// SyncHelmfileOptions defines options for the Helmfile sync operation
type SyncHelmfileOptions struct {
	BaseDomain       string
	AdditionalValues map[string]interface{}
	RepoURL          string
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
			Description: "Installing Helm",
			Progress:    0.02,
			Action: func(ctx context.Context) error {
				// Check if helm is already installed
				cmd := exec.CommandContext(ctx, "helm", "version")
				out, err := cmd.CombinedOutput()
				if err == nil {
					msg := fmt.Sprintf("Helm is already installed: %s", strings.TrimSpace(string(out)))
					self.logProgress(dependencyName, 0.03, msg, nil, StatusInstalling)
					return nil
				}

				self.logProgress(dependencyName, 0.02, "Helm not found, installing...", nil, StatusInstalling)

				// Create temp directory for download
				tempDir, err := os.MkdirTemp("", "helm-*")
				if err != nil {
					return fmt.Errorf("failed to create temp directory: %w", err)
				}
				defer os.RemoveAll(tempDir)

				// Determine OS and architecture
				goarch := runtime.GOARCH

				// Map architecture to Helm naming convention
				helmArch := goarch
				if goarch == "amd64" {
					helmArch = "amd64"
				} else if goarch == "arm64" {
					helmArch = "arm64"
				}

				version := "3.17.3"

				// Construct the download URL for Helm
				url := fmt.Sprintf("https://get.helm.sh/helm-v%s-%s-%s.tar.gz",
					version, "linux", helmArch)

				self.logProgress(dependencyName, 0.023, fmt.Sprintf("Downloading Helm from %s", url), nil, StatusInstalling)

				// Download Helm
				tarPath := filepath.Join(tempDir, "helm.tar.gz")
				if err := self.downloadFileWithProgress(url, tarPath); err != nil {
					return fmt.Errorf("failed to download helm: %w", err)
				}

				self.logProgress(dependencyName, 0.025, "Extracting Helm", nil, StatusInstalling)

				// Extract the file
				cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("failed to extract helm: %w, output: %s", err, string(out))
				}

				// Find an appropriate bin directory
				binPath := "/usr/local/bin"
				self.logProgress(dependencyName, 0.027, "Checking installation directory", nil, StatusInstalling)

				if !canWriteToDir(binPath) {
					// Try user's local bin directory instead
					home, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get home directory: %w", err)
					}
					binPath = filepath.Join(home, ".local", "bin")
					if err := os.MkdirAll(binPath, 0755); err != nil {
						return fmt.Errorf("failed to create bin directory: %w", err)
					}

					// Make sure the bin directory is in PATH
					if !strings.Contains(os.Getenv("PATH"), binPath) {
						self.sendLog(fmt.Sprintf("Note: Make sure to add %s to your PATH", binPath))
					}
				}

				self.logProgress(dependencyName, 0.028, fmt.Sprintf("Installing Helm to %s", binPath), nil, StatusInstalling)

				// The binary is in a subdirectory named after the OS-ARCH
				sourcePath := filepath.Join(tempDir, fmt.Sprintf("%s-%s", "linux", helmArch), "helm")
				destPath := filepath.Join(binPath, "helm")

				input, err := os.ReadFile(sourcePath)
				if err != nil {
					return fmt.Errorf("failed to read helm binary: %w", err)
				}

				if err = os.WriteFile(destPath, input, 0755); err != nil {
					return fmt.Errorf("failed to install helm: %w", err)
				}

				// Verify installation
				cmd = exec.CommandContext(ctx, destPath, "version")
				out, err = cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("helm installation verification failed: %w", err)
				}

				self.logProgress(dependencyName, 0.04, fmt.Sprintf("Helm successfully installed: %s", strings.TrimSpace(string(out))), nil, StatusInstalling)

				// Set helm path for later use
				os.Setenv("HELM_PATH", destPath)

				return nil
			},
		},
		{
			Description: "Installing Helm Diff Plugin",
			Progress:    0.045,
			Action: func(ctx context.Context) error {
				// Get helm path (if we installed it) or use "helm" command
				helmPath := os.Getenv("HELM_PATH")
				if helmPath == "" {
					helmPath = "helm"
				}

				// Check if diff plugin is already installed
				cmd := exec.CommandContext(ctx, helmPath, "plugin", "list")
				out, err := cmd.CombinedOutput()

				if err == nil && strings.Contains(string(out), "diff") {
					msg := "Helm diff plugin is already installed"
					self.logProgress(dependencyName, 0.046, msg, nil, StatusInstalling)
					return nil
				}

				self.logProgress(dependencyName, 0.046, "Helm diff plugin not found, installing...", nil, StatusInstalling)

				// Install the diff plugin
				cmd = exec.CommandContext(ctx, helmPath, "plugin", "install", "https://github.com/databus23/helm-diff")

				// Stream the output
				stdoutPipe, err := cmd.StdoutPipe()
				if err != nil {
					return fmt.Errorf("failed to create helm plugin stdout pipe: %w", err)
				}

				stderrPipe, err := cmd.StderrPipe()
				if err != nil {
					return fmt.Errorf("failed to create helm plugin stderr pipe: %w", err)
				}

				if err := cmd.Start(); err != nil {
					return fmt.Errorf("failed to start helm plugin install: %w", err)
				}

				// Stream stdout
				go func() {
					scanner := bufio.NewScanner(stdoutPipe)
					for scanner.Scan() {
						self.sendLog("helm plugin: " + scanner.Text())
					}
				}()

				// Stream stderr
				go func() {
					scanner := bufio.NewScanner(stderrPipe)
					for scanner.Scan() {
						self.sendLog("helm plugin error: " + scanner.Text())
					}
				}()

				if err := cmd.Wait(); err != nil {
					return fmt.Errorf("failed to install helm diff plugin: %w", err)
				}

				// Verify installation
				cmd = exec.CommandContext(ctx, helmPath, "plugin", "list")
				out, err = cmd.CombinedOutput()
				if err != nil || !strings.Contains(string(out), "diff") {
					return fmt.Errorf("helm diff plugin installation verification failed: %w", err)
				}

				self.logProgress(dependencyName, 0.049, "Helm diff plugin successfully installed", nil, StatusInstalling)
				return nil
			},
		},
		{
			Description: "Installing Helmfile",
			Progress:    0.05,
			Action: func(ctx context.Context) error {
				// Check if helmfile is already installed
				cmd := exec.CommandContext(ctx, "helmfile", "--version")
				out, err := cmd.CombinedOutput()
				if err == nil {
					msg := fmt.Sprintf("Helmfile is already installed: %s", strings.TrimSpace(string(out)))
					self.logProgress(dependencyName, 0.05, msg, nil, StatusInstalling)
					return nil
				}

				self.logProgress(dependencyName, 0.05, "Helmfile not found, installing...", nil, StatusInstalling)

				// Create temp directory for download
				tempDir, err := os.MkdirTemp("", "helmfile-*")
				if err != nil {
					return fmt.Errorf("failed to create temp directory: %w", err)
				}
				defer os.RemoveAll(tempDir)

				// Determine OS and architecture
				goos := runtime.GOOS
				goarch := runtime.GOARCH
				version := "0.171.0"

				// Download URL
				url := fmt.Sprintf("https://github.com/helmfile/helmfile/releases/download/v%s/helmfile_%s_%s_%s.tar.gz",
					version, version, goos, goarch)

				self.logProgress(dependencyName, 0.06, fmt.Sprintf("Downloading Helmfile from %s", url), nil, StatusInstalling)

				// Download helmfile
				tarPath := filepath.Join(tempDir, "helmfile.tar.gz")
				if err := self.downloadFileWithProgress(url, tarPath); err != nil {
					return fmt.Errorf("failed to download helmfile: %w", err)
				}

				self.logProgress(dependencyName, 0.07, "Extracting Helmfile", nil, StatusInstalling)

				// Extract the file
				cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("failed to extract helmfile: %w, output: %s", err, string(out))
				}

				// Find an appropriate bin directory
				binPath := "/usr/local/bin"
				self.logProgress(dependencyName, 0.08, "Checking installation directory", nil, StatusInstalling)

				if !canWriteToDir(binPath) {
					// Try user's local bin directory instead
					home, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get home directory: %w", err)
					}
					binPath = filepath.Join(home, ".local", "bin")
					if err := os.MkdirAll(binPath, 0755); err != nil {
						return fmt.Errorf("failed to create bin directory: %w", err)
					}

					// Make sure the bin directory is in PATH
					if !strings.Contains(os.Getenv("PATH"), binPath) {
						self.sendLog(fmt.Sprintf("Note: Make sure to add %s to your PATH", binPath))
					}
				}

				self.logProgress(dependencyName, 0.09, fmt.Sprintf("Installing Helmfile to %s", binPath), nil, StatusInstalling)

				// Copy the binary
				sourcePath := filepath.Join(tempDir, "helmfile")
				destPath := filepath.Join(binPath, "helmfile")

				input, err := os.ReadFile(sourcePath)
				if err != nil {
					return fmt.Errorf("failed to read helmfile: %w", err)
				}

				if err = os.WriteFile(destPath, input, 0755); err != nil {
					return fmt.Errorf("failed to install helmfile: %w", err)
				}

				// Verify installation
				cmd = exec.CommandContext(ctx, destPath, "--version")
				out, err = cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("helmfile installation verification failed: %w", err)
				}

				self.logProgress(dependencyName, 0.10, fmt.Sprintf("Helmfile successfully installed: %s", strings.TrimSpace(string(out))), nil, StatusInstalling)

				// Set helmfile path for later use
				os.Setenv("HELMFILE_PATH", destPath)

				return nil
			},
		},
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
				// Get helmfile path (if we installed it) or use "helmfile" command
				helmfilePath := os.Getenv("HELMFILE_PATH")
				if helmfilePath == "" {
					helmfilePath = "helmfile"
				}

				// Ensure HELM_PATH is in the environment if we installed it
				if helmPath := os.Getenv("HELM_PATH"); helmPath != "" {
					// Make helm executable available to helmfile by putting it in the PATH
					os.Setenv("PATH", filepath.Dir(helmPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
				}

				// Construct arguments for helmfile command
				args := []string{
					"--file", filepath.Join(repoDir, "helmfile.yaml"),
					"--state-values-set", "baseDomain=" + opts.BaseDomain,
				}

				// Add any additional values if present
				for key, value := range opts.AdditionalValues {
					args = append(args, "--state-values-set", fmt.Sprintf("%s=%v", key, value))
				}

				// Add final "sync" command
				args = append(args, "sync")

				self.logProgress(dependencyName, 0.31, fmt.Sprintf("Starting helmfile sync with baseDomain=%s", opts.BaseDomain), nil, StatusInstalling)

				// Set up the command to run helmfile sync
				cmd := exec.CommandContext(ctx, helmfilePath, args...)
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

				// Clear the environment variables we set
				os.Unsetenv("HELMFILE_PATH")
				os.Unsetenv("HELM_PATH")

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
