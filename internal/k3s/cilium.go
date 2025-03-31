package k3s

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// CiliumInstaller handles installation of Cilium CNI
type CiliumInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
	// Path to the kubeconfig file
	KubeconfigPath string
}

// NewCiliumInstaller creates a new Cilium Installer
func NewCiliumInstaller(logChan chan<- string) *CiliumInstaller {
	return &CiliumInstaller{
		LogChan:        logChan,
		KubeconfigPath: "/etc/rancher/k3s/k3s.yaml",
	}
}

// SetKubeconfig sets a custom path for the kubeconfig file
func (c *CiliumInstaller) SetKubeconfig(path string) {
	c.KubeconfigPath = path
}

// Install installs and configures Cilium
func (c *CiliumInstaller) Install() error {
	// Ensure kubeconfig exists
	if _, err := os.Stat(c.KubeconfigPath); os.IsNotExist(err) {
		c.log("Kubeconfig file does not exist at: " + c.KubeconfigPath)
		return fmt.Errorf("kubeconfig file not found: %w", err)
	}

	// Set KUBECONFIG environment variable
	os.Setenv("KUBECONFIG", c.KubeconfigPath)

	// Get the internal IP to use for k8sServiceHost
	internalIP, err := c.getInternalIP()
	if err != nil {
		return fmt.Errorf("failed to get internal IP: %w", err)
	}
	c.log(fmt.Sprintf("Using internal IP: %s", internalIP))

	// Install Cilium CLI
	if err := c.installCiliumCLI(); err != nil {
		return fmt.Errorf("failed to install Cilium CLI: %w", err)
	}

	// Install Cilium
	if err := c.installCilium(internalIP); err != nil {
		return fmt.Errorf("failed to install Cilium: %w", err)
	}

	// Configure Cilium LoadBalancer IP pool
	if err := c.configureLBIPPool(); err != nil {
		return fmt.Errorf("failed to configure Cilium LoadBalancer IP pool: %w", err)
	}

	c.log("Cilium installation and configuration completed successfully")
	return nil
}

// installCiliumCLI installs the Cilium CLI tool
func (c *CiliumInstaller) installCiliumCLI() error {
	c.log("Installing Cilium CLI...")

	// Get the latest stable version
	c.log("Determining latest Cilium CLI version...")
	versionCmd := exec.Command("curl", "-s", "https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt")
	versionOutput, err := versionCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error getting Cilium CLI version: %s", string(versionOutput)))
		return fmt.Errorf("failed to get Cilium CLI version: %w", err)
	}
	ciliumCliVersion := strings.TrimSpace(string(versionOutput))
	c.log(fmt.Sprintf("Found Cilium CLI version: %s", ciliumCliVersion))

	// Determine architecture
	archCmd := exec.Command("uname", "-m")
	archOutput, err := archCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error determining architecture: %s", string(archOutput)))
		return fmt.Errorf("failed to determine architecture: %w", err)
	}

	cliArch := "amd64"
	if strings.TrimSpace(string(archOutput)) == "aarch64" {
		cliArch = "arm64"
	}
	c.log(fmt.Sprintf("Using architecture: %s", cliArch))

	// Download Cilium CLI
	c.log("Downloading Cilium CLI...")
	downloadURL := fmt.Sprintf("https://github.com/cilium/cilium-cli/releases/download/%s/cilium-linux-%s.tar.gz", ciliumCliVersion, cliArch)
	shaURL := downloadURL + ".sha256sum"

	// Download the tarball
	tarballPath := fmt.Sprintf("/tmp/cilium-linux-%s.tar.gz", cliArch)
	downloadCmd := exec.Command("curl", "-L", "--fail", "-o", tarballPath, downloadURL)
	downloadOutput, err := downloadCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error downloading Cilium CLI: %s", string(downloadOutput)))
		return fmt.Errorf("failed to download Cilium CLI: %w", err)
	}

	// Download the SHA256 checksum
	shaPath := tarballPath + ".sha256sum"
	shaCmd := exec.Command("curl", "-L", "--fail", "-o", shaPath, shaURL)
	shaOutput, err := shaCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error downloading SHA checksum: %s", string(shaOutput)))
		return fmt.Errorf("failed to download SHA checksum: %w", err)
	}

	// Verify the checksum
	c.log("Verifying checksum...")
	verifyCmd := exec.Command("sha256sum", "--check", shaPath)
	verifyCmd.Dir = "/tmp" // Set working directory to where the files are
	verifyOutput, err := verifyCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error verifying checksum: %s", string(verifyOutput)))
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Extract the CLI
	c.log("Extracting Cilium CLI...")
	extractCmd := exec.Command("tar", "xzvfC", tarballPath, "/usr/local/bin")
	extractOutput, err := extractCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error extracting Cilium CLI: %s", string(extractOutput)))
		return fmt.Errorf("failed to extract Cilium CLI: %w", err)
	}

	// Clean up downloaded files
	c.log("Cleaning up downloaded files...")
	os.Remove(tarballPath)
	os.Remove(shaPath)

	// Verify Cilium CLI works
	c.log("Verifying Cilium CLI installation...")
	versionCheckCmd := exec.Command("cilium", "version")
	versionCheckOutput, err := versionCheckCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error verifying Cilium CLI: %s", string(versionCheckOutput)))
		return fmt.Errorf("Cilium CLI verification failed: %w", err)
	}
	c.log(fmt.Sprintf("Cilium CLI installed successfully: %s", strings.TrimSpace(string(versionCheckOutput))))

	return nil
}

