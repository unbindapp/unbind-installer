package dependencies

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
				if err := cmd.Run(); err == nil {
					self.sendLog("Helmfile is already installed")
					return nil
				}

				self.sendLog("Helmfile not found, installing...")

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

				// Download helmfile
				tarPath := filepath.Join(tempDir, "helmfile.tar.gz")
				if err := downloadFile(url, tarPath); err != nil {
					return fmt.Errorf("failed to download helmfile: %w", err)
				}

				// Extract the file
				cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("failed to extract helmfile: %w", err)
				}

				// Move to /usr/local/bin or ~/.local/bin
				binPath := "/usr/local/bin"
				if _, err := os.Stat(binPath); os.IsNotExist(err) || !canWriteToDir(binPath) {
					// Try user's local bin directory instead
					home, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get home directory: %w", err)
					}
					binPath = filepath.Join(home, ".local", "bin")
					os.MkdirAll(binPath, 0755)
				}

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

				self.sendLog(fmt.Sprintf("Helmfile installed to %s", destPath))
				return nil
			},
		},
		{
			Description: "Creating temporary directory",
			Progress:    0.1,
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
			Progress:    0.2,
			Action: func(ctx context.Context) error {
				cmd := exec.CommandContext(ctx, "git", "clone", opts.RepoURL, repoDir)
				output, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("failed to clone repository: %w, output: %s", err, string(output))
				}
				self.sendLog(fmt.Sprintf("Repository cloned successfully to %s", repoDir))
				return nil
			},
		},
		{
			Description: "Running helmfile sync",
			Progress:    0.3,
			Action: func(ctx context.Context) error {
				// Set up the command to run helmfile sync
				cmd := exec.CommandContext(
					ctx,
					filepath.Join("/usr/local/bin", "helmfile"),
					"--file", filepath.Join(repoDir, "helmfile.yaml"),
					"--state-values-set", "globals.baseDomain="+opts.BaseDomain,
					"sync",
				)

				// Set the current working directory
				cmd.Dir = repoDir

				// Create a pipe for stdout/stderr
				stdout, err := cmd.StdoutPipe()
				if err != nil {
					return fmt.Errorf("failed to create stdout pipe: %w", err)
				}
				stderr, err := cmd.StderrPipe()
				if err != nil {
					return fmt.Errorf("failed to create stderr pipe: %w", err)
				}

				// Start the command
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("failed to start helmfile sync: %w", err)
				}

				// Set up a go routine to stream logs
				go func() {
					buffer := make([]byte, 4096)
					for {
						n, err := stdout.Read(buffer)
						if n > 0 {
							self.sendLog(string(buffer[:n]))
							// Update progress based on the output
							self.updateProgressBasedOnOutput(string(buffer[:n]))
						}
						if err != nil {
							break
						}
					}
				}()

				// Set up a go routine to stream errors
				go func() {
					buffer := make([]byte, 4096)
					for {
						n, err := stderr.Read(buffer)
						if n > 0 {
							self.sendLog("ERROR: " + string(buffer[:n]))
						}
						if err != nil {
							break
						}
					}
				}()

				// Wait for the command to complete
				syncStartTime := time.Now()
				err = cmd.Wait()
				syncDuration := time.Since(syncStartTime).Round(time.Second)

				if err != nil {
					return fmt.Errorf("helmfile sync failed after %v: %w", syncDuration, err)
				}

				self.logProgress("helmfile-sync", 0.9,
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
				return nil
			},
		},
	})
}

// Helper function to download a file
func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
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

// updateProgressBasedOnOutput updates the progress based on the output from helmfile
func (self *DependenciesManager) updateProgressBasedOnOutput(output string) {
	if containsString(output, "Processing releases") {
		self.updateProgress("helmfile-sync", "Processing releases", 0.4)
	} else if containsString(output, "Building dependency release") {
		self.updateProgress("helmfile-sync", "Building dependencies", 0.5)
	} else if containsString(output, "Installing release") {
		self.updateProgress("helmfile-sync", "Installing releases", 0.6)
	} else if containsString(output, "Upgrading release") {
		self.updateProgress("helmfile-sync", "Upgrading releases", 0.7)
	} else if containsString(output, "Release was successful") {
		self.updateProgress("helmfile-sync", "Release successful", 0.8)
	}
}

// containsString is a helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}
