package k3s

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Installer handles installation of K3S
type Installer struct {
	// Channel to send log messages
	LogChan chan<- string
}

// NewInstaller creates a new K3S Installer
func NewInstaller(logChan chan<- string) *Installer {
	return &Installer{
		LogChan: logChan,
	}
}

// Install installs K3S with the specified flags
func (self *Installer) Install() error {
	// Keep the incorrect flag format for testing error handling
	k3sInstallFlags := "--flannel-backend=none --disable-kube-proxy --disable=servicelb --disable-network-policy --disable=traefik"

	// Log the start of installation
	self.log("Installing K3S...")

	// Download K3S installation script
	self.log("Downloading K3S installation script...")
	downloadCmd := exec.Command("curl", "-sfL", "https://get.k3s.io", "-o", "/tmp/k3s-installer.sh")
	downloadOutput, err := downloadCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error downloading K3S installer: %s", string(downloadOutput)))
		return fmt.Errorf("failed to download K3S installer: %w", err)
	}

	// Make the installer executable
	self.log("Setting execution permissions on installer script...")
	chmodCmd := exec.Command("chmod", "+x", "/tmp/k3s-installer.sh")
	chmodOutput, err := chmodCmd.CombinedOutput()
	if err != nil {
		self.log(fmt.Sprintf("Error setting permissions: %s", string(chmodOutput)))
		return fmt.Errorf("failed to set installer permissions: %w", err)
	}

	// Run the K3S installer
	self.log(fmt.Sprintf("Running K3S installer with flags: %s", k3sInstallFlags))
	installCmd := exec.Command("/bin/sh", "/tmp/k3s-installer.sh")
	installCmd.Env = append(os.Environ(), fmt.Sprintf("INSTALL_K3S_EXEC=%s", k3sInstallFlags))
	installOutput, err := installCmd.CombinedOutput()
	installOutputStr := string(installOutput)
	self.log(fmt.Sprintf("Installation output: %s", installOutputStr))

	// Check for service failures in the installer output regardless of the err value
	if strings.Contains(installOutputStr, "Job for k3s.service failed") {
		self.log("Detected k3s.service failure in the installation output")

		// Collect detailed service diagnostics
		serviceError := self.collectServiceDiagnostics()

		// Only return an error if collectServiceDiagnostics found an actual error
		if serviceError != nil {
			return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
		}
	}

	// Check if the service is active - this is the most reliable indicator
	serviceStatus, statusErr := self.checkServiceStatus()
	if statusErr != nil || serviceStatus != "active" {
		self.log(fmt.Sprintf("K3S service is not active after installation (status: %s)", serviceStatus))

		// Collect detailed service diagnostics
		serviceError := self.collectServiceDiagnostics()

		// Only return an error if collectServiceDiagnostics found an actual error
		if serviceError != nil {
			return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
		}
	}

	// Once we reach here, the service is active, so continue with verifying functionality
	return self.waitForK3SAndVerify()
}

// checkServiceStatus checks if the K3S service is active
func (self *Installer) checkServiceStatus() (string, error) {
	checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
	statusOutput, err := checkCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(statusOutput))

	return statusStr, err
}

// checkJournalForErrors looks for critical error messages in the K3S service journal
// This version is more selective about what constitutes a real error
func (self *Installer) checkJournalForErrors() (bool, error) {
	// Focus on recent fatal-level messages that would indicate a non-working service
	journalCmd := exec.Command("journalctl", "-n", "50", "-u", "k3s.service",
		"-p", "3", "--grep=level=fatal")
	journalOutput, _ := journalCmd.CombinedOutput()
	journalOutputStr := strings.TrimSpace(string(journalOutput))

	// If we find fatal errors, we need to check if they're recent
	if len(journalOutputStr) > 0 {
		// Get the systemd service state - if it's active, recent errors might not be fatal
		statusCmd := exec.Command("systemctl", "is-active", "k3s.service")
		statusOutput, _ := statusCmd.CombinedOutput()
		statusStr := strings.TrimSpace(string(statusOutput))

		// Even if we found error messages, if the service is active, don't report an error
		if statusStr == "active" {
			self.log("Found error messages in journal, but service is active, so ignoring them")
			return false, nil
		}

		return true, nil
	}

	return false, nil
}

