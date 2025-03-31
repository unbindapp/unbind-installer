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
func (i *Installer) Install() error {
	// Keep the incorrect flag format for testing error handling
	k3sInstallFlags := "--flannel-backend=none --disable-kube-proxy --disable=servicelb --disable-network-policy --disable-traefik"

	// Log the start of installation
	i.log("Installing K3S...")

	// Download K3S installation script
	i.log("Downloading K3S installation script...")
	downloadCmd := exec.Command("curl", "-sfL", "https://get.k3s.io", "-o", "/tmp/k3s-installer.sh")
	downloadOutput, err := downloadCmd.CombinedOutput()
	if err != nil {
		i.log(fmt.Sprintf("Error downloading K3S installer: %s", string(downloadOutput)))
		return fmt.Errorf("failed to download K3S installer: %w", err)
	}

	// Make the installer executable
	i.log("Setting execution permissions on installer script...")
	chmodCmd := exec.Command("chmod", "+x", "/tmp/k3s-installer.sh")
	chmodOutput, err := chmodCmd.CombinedOutput()
	if err != nil {
		i.log(fmt.Sprintf("Error setting permissions: %s", string(chmodOutput)))
		return fmt.Errorf("failed to set installer permissions: %w", err)
	}

	// Run the K3S installer
	i.log(fmt.Sprintf("Running K3S installer with flags: %s", k3sInstallFlags))
	installCmd := exec.Command("/bin/sh", "/tmp/k3s-installer.sh")
	installCmd.Env = append(os.Environ(), fmt.Sprintf("INSTALL_K3S_EXEC=%s", k3sInstallFlags))
	installOutput, err := installCmd.CombinedOutput()
	installOutputStr := string(installOutput)
	i.log(fmt.Sprintf("Installation output: %s", installOutputStr))

	// Check for service failures in the installer output regardless of the err value
	// This is important because the script can exit successfully while the service fails
	if strings.Contains(installOutputStr, "Job for k3s.service failed") {
		i.log("Detected k3s.service failure in the installation output")

		// Collect detailed service diagnostics
		serviceError := i.collectServiceDiagnostics()

		// Return the error from the service failure
		return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
	}

	// Even if the installer script didn't explicitly report a service failure,
	// we should check if the service started properly
	serviceStatus, statusErr := i.checkServiceStatus()
	if statusErr != nil || serviceStatus != "active" {
		i.log(fmt.Sprintf("K3S service is not active after installation (status: %s)", serviceStatus))

		// Collect detailed service diagnostics
		serviceError := i.collectServiceDiagnostics()

		// Return the error from the service failure
		return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
	}

	// Explicitly check for any error messages in the systemd journal that might indicate a problem
	journalHasErrors, _ := i.checkJournalForErrors()
	if journalHasErrors {
		i.log("Detected errors in K3S service journal")

		// Collect detailed service diagnostics
		serviceError := i.collectServiceDiagnostics()

		// Return the error from the service failure
		return fmt.Errorf("K3S service has errors after installation: %w", serviceError)
	}

	// Continue with waiting for K3S to be fully ready
	return i.waitForK3SAndVerify()
}

// checkServiceStatus checks if the K3S service is active
func (i *Installer) checkServiceStatus() (string, error) {
	checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
	statusOutput, err := checkCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(statusOutput))

	return statusStr, err
}

// checkJournalForErrors looks for error messages in the K3S service journal
func (i *Installer) checkJournalForErrors() (bool, error) {
	// Look for error and fatal level messages in the journal
	journalCmd := exec.Command("journalctl", "-n", "50", "-u", "k3s.service", "--grep=error|fatal")
	journalOutput, err := journalCmd.CombinedOutput()
	journalOutputStr := strings.TrimSpace(string(journalOutput))

	// If there are any entries, we found errors
	return len(journalOutputStr) > 0, err
}

