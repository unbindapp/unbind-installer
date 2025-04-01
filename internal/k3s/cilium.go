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
	"time"

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
	// Update message channel (using the same K3SUpdateMessage structure)
	UpdateChan chan<- K3SUpdateMessage
	// Kubeconfig path
	KubeconfigPath string
	// K8s client
	K8sClient *dynamic.DynamicClient
	// CIDR for the load balancer pool
	CIDR string
	// InternalIP for the Kubernetes API server
	InternalIP string
	// Installation state
	state struct {
		startTime   time.Time
		endTime     time.Time
		status      string
		lastMsg     K3SUpdateMessage
		stepHistory []string
	}
}

// NewCiliumInstaller creates a new Cilium Installer
func NewCiliumInstaller(logChan chan<- string, updateChan chan<- K3SUpdateMessage, kubeConfig string, client *dynamic.DynamicClient, internalIP, cidr string) *CiliumInstaller {
	inst := &CiliumInstaller{
		LogChan:        logChan,
		UpdateChan:     updateChan,
		KubeconfigPath: kubeConfig,
		K8sClient:      client,
		InternalIP:     internalIP,
		CIDR:           cidr,
	}

	// Initialize state
	inst.state.status = "pending"
	inst.state.stepHistory = []string{}

	return inst
}

// logProgress sends a log message, progress update, and update message
func (self *CiliumInstaller) logProgress(progress float64, status string, description string, err error) {
	// Send log message
	if description != "" {
		self.log(description)
	}

	// Update state if status changes
	if status != self.state.status {
		if status == "installing" && self.state.startTime.IsZero() {
			self.state.startTime = time.Now()
		} else if status == "completed" || status == "failed" {
			self.state.endTime = time.Now()
		}
		self.state.status = status
	}

	// Add to step history if it's a new step
	if description != "" && len(self.state.stepHistory) == 0 || self.state.stepHistory[len(self.state.stepHistory)-1] != description {
		self.state.stepHistory = append(self.state.stepHistory, description)
	}

	// Send detailed update message
	self.sendUpdateMessage(progress, status, description, err)
}

// log sends a message to the log channel if available
func (self *CiliumInstaller) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}

// sendUpdateMessage sends a detailed update message
func (self *CiliumInstaller) sendUpdateMessage(progress float64, status string, description string, err error) {
	msg := K3SUpdateMessage{
		Progress:    progress,
		Status:      status,
		Description: description,
		Error:       err,
		StartTime:   self.state.startTime,
		EndTime:     self.state.endTime,
		StepHistory: self.state.stepHistory,
	}

	self.state.lastMsg = msg

	if self.UpdateChan != nil {
		self.UpdateChan <- msg
	}
}

