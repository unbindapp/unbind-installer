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
	// Define K3S installation flags
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

	// Even if the installer completes successfully, the service might fail to start
	// Check for specific failure messages in the output
	if strings.Contains(installOutputStr, "Job for k3s.service failed") {
		i.log("Detected k3s.service failure in the installation output")

		// Get detailed service status
		statusCmd := exec.Command("systemctl", "status", "k3s.service")
		statusOutput, _ := statusCmd.CombinedOutput()
		i.log(fmt.Sprintf("K3S service status: %s", string(statusOutput)))

		// Get journal logs
		journalCmd := exec.Command("journalctl", "-n", "20", "-u", "k3s.service")
		journalOutput, _ := journalCmd.CombinedOutput()
		i.log(fmt.Sprintf("K3S service logs: %s", string(journalOutput)))

		return fmt.Errorf("K3S service failed to start after installation")
	}

	if err != nil {
		i.log(fmt.Sprintf("Error installing K3S: %s", installOutputStr))
		return fmt.Errorf("failed to install K3S: %w", err)
	}

	// Wait longer for the service to be fully up
	i.log("Waiting for K3S service to start (this may take up to 30 seconds)...")

	// Try multiple times to check if the service is active
	maxRetries := 6
	for retry := 0; retry < maxRetries; retry++ {
		time.Sleep(5 * time.Second)

		i.log(fmt.Sprintf("Checking K3S service status (attempt %d/%d)...", retry+1, maxRetries))
		checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
		statusOutput, _ := checkCmd.CombinedOutput()
		statusStr := strings.TrimSpace(string(statusOutput))

		if statusStr == "active" {
			i.log("K3S service is now active")
			break
		}

		if retry == maxRetries-1 {
			i.log("K3S service failed to become active after multiple attempts")

			// Get detailed service status
			statusCmd := exec.Command("systemctl", "status", "k3s.service")
			statusOutput, _ := statusCmd.CombinedOutput()
			i.log(fmt.Sprintf("K3S service status: %s", string(statusOutput)))

			// Get journal logs
			journalCmd := exec.Command("journalctl", "-n", "20", "-u", "k3s.service")
			journalOutput, _ := journalCmd.CombinedOutput()
			i.log(fmt.Sprintf("K3S service logs: %s", string(journalOutput)))

			return fmt.Errorf("K3S service failed to become active after installation")
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
			return fmt.Errorf("K3S installed but kubeconfig not created")
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
			return fmt.Errorf("K3S installed but not functioning correctly: %w", err)
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