// collectServiceDiagnostics collects detailed diagnostics about the K3S service
func (i *Installer) collectServiceDiagnostics() error {
	// Get detailed service status
	statusCmd := exec.Command("systemctl", "status", "-l", "k3s.service")
	statusOutput, _ := statusCmd.CombinedOutput()
	i.log(fmt.Sprintf("K3S service status: %s", string(statusOutput)))

	// Get journal logs with priority (error and above)
	journalCmd := exec.Command("journalctl", "-n", "50", "-p", "err", "-u", "k3s.service", "-l")
	journalOutput, _ := journalCmd.CombinedOutput()
	i.log(fmt.Sprintf("K3S service errors from journal: %s", string(journalOutput)))

	// Get all recent journal logs for the service
	allLogsCmd := exec.Command("journalctl", "-n", "100", "-u", "k3s.service", "-l")
	allLogsOutput, _ := allLogsCmd.CombinedOutput()
	i.log(fmt.Sprintf("Recent K3S service logs: %s", string(allLogsOutput)))

	// Check if the service is failed
	isFailedCmd := exec.Command("systemctl", "is-failed", "k3s.service")
	isFailedOutput, _ := isFailedCmd.CombinedOutput()
	isFailedStr := strings.TrimSpace(string(isFailedOutput))

	if isFailedStr == "failed" {
		return fmt.Errorf("K3S service is in failed state")
	} else {
		return fmt.Errorf("K3S service is in abnormal state: %s", isFailedStr)
	}
}

// waitForK3SAndVerify waits for K3S to start and verifies the installation
func (i *Installer) waitForK3SAndVerify() error {
	// Wait longer for the service to be fully up
	i.log("Waiting for K3S service to start (this may take up to 30 seconds)...")

	// Try multiple times to check if the service is active
	maxRetries := 6
	for retry := 0; retry < maxRetries; retry++ {
		time.Sleep(5 * time.Second)

		i.log(fmt.Sprintf("Checking K3S service status (attempt %d/%d)...", retry+1, maxRetries))
		status, err := i.checkServiceStatus()

		if status == "active" {
			i.log("K3S service is now active")
			break
		}

		// On the last retry, or if we get a definitive "failed" status, collect detailed logs
		if retry == maxRetries-1 || status == "failed" {
			i.log(fmt.Sprintf("K3S service failed to become active (status: %s, error: %v)", status, err))

			// Collect detailed diagnostics
			serviceError := i.collectServiceDiagnostics()

			return fmt.Errorf("K3S service failed to become active after installation: %w", serviceError)
		}
	}

	// Wait for kubeconfig to be created
	i.log("Waiting for kubeconfig to be created...")
	kubeconfigPath := "/etc/rancher/k3s/k3s.yaml"

	maxKubeRetries := 6
	for retry := 0; retry < maxKubeRetries; retry++ {
		time.Sleep(5 * time.Second)

		i.log(fmt.Sprintf("Checking for kubeconfig (attempt %d/%d)...", retry+1, maxKubeRetries))
		if _, err := os.Stat(kubeconfigPath); err == nil {
			i.log("Kubeconfig file found")
			break
		}

		if retry == maxKubeRetries-1 {
			i.log("Kubeconfig file not created after multiple attempts")

			// Collect detailed diagnostics
			serviceError := i.collectServiceDiagnostics()

			return fmt.Errorf("K3S installed but kubeconfig not created: %w", serviceError)
		}
	}

	// Set KUBECONFIG environment variable
	i.log("Setting KUBECONFIG environment variable...")
	os.Setenv("KUBECONFIG", kubeconfigPath)

	// Verify k3s is working by checking nodes
	i.log("Verifying K3S installation by checking nodes...")

	// Retry kubectl command a few times in case the API server isn't fully up yet
	maxNodeRetries := 6
	for retry := 0; retry < maxNodeRetries; retry++ {
		time.Sleep(5 * time.Second)

		i.log(fmt.Sprintf("Checking K3S nodes (attempt %d/%d)...", retry+1, maxNodeRetries))
		kubeCmd := exec.Command("k3s", "kubectl", "get", "nodes")
		kubeOutput, err := kubeCmd.CombinedOutput()

		if err == nil {
			i.log(fmt.Sprintf("K3S nodes: %s", string(kubeOutput)))
			i.log("K3S installation verified successfully")
			return nil
		}

		if retry == maxNodeRetries-1 {
			i.log(fmt.Sprintf("Error checking K3S nodes: %s", string(kubeOutput)))

			// Collect detailed diagnostics
			serviceError := i.collectServiceDiagnostics()

			return fmt.Errorf("K3S installed but not functioning correctly: %w", serviceError)
		}
	}

	return nil
}

// log sends a message to the log channel if available
func (i *Installer) log(message string) {
	if i.LogChan != nil {
		i.LogChan <- message
	}
}
