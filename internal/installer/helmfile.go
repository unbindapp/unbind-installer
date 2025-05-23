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

	// Registry configuration
	DisableRegistry  bool   // Whether to disable the local registry component
	RegistryUsername string // External registry username
	RegistryPassword string // External registry password
	RegistryHost     string // External registry host
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
				self.logProgress(dependencyName, 0.20, fmt.Sprintf("Preparing to clone from %s", opts.RepoURL), nil, StatusInstalling)

				// First check if git is installed
				checkCmd := exec.CommandContext(ctx, "git", "--version")
				if err := checkCmd.Run(); err != nil {
					return fmt.Errorf("git command not found, please install git: %w", err)
				}

				// Send explicit progress update that we're starting the clone
				self.logProgress(dependencyName, 0.21, "Initializing git clone...", nil, StatusInstalling)

				// Set up progress updates during clone
				cloneDone := make(chan struct{})
				go func() {
					ticker := time.NewTicker(150 * time.Millisecond) // Faster updates
					defer ticker.Stop()

					currentProgress := 0.21
					messages := []string{
						"Initializing git clone...",
						"Connecting to repository...",
						"Receiving objects...",
						"Resolving deltas...",
						"Checking out files...",
					}
					msgIndex := 0

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.28 {
								// Increase progress increment for faster movement
								currentProgress += 0.015

								// Rotate through different messages more frequently
								if currentProgress > 0.21 && msgIndex < len(messages)-1 &&
									currentProgress > 0.21+float64(msgIndex)*0.01 {
									msgIndex++
								}

								self.logProgress(dependencyName, currentProgress, messages[msgIndex], nil, StatusInstalling)
							}
						case <-cloneDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				// Use timeout to prevent hanging on slow connections
				cloneCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()

				cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth=1", opts.RepoURL, repoDir)

				// Stream the output
				stdoutPipe, err := cmd.StdoutPipe()
				if err != nil {
					close(cloneDone)
					return fmt.Errorf("failed to create git stdout pipe: %w", err)
				}

				stderrPipe, err := cmd.StderrPipe()
				if err != nil {
					close(cloneDone)
					return fmt.Errorf("failed to create git stderr pipe: %w", err)
				}

				// Add progress update before starting the command
				self.logProgress(dependencyName, 0.22, "Starting git clone operation...", nil, StatusInstalling)

				if err := cmd.Start(); err != nil {
					close(cloneDone)
					return fmt.Errorf("failed to start git clone: %w", err)
				}

				// Stream stdout
				go func() {
					scanner := bufio.NewScanner(stdoutPipe)
					for scanner.Scan() {
						line := scanner.Text()
						self.sendLog("git: " + line)

						// Use output to trigger progress updates
						if strings.Contains(line, "Receiving objects") {
							self.logProgress(dependencyName, 0.23, "Receiving objects from repository...", nil, StatusInstalling)
						} else if strings.Contains(line, "Resolving deltas") {
							self.logProgress(dependencyName, 0.25, "Resolving deltas...", nil, StatusInstalling)
						}
					}
				}()

				// Stream stderr
				go func() {
					scanner := bufio.NewScanner(stderrPipe)
					for scanner.Scan() {
						line := scanner.Text()
						self.sendLog("git: " + line)

						// Use output to trigger progress updates
						if strings.Contains(line, "Cloning into") {
							self.logProgress(dependencyName, 0.22, "Initializing repository...", nil, StatusInstalling)
						} else if strings.Contains(line, "remote:") {
							self.logProgress(dependencyName, 0.24, "Connected to remote repository...", nil, StatusInstalling)
						}
					}
				}()

				err = cmd.Wait()
				close(cloneDone) // Signal progress updates to stop

				if err != nil {
					return fmt.Errorf("failed to clone repository: %w", err)
				}

				// Signal cloning is complete with another explicit update
				self.logProgress(dependencyName, 0.28, "Repository cloned successfully", nil, StatusInstalling)

				// Signal moving to next step
				self.logProgress(dependencyName, 0.29, fmt.Sprintf("Preparing to run helmfile in %s", repoDir), nil, StatusInstalling)

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

				// Add registry configuration options
				if opts.DisableRegistry {
					// Configure external registry
					args = append(args, "--state-values-set", "externalRegistry.enabled=true")

					// Set host if provided, default to docker.io
					registryHost := "docker.io"
					if opts.RegistryHost != "" {
						registryHost = opts.RegistryHost
					}
					args = append(args, "--state-values-set", "externalRegistry.host="+registryHost)

					// Add registry credentials if using external registry
					if opts.RegistryUsername != "" {
						args = append(args, "--state-values-set", "externalRegistry.username="+opts.RegistryUsername)
					}

					if opts.RegistryPassword != "" {
						args = append(args, "--state-values-set", "externalRegistry.password="+opts.RegistryPassword)
					}
				} else {
					// Use self-hosted registry
					args = append(args, "--state-values-set", "externalRegistry.enabled=false")
				}

				// Add any additional values if present
				for key, value := range opts.AdditionalValues {
					args = append(args, "--state-values-set", fmt.Sprintf("%s=%v", key, value))
				}

				// Add final "sync" command
				args = append(args, "sync")

				// Immediately report that we're starting the helmfile sync
				self.logProgress(dependencyName, 0.30, "Preparing helmfile command...", nil, StatusInstalling)

				// Show progress as we prepare to run the command
				self.logProgress(dependencyName, 0.31, "Starting helmfile sync operation...", nil, StatusInstalling)

				// Set up the command to run helmfile sync
				cmd := exec.CommandContext(ctx, "helmfile", args...)
				cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", self.kubeConfigPath))

				// Start progress updates during wait
				waitDone := make(chan error, 1)

				// Define progress stages with meaningful descriptions
				type progressStage struct {
					progress float64
					message  string
				}

				stages := []progressStage{
					{0.35, "Initializing Kubernetes cluster..."},
					{0.40, "Verifying Kubernetes configuration..."},
					{0.45, "Preparing Helm charts..."},
					{0.50, "Installing Container Registry..."},
					{0.55, "Installing PostgreSQL Operator..."},
					{0.60, "Installing Valkey Cache..."},
					{0.65, "Installing Ingress Controller..."},
					{0.70, "Configuring Authentication Services..."},
					{0.75, "Installing Core Unbind Services..."},
					{0.80, "Configuring Network Policies..."},
					{0.85, "Finalizing Installation..."},
				}

				// Update progress during long-running operations
				go func() {
					stageIndex := 0

					// Set up a ticker for progress updates
					ticker := time.NewTicker(800 * time.Millisecond) // Slightly slower to allow more visible stages
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if stageIndex < len(stages) {
								// Send current stage update
								stage := stages[stageIndex]
								self.logProgress(dependencyName, stage.progress, stage.message, nil, StatusInstalling)

								// Advance to next stage after a delay
								stageIndex++
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
					close(waitDone)
					return fmt.Errorf("failed to create stdout pipe: %w", err)
				}

				stderrPipe, err := cmd.StderrPipe()
				if err != nil {
					close(waitDone)
					return fmt.Errorf("failed to create stderr pipe: %w", err)
				}

				// Start the command
				if err := cmd.Start(); err != nil {
					close(waitDone)
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

// updateProgressBasedOnOutput updates the progress based on the output line
func (self *UnbindInstaller) updateProgressBasedOnOutput(dependency string, output string) {
	// Match known output lines to update progress and description
	switch {
	case containsString(output, "Processing chart"):
		// Extract chart name if possible
		parts := strings.Split(output, "Processing chart ")
		chartName := "helm chart"
		if len(parts) > 1 {
			chartName = strings.TrimSpace(strings.Split(parts[1], " ")[0])
		}
		self.logProgress(dependency, 0.40, fmt.Sprintf("Processing %s", chartName), nil, StatusInstalling)

	case containsString(output, "Installing chart"):
		// Extract chart name if possible
		parts := strings.Split(output, "Installing chart ")
		chartName := "helm chart"
		if len(parts) > 1 {
			chartName = strings.TrimSpace(strings.Split(parts[1], " ")[0])
		}
		self.logProgress(dependency, 0.55, fmt.Sprintf("Installing %s", chartName), nil, StatusInstalling)

	case containsString(output, "Installing release"):
		// Extract release name if possible
		parts := strings.Split(output, "Installing release ")
		releaseName := "component"
		if len(parts) > 1 {
			releaseName = strings.TrimSpace(strings.Split(parts[1], " ")[0])
		}
		self.logProgress(dependency, 0.65, fmt.Sprintf("Installing %s", releaseName), nil, StatusInstalling)

	case containsString(output, "Building dependency release"):
		self.logProgress(dependency, 0.75, "Building dependency releases", nil, StatusInstalling)

	case containsString(output, "Upgrading release"):
		// Extract release name if possible
		parts := strings.Split(output, "Upgrading release ")
		releaseName := "component"
		if len(parts) > 1 {
			releaseName = strings.TrimSpace(strings.Split(parts[1], " ")[0])
		}
		self.logProgress(dependency, 0.80, fmt.Sprintf("Upgrading %s", releaseName), nil, StatusInstalling)

	case containsString(output, "UPDATED RELEASES:"):
		self.logProgress(dependency, 0.85, "Finalizing release updates", nil, StatusInstalling)

	case containsString(output, "Deleting release"):
		// Extract release name if possible
		parts := strings.Split(output, "Deleting release ")
		releaseName := "component"
		if len(parts) > 1 {
			releaseName = strings.TrimSpace(strings.Split(parts[1], " ")[0])
		}
		self.logProgress(dependency, 0.90, fmt.Sprintf("Cleaning up %s", releaseName), nil, StatusInstalling)

	case containsString(output, "DELETED RELEASES:") || containsString(output, "Listing releases matching"):
		self.logProgress(dependency, 0.95, "Verifying installation", nil, StatusInstalling)
	}
}

// containsString is a helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return strings.Contains(s, substr)
}