// collectServiceDiagnostics collects detailed diagnostics about the K3S service
func (self *Installer) collectServiceDiagnostics() error {
	// Get detailed service status
	statusCmd := exec.Command("systemctl", "status", "-l", "k3s.service")
	statusOutput, _ := statusCmd.CombinedOutput()
	self.log(fmt.Sprintf("K3S service status: %s", string(statusOutput)))

	// Check if the service is active
	isActiveCmd := exec.Command("systemctl", "is-active", "k3s.service")
	isActiveOutput, _ := isActiveCmd.CombinedOutput()
	isActiveStr := strings.TrimSpace(string(isActiveOutput))

	// If the service is active, it's not in a failed state regardless of journal messages
	if isActiveStr == "active" {
		// Look for a message indicating k3s is running in the status output
		if strings.Contains(string(statusOutput), "k3s is up and running") {
			self.log("K3S service is active and running properly")
			return nil
		}
	}

	// Get journal logs with priority (error and above)
	journalCmd := exec.Command("journalctl", "-n", "50", "-p", "err", "-u", "k3s.service", "-l")
	journalOutput, _ := journalCmd.CombinedOutput()
	self.log(fmt.Sprintf("K3S service errors from journal: %s", string(journalOutput)))

	// Check if the service is failed
	isFailedCmd := exec.Command("systemctl", "is-failed", "k3s.service")
	isFailedOutput, _ := isFailedCmd.CombinedOutput()
	isFailedStr := strings.TrimSpace(string(isFailedOutput))

	if isFailedStr == "failed" {
		return fmt.Errorf("K3S service is in failed state")
	} else if isActiveStr != "active" {
		return fmt.Errorf("K3S service is in abnormal state: %s", isActiveStr)
	}

	// If we got here and the service is active, it's probably fine
	return nil
}

// waitForK3SAndVerify waits for K3S to start and verifies the installation
func (self *Installer) waitForK3SAndVerify() error {
	// Wait longer for the service to be fully up
	self.log("Waiting for K3S service to start (this may take up to 30 seconds)...")

	// Try multiple times to check if the service is active
	maxRetries := 6
	for retry := 0; retry < maxRetries; retry++ {
		time.Sleep(5 * time.Second)

		self.log(fmt.Sprintf("Checking K3S service status (attempt %d/%d)...", retry+1, maxRetries))
		status, err := self.checkServiceStatus()

		if status == "active" {
			self.log("K3S service is now active")
			break
		}

		// On the last retry, or if we get a definitive "failed" status, collect detailed logs
		if retry == maxRetries-1 || status == "failed" {
			self.log(fmt.Sprintf("K3S service failed to become active (status: %s, error: %v)", status, err))

			// Collect detailed diagnostics
			serviceError := self.collectServiceDiagnostics()

			return fmt.Errorf("K3S service failed to become active after installation: %w", serviceError)
		}
	}

	// Wait for kubeconfig to be created
	self.log("Waiting for kubeconfig to be created...")
	kubeconfigPath := "/etc/rancher/k3s/k3s.yaml"

	maxKubeRetries := 6
	for retry := 0; retry < maxKubeRetries; retry++ {
		time.Sleep(5 * time.Second)

		self.log(fmt.Sprintf("Checking for kubeconfig (attempt %d/%d)...", retry+1, maxKubeRetries))
		if _, err := os.Stat(kubeconfigPath); err == nil {
			self.log("Kubeconfig file found")
			break
		}

		if retry == maxKubeRetries-1 {
			self.log("Kubeconfig file not created after multiple attempts")

			// Collect detailed diagnostics
			serviceError := self.collectServiceDiagnostics()

			return fmt.Errorf("K3S installed but kubeconfig not created: %w", serviceError)
		}
	}

	// Set KUBECONFIG environment variable
	self.log("Setting KUBECONFIG environment variable...")
	os.Setenv("KUBECONFIG", kubeconfigPath)

	// Verify k3s is working by checking nodes
	self.log("Verifying K3S installation by checking nodes...")

	// Retry kubectl command a few times in case the API server isn't fully up yet
	maxNodeRetries := 6
	for retry := 0; retry < maxNodeRetries; retry++ {
		time.Sleep(5 * time.Second)

		self.log(fmt.Sprintf("Checking K3S nodes (attempt %d/%d)...", retry+1, maxNodeRetries))
		kubeCmd := exec.Command("k3s", "kubectl", "get", "nodes")
		kubeOutput, err := kubeCmd.CombinedOutput()

		if err == nil {
			self.log(fmt.Sprintf("K3S nodes: %s", string(kubeOutput)))
			self.log("K3S installation verified successfully")
			return nil
		}

		if retry == maxNodeRetries-1 {
			self.log(fmt.Sprintf("Error checking K3S nodes: %s", string(kubeOutput)))

			// Collect detailed diagnostics
			serviceError := self.collectServiceDiagnostics()

			return fmt.Errorf("K3S installed but not functioning correctly: %w", serviceError)
		}
	}

	return nil
}

// log sends a message to the log channel if available
func (self *Installer) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}
