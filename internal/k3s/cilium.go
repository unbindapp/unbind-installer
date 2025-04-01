package k3s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/unbindapp/unbind-installer/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// CiliumInstaller handles installation of Cilium CNI
type CiliumInstaller struct {
	// Channel to send log messages
	LogChan chan<- string
	// Kubeconfig path
	KubeconfigPath string
	// K8s client
	K8sClient *dynamic.DynamicClient
	// CIDR for the load balancer pool
	CIDR string
	// InternalIP for the Kubernetes API server
	InternalIP string
}

// NewCiliumInstaller creates a new Cilium Installer
func NewCiliumInstaller(logChan chan<- string, kubeConfig string, client *dynamic.DynamicClient, internalIP, cidr string) *CiliumInstaller {
	return &CiliumInstaller{
		LogChan:        logChan,
		KubeconfigPath: kubeConfig,
		K8sClient:      client,
		InternalIP:     internalIP,
		CIDR:           cidr,
	}
}

// log sends a message to the log channel if available
func (c *CiliumInstaller) log(message string) {
	if c.LogChan != nil {
		c.LogChan <- message
	}
}

// Install installs and configures Cilium
func (c *CiliumInstaller) Install() error {
	// Install Cilium CLI
	if err := c.installCiliumCLI(); err != nil {
		return fmt.Errorf("failed to install Cilium CLI: %w", err)
	}

	// Install Cilium
	if err := c.installCilium(); err != nil {
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
	resp, err := http.Get("https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt")
	if err != nil {
		return fmt.Errorf("failed to get Cilium CLI version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get Cilium CLI version, status code: %d", resp.StatusCode)
	}

	versionBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read Cilium CLI version: %w", err)
	}

	ciliumCliVersion := strings.TrimSpace(string(versionBytes))
	c.log(fmt.Sprintf("Found Cilium CLI version: %s", ciliumCliVersion))

	// Download Cilium CLI
	c.log("Downloading Cilium CLI...")
	downloadURL := fmt.Sprintf("https://github.com/cilium/cilium-cli/releases/download/%s/cilium-linux-%s.tar.gz", ciliumCliVersion, runtime.GOARCH)
	shaURL := downloadURL + ".sha256sum"

	// Download the tarball
	tarballPath := fmt.Sprintf("/tmp/cilium-linux-%s.tar.gz", runtime.GOARCH)
	err = utils.DownloadFile(tarballPath, downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download Cilium CLI: %w", err)
	}

	// Download the SHA256 checksum
	shaPath := tarballPath + ".sha256sum"
	err = utils.DownloadFile(shaPath, shaURL)
	if err != nil {
		return fmt.Errorf("failed to download SHA checksum: %w", err)
	}

	// Verify the checksum using native Go crypto functions
	c.log("Verifying checksum...")
	err = utils.VerifyChecksum(tarballPath, shaPath)
	if err != nil {
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
func (c *CiliumInstaller) installCilium() error {
	c.log("Installing Cilium...")

	// Build the install command
	installCmd := exec.Command(
		"cilium", "install",
		"--set", fmt.Sprintf("k8sServiceHost=%s", c.InternalIP),
		"--set", "k8sServicePort=6443",
		"--set", "kubeProxyReplacement=true",
		"--set", "ipam.operator.clusterPoolIPv4PodCIDRList=10.42.0.0/16",
		"--namespace", "kube-system",
	)

	// Set KUBECONFIG environment variable
	c.log(fmt.Sprintf("Using KUBECONFIG: %s", c.KubeconfigPath))
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

// configureLBIPPool configures the Cilium LoadBalancer IP pool using the DynamicClient
func (c *CiliumInstaller) configureLBIPPool() error {
	c.log(fmt.Sprintf("Configuring Cilium LoadBalancer IP pool for %s...", c.CIDR))

	// Define the resource
	poolResource := schema.GroupVersionResource{
		Group:    "cilium.io",
		Version:  "v2alpha1",
		Resource: "ciliumloadbalanceripools",
	}

	// Create the pool object
	pool := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cilium.io/v2alpha1",
			"kind":       "CiliumLoadBalancerIPPool",
			"metadata": map[string]interface{}{
				"name": "external",
			},
			"spec": map[string]interface{}{
				"blocks": []interface{}{
					map[string]interface{}{
						"cidr": c.CIDR,
					},
				},
			},
		},
	}

	ctx := context.Background()

	// Check if the resource already exists
	_, err := c.K8sClient.Resource(poolResource).Get(ctx, "external", metav1.GetOptions{})
	if err == nil {
		// Resource exists, update it
		c.log("LoadBalancer IP pool already exists, updating...")
		_, err = c.K8sClient.Resource(poolResource).Update(ctx, pool, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update LoadBalancer IP pool: %w", err)
		}
		c.log("LoadBalancer IP pool updated successfully")
	} else {
		// Resource doesn't exist, create it
		_, err = c.K8sClient.Resource(poolResource).Create(ctx, pool, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create LoadBalancer IP pool: %w", err)
		}
		c.log("LoadBalancer IP pool created successfully")
	}

	return nil
}