// Install installs and configures Cilium with progress tracking
func (self *CiliumInstaller) Install(ctx context.Context) error {
	// Start the installation process and initialize state
	self.state.startTime = time.Now()
	self.state.status = "installing"

	// Send initial status update
	self.logProgress(0.01, "installing", "Preparing Cilium installation...", nil)

	// Define the installation steps
	steps := []InstallationStep{
		{
			Description: "Determining latest Cilium CLI version",
			Progress:    0.10,
			Action: func(ctx context.Context) error {
				// Get the latest stable version
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
				self.log(fmt.Sprintf("Found Cilium CLI version: %s", ciliumCliVersion))
				return nil
			},
		},
		{
			Description: "Downloading Cilium CLI",
			Progress:    0.20,
			Action: func(ctx context.Context) error {
				// Get the latest stable version first (needed for URL construction)
				resp, err := http.Get("https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt")
				if err != nil {
					return fmt.Errorf("failed to get Cilium CLI version: %w", err)
				}
				defer resp.Body.Close()

				versionBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					return fmt.Errorf("failed to read Cilium CLI version: %w", err)
				}

				ciliumCliVersion := strings.TrimSpace(string(versionBytes))

				// Download Cilium CLI
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

				return nil
			},
		},
		{
			Description: "Verifying Cilium CLI checksum",
			Progress:    0.30,
			Action: func(ctx context.Context) error {
				tarballPath := fmt.Sprintf("/tmp/cilium-linux-%s.tar.gz", runtime.GOARCH)
				shaPath := tarballPath + ".sha256sum"

				// Verify the checksum
				err := utils.VerifyChecksum(tarballPath, shaPath)
				if err != nil {
					return fmt.Errorf("checksum verification failed: %w", err)
				}

				return nil
			},
		},
		{
			Description: "Extracting Cilium CLI",
			Progress:    0.40,
			Action: func(ctx context.Context) error {
				tarballPath := fmt.Sprintf("/tmp/cilium-linux-%s.tar.gz", runtime.GOARCH)

				// Extract the CLI
				extractCmd := exec.CommandContext(ctx, "tar", "xzvfC", tarballPath, "/usr/local/bin")
				extractOutput, err := extractCmd.CombinedOutput()
				if err != nil {
					self.log(fmt.Sprintf("Error extracting Cilium CLI: %s", string(extractOutput)))
					return fmt.Errorf("failed to extract Cilium CLI: %w", err)
				}

				// Clean up downloaded files
				os.Remove(tarballPath)
				os.Remove(tarballPath + ".sha256sum")

				// Verify Cilium CLI works
				versionCheckCmd := exec.CommandContext(ctx, "cilium", "version")
				versionCheckOutput, err := versionCheckCmd.CombinedOutput()
				if err != nil {
					self.log(fmt.Sprintf("Error verifying Cilium CLI: %s", string(versionCheckOutput)))
					return fmt.Errorf("Cilium CLI verification failed: %w", err)
				}
				self.log(fmt.Sprintf("Cilium CLI installed successfully: %s", strings.TrimSpace(string(versionCheckOutput))))

				return nil
			},
		},
		{
			Description: "Installing Cilium CNI",
			Progress:    0.50,
			Action: func(ctx context.Context) error {
				// Build the install command
				installCmd := exec.CommandContext(ctx,
					"cilium", "install",
					"--version", "v1.17.2",
					"--set", fmt.Sprintf("k8sServiceHost=%s", self.InternalIP),
					"--set", "k8sServicePort=6443",
					"--set", "kubeProxyReplacement=true",
					"--set", "ipam.operator.clusterPoolIPv4PodCIDRList=10.42.0.0/16",
					"--namespace", "kube-system",
				)

				// Set KUBECONFIG environment variable
				self.log(fmt.Sprintf("Using KUBECONFIG: %s", self.KubeconfigPath))
				installCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", self.KubeconfigPath))

				// Run the install command
				var outBuffer bytes.Buffer
				var errBuffer bytes.Buffer
				installCmd.Stdout = &outBuffer
				installCmd.Stderr = &errBuffer

				self.log(fmt.Sprintf("Running command: %s", installCmd.String()))

				// Start progress updates during installation
				installDone := make(chan error, 1)

				go func() {
					currentProgress := 0.50

					ticker := time.NewTicker(2 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.70 {
								currentProgress += 0.03
								self.logProgress(currentProgress, "installing", self.state.lastMsg.Description, nil)
							}
						case <-installDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				err := installCmd.Run()
				close(installDone)

				stdout := outBuffer.String()
				stderr := errBuffer.String()

				// Log output regardless of success or failure
				if stdout != "" {
					self.log(fmt.Sprintf("Cilium install stdout: %s", stdout))
				}
				if stderr != "" {
					self.log(fmt.Sprintf("Cilium install stderr: %s", stderr))
				}

				if err != nil {
					return fmt.Errorf("cilium installation failed: %w", err)
				}

				return nil
			},
		},
		{
			Description: "Waiting for Cilium to be ready",
			Progress:    0.75,
			Action: func(ctx context.Context) error {
				// Wait for Cilium to be ready
				statusCmd := exec.CommandContext(ctx, "cilium", "status", "--wait")
				statusCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", self.KubeconfigPath))

				var statusOutBuffer bytes.Buffer
				var statusErrBuffer bytes.Buffer
				statusCmd.Stdout = &statusOutBuffer
				statusCmd.Stderr = &statusErrBuffer

				// Start progress updates during wait
				waitStartTime := time.Now()
				waitDone := make(chan error, 1)

				go func() {
					currentProgress := 0.75

					ticker := time.NewTicker(2 * time.Second)
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							if currentProgress < 0.85 {
								currentProgress += 0.02
								elapsed := time.Since(waitStartTime).Round(time.Second)
								updatedDescription := fmt.Sprintf("Waiting for Cilium to be ready (elapsed: %v)...", elapsed)
								self.logProgress(currentProgress, "installing", updatedDescription, nil)
							}
						case <-waitDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				statusErr := statusCmd.Run()
				close(waitDone)

				statusStdout := statusOutBuffer.String()
				statusStderr := statusErrBuffer.String()

				if statusStdout != "" {
					self.log(fmt.Sprintf("Cilium status stdout: %s", statusStdout))
				}
				if statusStderr != "" {
					self.log(fmt.Sprintf("Cilium status stderr: %s", statusStderr))
				}

				if statusErr != nil {
					return fmt.Errorf("cilium status check failed: %w", statusErr)
				}

				self.log("Cilium installed successfully")
				return nil
			},
		},
		{
			Description: "Configuring Cilium LoadBalancer IP pool",
			Progress:    0.90,
			Action: func(ctx context.Context) error {
				self.log(fmt.Sprintf("Configuring Cilium LoadBalancer IP pool for %s...", self.CIDR))

				// Define the resource
				poolResource := schema.GroupVersionResource{
					Group:    "cilium.io",
					Version:  "v2alpha1",
					Resource: "ciliumloadbalancerippools",
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
									"cidr": self.CIDR,
								},
							},
						},
					},
				}

				// Check if the resource already exists
				_, err := self.K8sClient.Resource(poolResource).Get(ctx, "external", metav1.GetOptions{})
				if err == nil {
					// Resource exists, update it
					self.log("LoadBalancer IP pool already exists, updating...")
					_, err = self.K8sClient.Resource(poolResource).Update(ctx, pool, metav1.UpdateOptions{})
					if err != nil {
						return fmt.Errorf("failed to update LoadBalancer IP pool: %w", err)
					}
					self.log("LoadBalancer IP pool updated successfully")
				} else {
					// Resource doesn't exist, create it
					_, err = self.K8sClient.Resource(poolResource).Create(ctx, pool, metav1.CreateOptions{})
					if err != nil {
						return fmt.Errorf("failed to create LoadBalancer IP pool: %w", err)
					}
					self.log("LoadBalancer IP pool created successfully")
				}

				return nil
			},
		},
	}

	// Execute all installation steps
	for _, step := range steps {
		// Update progress for the current step
		self.logProgress(step.Progress, "installing", step.Description, nil)

		// Execute the step's action
		if err := step.Action(ctx); err != nil {
			// Set end time and send failure update
			self.state.endTime = time.Now()
			self.logProgress(step.Progress, "failed", fmt.Sprintf("Failed: %s", step.Description), err)
			return err
		}
	}

	// Set end time and send final progress update
	self.state.endTime = time.Now()
	self.logProgress(1.0, "completed", "Cilium installation completed successfully", nil)

	return nil
}

// GetLastUpdateMessage returns the most recent update message
func (self *CiliumInstaller) GetLastUpdateMessage() K3SUpdateMessage {
	return self.state.lastMsg
}

// GetInstallationState returns the current installation state
func (self *CiliumInstaller) GetInstallationState() (status string, startTime time.Time, endTime time.Time) {
	return self.state.status, self.state.startTime, self.state.endTime
}