// installCilium installs Cilium with the required configuration
func (c *CiliumInstaller) installCilium(internalIP string) error {
	c.log("Installing Cilium...")

	// Build the install command
	installCmd := exec.Command(
		"cilium", "install",
		"--set", fmt.Sprintf("k8sServiceHost=%s", internalIP),
		"--set", "k8sServicePort=6443",
		"--set", "kubeProxyReplacement=true",
		"--set", "ipam.operator.clusterPoolIPv4PodCIDRList=10.42.0.0/16",
		"--namespace", "kube-system",
	)

	// Set KUBECONFIG environment variable
	installCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", c.KubeconfigPath))

	// Run the install command
	var outBuffer bytes.Buffer
	var errBuffer bytes.Buffer
	installCmd.Stdout = &outBuffer
	installCmd.Stderr = &errBuffer

	c.log(fmt.Sprintf("Running command: %s", installCmd.String()))
	err := installCmd.Run()
	stdout := outBuffer.String()
	stderr := errBuffer.String()

	// Log output regardless of success or failure
	if stdout != "" {
		c.log(fmt.Sprintf("Cilium install stdout: %s", stdout))
	}
	if stderr != "" {
		c.log(fmt.Sprintf("Cilium install stderr: %s", stderr))
	}

	if err != nil {
		return fmt.Errorf("cilium installation failed: %w", err)
	}

	// Wait for Cilium to be ready
	c.log("Waiting for Cilium to be ready...")
	statusCmd := exec.Command("cilium", "status", "--wait")
	statusCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", c.KubeconfigPath))

	var statusOutBuffer bytes.Buffer
	var statusErrBuffer bytes.Buffer
	statusCmd.Stdout = &statusOutBuffer
	statusCmd.Stderr = &statusErrBuffer

	statusErr := statusCmd.Run()
	statusStdout := statusOutBuffer.String()
	statusStderr := statusErrBuffer.String()

	if statusStdout != "" {
		c.log(fmt.Sprintf("Cilium status stdout: %s", statusStdout))
	}
	if statusStderr != "" {
		c.log(fmt.Sprintf("Cilium status stderr: %s", statusStderr))
	}

	if statusErr != nil {
		return fmt.Errorf("cilium status check failed: %w", statusErr)
	}

	c.log("Cilium installed successfully")
	return nil
}

