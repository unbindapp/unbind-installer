package k3s

import (
	"fmt"
	"os"
	"os/exec"
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
	if err != nil {
		i.log(fmt.Sprintf("Error installing K3S: %s", string(installOutput)))
		return fmt.Errorf("failed to install K3S: %w", err)
	}

	i.log("K3S installed successfully")

	// Set KUBECONFIG environment variable
	i.log("Setting KUBECONFIG environment variable...")
	os.Setenv("KUBECONFIG", "/etc/rancher/k3s/k3s.yaml")

	// Wait for K3S to be ready
	i.log("Waiting for K3S to be ready...")
	time.Sleep(10 * time.Second)

	// Check if K3S is running
	i.log("Checking K3S status...")
	checkCmd := exec.Command("systemctl", "is-active", "k3s")
	checkOutput, err := checkCmd.CombinedOutput()
	if err != nil {
		i.log(fmt.Sprintf("K3S service might not be running: %s", string(checkOutput)))
		return fmt.Errorf("K3S service not active: %w", err)
	}

	i.log("K3S is running")
	return nil
}

// log sends a message to the log channel if available
func (i *Installer) log(message string) {
	if i.LogChan != nil {
		i.LogChan <- message
	}
}
