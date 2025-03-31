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

	if err != nil {
		i.log(fmt.Sprintf("Error installing K3S: %s", installOutputStr))
		return fmt.Errorf("failed to install K3S: %w", err)
	}

	// Check if the service is active
	i.log("Checking K3S service status...")
	time.Sleep(5 * time.Second) // Give the service time to start

	checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
	statusOutput, err := checkCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(statusOutput))

	if err != nil || statusStr != "active" {
		// If service is not active, check the service status for detailed error
		i.log("K3S service is not active, checking for errors...")

		journalCmd := exec.Command("journalctl", "-n", "20", "-u", "k3s.service")
		journalOutput, _ := journalCmd.CombinedOutput()
		i.log(fmt.Sprintf("K3S service logs: %s", string(journalOutput)))

		return fmt.Errorf("K3S service failed to start: %s", statusStr)
	}

	i.log("K3S installed and service is running successfully")

	// Set KUBECONFIG environment variable
	i.log("Setting KUBECONFIG environment variable...")
	os.Setenv("KUBECONFIG", "/etc/rancher/k3s/k3s.yaml")

	// Verify k3s is working by checking nodes
	i.log("Verifying K3S installation by checking nodes...")
	kubeCmd := exec.Command("k3s", "kubectl", "get", "nodes")
	kubeOutput, err := kubeCmd.CombinedOutput()

	if err != nil {
		i.log(fmt.Sprintf("Error checking K3S nodes: %s", string(kubeOutput)))
		return fmt.Errorf("K3S installed but not functioning correctly: %w", err)
	}

	i.log(fmt.Sprintf("K3S nodes: %s", string(kubeOutput)))
	i.log("K3S installation verified successfully")

	return nil
}

// log sends a message to the log channel if available
func (i *Installer) log(message string) {
	if i.LogChan != nil {
		i.LogChan <- message
	}
}
