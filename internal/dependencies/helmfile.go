package dependencies

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
func (self *DependenciesManager) SyncHelmfileWithSteps(ctx context.Context, opts SyncHelmfileOptions) error {
	var repoDir string

	// Set defaults if not provided
	if opts.RepoURL == "" {
		opts.RepoURL = "https://github.com/unbindapp/unbind-charts.git"
	}

	return self.InstallDependencyWithSteps(ctx, "helmfile-sync", []InstallationStep{
		{
			Description: "Installing Helmfile",
			Progress:    0.05,
			Action: func(ctx context.Context) error {
				// Check if helmfile is already installed
				cmd := exec.CommandContext(ctx, "helmfile", "--version")
				out, err := cmd.CombinedOutput()
				if err == nil {
					self.sendLog(fmt.Sprintf("Helmfile is already installed: %s", strings.TrimSpace(string(out))))
					return nil
				}

				self.logProgress("helmfile-sync", 0.05, "Helmfile not found, installing...")

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

				self.logProgress("helmfile-sync", 0.06, "Downloading Helmfile from %s", url)

				// Download helmfile
				tarPath := filepath.Join(tempDir, "helmfile.tar.gz")
				if err := self.downloadFileWithProgress(url, tarPath); err != nil {
					return fmt.Errorf("failed to download helmfile: %w", err)
				}

				self.logProgress("helmfile-sync", 0.07, "Extracting Helmfile")

				// Extract the file
				cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
				if out, err := cmd.CombinedOutput(); err != nil {
					return fmt.Errorf("failed to extract helmfile: %w, output: %s", err, string(out))
				}

				// Find an appropriate bin directory
				binPath := "/usr/local/bin"
				self.logProgress("helmfile-sync", 0.08, "Checking installation directory")

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

				self.logProgress("helmfile-sync", 0.09, "Installing Helmfile to %s", binPath)

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

				self.logProgress("helmfile-sync", 0.10, "Helmfile successfully installed: %s", strings.TrimSpace(string(out)))

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
				self.logProgress("helmfile-sync", 0.21, "Cloning from %s", opts.RepoURL)

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

				self.logProgress("helmfile-sync", 0.29, "Repository cloned successfully to %s", repoDir)
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

				// Construct arguments for helmfile command
				args := []string{
					"--file", filepath.Join(repoDir, "helmfile.yaml"),
					"--state-values-set", "globals.baseDomain=" + opts.BaseDomain,
				}

				// Add any additional values if present
				for key, value := range opts.AdditionalValues {
					args = append(args, "--state-values-set", fmt.Sprintf("%s=%v", key, value))
				}

				// Add final "sync" command
				args = append(args, "sync")

				self.logProgress("helmfile-sync", 0.31, "Starting helmfile sync with baseDomain=%s", opts.BaseDomain)

				// Set up the command to run helmfile sync
				cmd := exec.CommandContext(ctx, helmfilePath, args...)

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

				// Run scanners in separate goroutines
				go func() {
					for stdoutScanner.Scan() {
						line := stdoutScanner.Text()
						self.sendLog(line)
						self.updateProgressBasedOnOutput(line)
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
						self.updateProgressBasedOnOutput(line)
					}
				}()

				// Wait for the command to complete
				syncStartTime := time.Now()
				err = cmd.Wait()
				syncDuration := time.Since(syncStartTime).Round(time.Second)

				if err != nil {
					return fmt.Errorf("helmfile sync failed after %v: %w", syncDuration, err)
				}

				self.logProgress("helmfile-sync", 0.90,
					"Helmfile sync completed successfully in %v", syncDuration)
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
						self.sendLog(fmt.Sprintf("Warning: failed to clean up repository directory %s: %v", repoDir, err))
						// We'll consider this a non-fatal error
					} else {
						self.sendLog("Cleaned up temporary repository directory")
					}
				}

				// Clear the environment variable we set
				os.Unsetenv("HELMFILE_PATH")

				return nil
			},
		},
	})
}

// Helper function to download a file with progress updates
func (self *DependenciesManager) downloadFileWithProgress(url, filepath string) error {
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
		Manager:       self,
		Dependency:    "helmfile-sync",
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
	Manager       *DependenciesManager
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

	// Only update every 10% to avoid flooding
	if scaledProgress-wc.LastProgress >= 0.01 {
		wc.Manager.updateProgress(wc.Dependency, "Downloading Helmfile", scaledProgress)
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
func (self *DependenciesManager) updateProgressBasedOnOutput(output string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return
	}

	if containsString(output, "Building dependency release") {
		self.updateProgress("helmfile-sync", "Building dependencies", 0.40)
	} else if containsString(output, "Processing releases") {
		self.updateProgress("helmfile-sync", "Processing releases", 0.45)
	} else if containsString(output, "Fetching chart") {
		self.updateProgress("helmfile-sync", "Fetching charts", 0.50)
	} else if containsString(output, "Preparing chart") || containsString(output, "Preparing values") {
		self.updateProgress("helmfile-sync", "Preparing charts", 0.55)
	} else if containsString(output, "Starting diff") {
		self.updateProgress("helmfile-sync", "Comparing differences", 0.60)
	} else if containsString(output, "release=") && containsString(output, "installed") {
		self.updateProgress("helmfile-sync", "Installing releases", 0.70)
	} else if containsString(output, "release=") && containsString(output, "upgraded") {
		self.updateProgress("helmfile-sync", "Upgrading releases", 0.75)
	} else if containsString(output, "Release was successful") {
		self.updateProgress("helmfile-sync", "Release successful", 0.80)
	} else if containsString(output, "release=") && containsString(output, "skipped") {
		self.updateProgress("helmfile-sync", "Skipping unchanged releases", 0.85)
	}
}

// containsString is a helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