// configureLBIPPool configures the Cilium LoadBalancer IP pool
func (c *CiliumInstaller) configureLBIPPool() error {
	c.log("Configuring Cilium LoadBalancer IP pool...")

	// Get network info
	primaryInterface, err := c.getPrimaryInterface()
	if err != nil {
		return fmt.Errorf("failed to determine primary interface: %w", err)
	}
	c.log(fmt.Sprintf("Using primary interface: %s", primaryInterface))

	// Get network CIDR for the primary interface
	networkCIDR, err := c.getNetworkCIDR(primaryInterface)
	if err != nil {
		return fmt.Errorf("failed to determine network CIDR: %w", err)
	}
	c.log(fmt.Sprintf("Using network CIDR: %s", networkCIDR))

	// Create Cilium LoadBalancer IP Pool configuration
	c.log("Creating Cilium LoadBalancer IP Pool...")
	ipPoolConfig := fmt.Sprintf(`apiVersion: "cilium.io/v2alpha1"
kind: CiliumLoadBalancerIPPool
metadata:
  name: "external"
spec:
  blocks:
  - cidr: "%s"
`, networkCIDR)

	// Write configuration to a temporary file
	tempFile, err := os.CreateTemp("", "cilium-lb-pool-*.yaml")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	if _, err := tempFile.Write([]byte(ipPoolConfig)); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}
	tempFile.Close()

	// Apply the configuration
	applyCmd := exec.Command("kubectl", "apply", "-f", tempFile.Name())
	applyCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", c.KubeconfigPath))
	applyOutput, err := applyCmd.CombinedOutput()
	if err != nil {
		c.log(fmt.Sprintf("Error configuring LoadBalancer IP pool: %s", string(applyOutput)))
		return fmt.Errorf("failed to configure LoadBalancer IP pool: %w", err)
	}

	c.log(fmt.Sprintf("LoadBalancer IP pool configured: %s", strings.TrimSpace(string(applyOutput))))
	return nil
}

// getInternalIP gets the internal IP of the machine
func (c *CiliumInstaller) getInternalIP() (string, error) {
	// Try to get the default interface used for routing
	primaryInterface, err := c.getPrimaryInterface()
	if err != nil {
		return "", fmt.Errorf("failed to determine primary interface: %w", err)
	}

	// Get the IP for this interface
	cmd := exec.Command("ip", "-o", "-4", "addr", "show", primaryInterface)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get IP information for interface %s: %w", primaryInterface, err)
	}

	// Extract the IP address using a regular expression
	re := regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return "", fmt.Errorf("no IP address found for interface %s", primaryInterface)
	}

	return matches[1], nil
}

// getPrimaryInterface gets the name of the primary network interface
func (c *CiliumInstaller) getPrimaryInterface() (string, error) {
	// Try to get the default interface used for routing
	routeCmd := exec.Command("ip", "route", "get", "1.1.1.1")
	routeOutput, err := routeCmd.CombinedOutput()
	if err == nil {
		re := regexp.MustCompile(`dev\s+(\S+)`)
		matches := re.FindStringSubmatch(string(routeOutput))
		if len(matches) >= 2 {
			return matches[1], nil
		}
	}

	// Fallback: get the first non-loopback interface
	ifaceCmd := exec.Command("ip", "-o", "-4", "addr", "show")
	ifaceOutput, err := ifaceCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to list network interfaces: %w", err)
	}

	// Extract interface names and find the first non-loopback
	lines := strings.Split(string(ifaceOutput), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "127.0.0.1") && strings.TrimSpace(line) != "" {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				// Extract interface name (remove trailing colon if present)
				iface := strings.TrimSuffix(fields[1], ":")
				return iface, nil
			}
		}
	}

	return "", fmt.Errorf("no suitable network interface found")
}

// getNetworkCIDR gets the network CIDR for the given interface
func (c *CiliumInstaller) getNetworkCIDR(iface string) (string, error) {
	cmd := exec.Command("ip", "-o", "-4", "addr", "show", iface)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get CIDR for interface %s: %w", iface, err)
	}

	// Extract the CIDR using a regular expression
	re := regexp.MustCompile(`inet\s+(\d+\.\d+\.\d+\.\d+/\d+)`)
	matches := re.FindStringSubmatch(string(output))
	if len(matches) < 2 {
		return "", fmt.Errorf("no CIDR found for interface %s", iface)
	}

	return matches[1], nil
}

// log sends a message to the log channel if available
func (c *CiliumInstaller) log(message string) {
	if c.LogChan != nil {
		c.LogChan <- message
	}
}
