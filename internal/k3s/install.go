package k3s

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Educational facts about the platform being installed
var platformFacts = []string{
	"Kubernetes (K8s) is an open-source system that keeps container-ized apps running and scales them automatically.",
	"An \"operator\" is a program that can extend the functionality of a Kubernetes cluster - like a plugin.",
	"K3s is a lightweight edition of Kubernetes packaged as a single small binary.",
	"K3s can run a full cluster on devices with as little as 512 MB of RAM, even a Raspberry Pi.",
	"K3s uses an embedded SQLite database by default, so no separate etcd cluster is required.",
	"K3s automatically creates and renews TLS certificates to keep cluster traffic encrypted.",
	"K3s supports both x86-64 and ARM CPUs, covering PCs, cloud VMs, and IoT boards.",
	"K3s runs containers with containerd instead of Docker for a lighter footprint.",
	"Kubernetes is \"self-healing\" - it will automatically restart failed containers or replace them with new ones.",
	"Helm is a package manager for Kubernetes that installs apps from \"helm charts.\"",
	"Helmfile groups multiple Helm charts so you can deploy an entire stack declaratively.",
	"Longhorn adds persistent, replicated block storage to your Kubernetes cluster.",
	"Longhorn provides a web UI for creating, resizing, and restoring storage volumes.",
	"A single command can install K3s, and the cluster typically starts in under 30 seconds.",
}

// FactRotator manages educational facts display without repetition
type FactRotator struct {
	facts     []string
	remaining []string
	current   int
}

// NewFactRotator creates a new fact rotator with shuffled facts
func NewFactRotator(facts []string) *FactRotator {
	rotator := &FactRotator{
		facts: make([]string, len(facts)),
	}
	copy(rotator.facts, facts)
	rotator.shuffle()
	return rotator
}

// shuffle randomizes the fact order
func (f *FactRotator) shuffle() {
	f.remaining = make([]string, len(f.facts))
	copy(f.remaining, f.facts)
	rand.Shuffle(len(f.remaining), func(i, j int) {
		f.remaining[i], f.remaining[j] = f.remaining[j], f.remaining[i]
	})
	f.current = 0
}

// GetNext returns the next fact, reshuffling when all facts are exhausted
func (f *FactRotator) GetNext() string {
	if f.current >= len(f.remaining) {
		f.shuffle() // Start over with a new shuffle
	}
	fact := f.remaining[f.current]
	f.current++
	return fact
}

// InstallationStep represents a single step in the k3s setup
type InstallationStep struct {
	Description string
	Progress    float64
	Action      func(context.Context) error
}

// K3SUpdateMessage contains progress info for the UI
type K3SUpdateMessage struct {
	Progress    float64   // Progress from 0.0 to 1.0
	Status      string    // Current status: "pending", "installing", "completed", "failed"
	Description string    // Current step description
	Error       error     // Error if any
	StartTime   time.Time // Start time of the installation
	EndTime     time.Time // End time of the installation
	StepHistory []string  // History of steps executed
}

// Installer manages k3s setup
type Installer struct {
	// Channel to send log messages
	LogChan chan<- string
	// Update message channel
	UpdateChan chan<- K3SUpdateMessage
	// Fact message channel
	FactChan chan<- string
	// Installation state
	state struct {
		startTime time.Time
		endTime   time.Time
		status    string
		lastMsg   K3SUpdateMessage
	}
	// Fact rotator for educational information
	factRotator *FactRotator
}

// NewInstaller creates an installer instance
func NewInstaller(logChan chan<- string, updateChan chan<- K3SUpdateMessage, factChan chan<- string) *Installer {
	inst := &Installer{
		LogChan:    logChan,
		UpdateChan: updateChan,
		FactChan:   factChan,
	}

	// Initialize state
	inst.state.status = "pending"

	// Initialize fact rotator
	inst.factRotator = NewFactRotator(platformFacts)

	return inst
}

// Last time we sent a progress update
var lastProgressUpdateTime time.Time
var minProgressInterval = 50 * time.Millisecond // Reduced interval for smoother updates

// logProgress tracks install progress
func (self *Installer) logProgress(progress float64, status string, description string, err error) {
	// Send log message only if it's a new description
	if description != "" && description != self.state.lastMsg.Description {
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

	// Always update step history if it's a new description
	if description != "" && (len(self.state.lastMsg.StepHistory) == 0 ||
		self.state.lastMsg.StepHistory[len(self.state.lastMsg.StepHistory)-1] != description) {
		self.state.lastMsg.StepHistory = append(self.state.lastMsg.StepHistory, description)
	}

	// We'll continue to track all updates in the local state
	// but only send significant ones to the UI
	self.sendUpdateMessage(progress, status, description, err)
}

// log outputs a message to the channel
func (self *Installer) log(message string) {
	if self.LogChan != nil {
		self.LogChan <- message
	}
}

// sendUpdateMessage pushes progress updates to UI
func (self *Installer) sendUpdateMessage(progress float64, status string, description string, err error) {
	msg := K3SUpdateMessage{
		Progress:    progress,
		Status:      status,
		Description: description,
		Error:       err,
		StartTime:   self.state.startTime,
		EndTime:     self.state.endTime,
		StepHistory: make([]string, len(self.state.lastMsg.StepHistory)),
	}

	// Make a copy of the step history to avoid mutation issues
	copy(msg.StepHistory, self.state.lastMsg.StepHistory)

	// Always update local state
	self.state.lastMsg = msg

	// Send to update channel immediately without throttling
	if self.UpdateChan != nil {
		select {
		case self.UpdateChan <- msg:
			// Message sent successfully
		default:
			// Channel is full, log it but don't block
			self.log("Warning: Progress update channel is full")
		}
	}
}

// sendFact sends an educational fact to the UI
func (self *Installer) sendFact(fact string) {
	if self.FactChan != nil {
		select {
		case self.FactChan <- fact:
			// Fact sent successfully
		default:
			// Channel is full, skip this fact
		}
	}
}

// Install sets up k3s and returns the kubeconfig path
func (self *Installer) Install(ctx context.Context) (string, error) {
	k3sInstallFlags := "--disable=traefik --disable=local-storage --kubelet-arg=fail-swap-on=false --cluster-init " +
		"--kubelet-arg=system-reserved=memory=512Mi,cpu=300m " +
		"--kubelet-arg=kube-reserved=memory=256Mi,cpu=200m " +
		"--kubelet-arg=eviction-soft-grace-period=memory.available=30s " +
		"--kubelet-arg=eviction-minimum-reclaim=memory.available=100Mi " +
		"--kubelet-arg=eviction-max-pod-grace-period=30 " +
		"--kubelet-arg=eviction-pressure-transition-period=30s " +
		"--kubelet-arg=feature-gates=MemoryQoS=true"

	var kubeconfigPath string

	// Start the installation process and initialize state
	self.state.startTime = time.Now()
	self.state.status = "installing"

	// Send initial status update
	self.logProgress(0.01, "installing", "Preparing K3S installation...", nil)

	// Define the installation steps
	steps := []InstallationStep{
		{
			Description: "Setting system file limits",
			Progress:    0.02,
			Action: func(ctx context.Context) error {
				// Set system-wide limits
				self.log("Setting system file limits...")

				// Set system-wide limits via sysctl
				sysctlContent := `fs.file-max = 65535
fs.inotify.max_user_watches = 2099999999
fs.inotify.max_user_instances = 2099999999`

				if err := os.WriteFile("/etc/sysctl.d/99-k3s-file-limits.conf", []byte(sysctlContent), 0644); err != nil {
					self.log(fmt.Sprintf("Warning: Could not write sysctl config: %v", err))
				}
				// Apply sysctl settings
				cmd := exec.CommandContext(ctx, "sysctl", "--system")
				if output, err := cmd.CombinedOutput(); err != nil {
					self.log(fmt.Sprintf("Warning: Could not apply sysctl settings: %v, output: %s", err, string(output)))
				}

				return nil
			},
		},
		{
			Description: "Downloading K3S installation script",
			Progress:    0.05,
			Action: func(ctx context.Context) error {
				self.log("Starting download of K3S installer script...")
				downloadCmd := exec.CommandContext(ctx, "curl", "-sfL", "https://get.k3s.io", "-o", "/tmp/k3s-installer.sh")
				downloadOutput, err := downloadCmd.CombinedOutput()
				if err != nil {
					errMsg := fmt.Sprintf("Error downloading K3S installer: %s", string(downloadOutput))
					self.log(errMsg)
					return fmt.Errorf("failed to download K3S installer: %w", err)
				}

				return nil
			},
		},
		{
			Description: "Setting execution permissions on installer script",
			Progress:    0.08,
			Action: func(ctx context.Context) error {
				chmodCmd := exec.CommandContext(ctx, "chmod", "+x", "/tmp/k3s-installer.sh")
				chmodOutput, err := chmodCmd.CombinedOutput()
				if err != nil {
					errMsg := fmt.Sprintf("Error setting permissions: %s", string(chmodOutput))
					self.log(errMsg)
					return fmt.Errorf("failed to set installer permissions: %w", err)
				}

				return nil
			},
		},
		{
			Description: "Running K3S installer",
			Progress:    0.35, // Much larger allocation since this takes 2-3 minutes
			Action: func(ctx context.Context) error {
				self.log(fmt.Sprintf("Running K3S installer with flags: %s", k3sInstallFlags))

				// Start a goroutine to show educational facts during installation
				factsDone := make(chan struct{})
				go func() {
					// Show first fact immediately
					fact := self.factRotator.GetNext()
					self.sendFact(fact)

					ticker := time.NewTicker(8 * time.Second) // Show a new fact every 8 seconds
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							fact := self.factRotator.GetNext()
							self.sendFact(fact)
						case <-factsDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				installCmd := exec.CommandContext(ctx, "/bin/sh", "/tmp/k3s-installer.sh")
				installCmd.Env = append(os.Environ(), fmt.Sprintf("INSTALL_K3S_EXEC=%s", k3sInstallFlags))

				installOutput, err := installCmd.CombinedOutput()
				close(factsDone) // Stop showing facts

				installOutputStr := string(installOutput)
				self.log(fmt.Sprintf("Installation output: %s", installOutputStr))

				if err != nil {
					return fmt.Errorf("K3S installation failed: %w", err)
				}

				if strings.Contains(installOutputStr, "Job for k3s.service failed") {
					self.log("Detected k3s.service failure in the installation output")

					serviceError := self.collectServiceDiagnostics()

					if serviceError != nil {
						return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Checking K3S service status",
			Progress:    0.40,
			Action: func(ctx context.Context) error {
				serviceStatus, statusErr := self.checkServiceStatus()
				if statusErr != nil || serviceStatus != "active" {
					errMsg := fmt.Sprintf("K3S service is not active after installation (status: %s)", serviceStatus)
					self.log(errMsg)

					serviceError := self.collectServiceDiagnostics()

					if serviceError != nil {
						return fmt.Errorf("K3S service failed to start after installation: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Waiting for K3S service to become fully active",
			Progress:    0.45,
			Action: func(ctx context.Context) error {
				maxRetries := 6
				for retry := 0; retry < maxRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking K3S service status (attempt %d/%d)...", retry+1, maxRetries))
					status, err := self.checkServiceStatus()

					if status == "active" {
						self.log("K3S service is now active")
						return nil
					}

					if retry == maxRetries-1 || status == "failed" {
						errMsg := fmt.Sprintf("K3S service failed to become active (status: %s, error: %v)", status, err)
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()
						return fmt.Errorf("K3S service failed to become active after installation: %w", serviceError)
					}
				}

				return nil
			},
		},
		{
			Description: "Installing Helm and dependencies",
			Progress:    0.65, // Larger allocation since this can take 1-2 minutes
			Action: func(ctx context.Context) error {
				// Start showing educational facts during Helm installation
				factsDone := make(chan struct{})
				go func() {
					// Show first fact immediately
					fact := self.factRotator.GetNext()
					self.sendFact(fact)

					ticker := time.NewTicker(5 * time.Second) // Show a new fact every 5 seconds
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							fact := self.factRotator.GetNext()
							self.sendFact(fact)
						case <-factsDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				// Find an appropriate bin directory first
				binPath := "/usr/local/bin"
				if !canWriteToDir(binPath) {
					// Try user's local bin directory instead
					home, err := os.UserHomeDir()
					if err != nil {
						close(factsDone)
						return fmt.Errorf("failed to get home directory: %w", err)
					}
					binPath = filepath.Join(home, ".local", "bin")
					if err := os.MkdirAll(binPath, 0755); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to create bin directory: %w", err)
					}

					// Make sure the bin directory is in PATH
					if !strings.Contains(os.Getenv("PATH"), binPath) {
						self.log(fmt.Sprintf("Note: Make sure to add %s to your PATH", binPath))
					}
				}

				// Check and install Helm if needed
				cmd := exec.CommandContext(ctx, "helm", "version")
				out, err := cmd.CombinedOutput()
				if err == nil {
					msg := fmt.Sprintf("Helm is already installed: %s", strings.TrimSpace(string(out)))
					self.log(msg)
				} else {
					self.log("Helm not found, installing...")

					// Create temp directory for download
					tempDir, err := os.MkdirTemp("", "helm-*")
					if err != nil {
						close(factsDone)
						return fmt.Errorf("failed to create temp directory: %w", err)
					}
					defer os.RemoveAll(tempDir)

					// Determine OS and architecture
					goarch := runtime.GOARCH

					// Map architecture to Helm naming convention
					helmArch := goarch
					if goarch == "amd64" {
						helmArch = "amd64"
					} else if goarch == "arm64" {
						helmArch = "arm64"
					}

					version := "3.17.3"

					// Construct the download URL for Helm
					url := fmt.Sprintf("https://get.helm.sh/helm-v%s-%s-%s.tar.gz",
						version, "linux", helmArch)

					self.log(fmt.Sprintf("Downloading Helm from %s", url))

					// Download Helm
					tarPath := filepath.Join(tempDir, "helm.tar.gz")
					if err := self.downloadFile(url, tarPath); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to download helm: %w", err)
					}

					self.log("Extracting Helm")

					// Extract the file
					cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
					if out, err := cmd.CombinedOutput(); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to extract helm: %w, output: %s", err, string(out))
					}

					self.log(fmt.Sprintf("Installing Helm to %s", binPath))

					// The binary is in a subdirectory named after the OS-ARCH
					sourcePath := filepath.Join(tempDir, fmt.Sprintf("%s-%s", "linux", helmArch), "helm")
					destPath := filepath.Join(binPath, "helm")

					input, err := os.ReadFile(sourcePath)
					if err != nil {
						close(factsDone)
						return fmt.Errorf("failed to read helm binary: %w", err)
					}

					if err = os.WriteFile(destPath, input, 0755); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to install helm: %w", err)
					}

					// Verify installation
					cmd = exec.CommandContext(ctx, destPath, "version")
					out, err = cmd.CombinedOutput()
					if err != nil {
						close(factsDone)
						return fmt.Errorf("helm installation verification failed: %w", err)
					}

					self.log(fmt.Sprintf("Helm successfully installed: %s", strings.TrimSpace(string(out))))
				}

				// Check and install Helm diff plugin if needed
				cmd = exec.CommandContext(ctx, "helm", "plugin", "list")
				out, err = cmd.CombinedOutput()
				if err == nil && strings.Contains(string(out), "diff") {
					self.log("Helm diff plugin is already installed")
				} else {
					self.log("Installing Helm diff plugin...")
					cmd = exec.CommandContext(ctx, "helm", "plugin", "install", "https://github.com/databus23/helm-diff")
					if out, err := cmd.CombinedOutput(); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to install helm diff plugin: %w, output: %s", err, string(out))
					}

					// Verify diff plugin installation
					cmd = exec.CommandContext(ctx, "helm", "plugin", "list")
					out, err = cmd.CombinedOutput()
					if err != nil || !strings.Contains(string(out), "diff") {
						close(factsDone)
						return fmt.Errorf("helm diff plugin installation verification failed: %w", err)
					}

					self.log("Helm diff plugin successfully installed")
				}

				// Check and install Helmfile if needed
				cmd = exec.CommandContext(ctx, "helmfile", "--version")
				out, err = cmd.CombinedOutput()
				if err == nil {
					msg := fmt.Sprintf("Helmfile is already installed: %s", strings.TrimSpace(string(out)))
					self.log(msg)
				} else {
					self.log("Helmfile not found, installing...")
					tempDir, err := os.MkdirTemp("", "helmfile-*")
					if err != nil {
						close(factsDone)
						return fmt.Errorf("failed to create temp directory: %w", err)
					}
					defer os.RemoveAll(tempDir)

					// Determine OS and architecture
					goarch := runtime.GOARCH
					helmArch := goarch
					if goarch == "amd64" {
						helmArch = "amd64"
					} else if goarch == "arm64" {
						helmArch = "arm64"
					}

					version := "0.171.0"
					url := fmt.Sprintf("https://github.com/helmfile/helmfile/releases/download/v%s/helmfile_%s_%s_%s.tar.gz",
						version, version, "linux", helmArch)

					self.log(fmt.Sprintf("Downloading Helmfile from %s", url))

					// Download helmfile
					tarPath := filepath.Join(tempDir, "helmfile.tar.gz")
					if err := self.downloadFile(url, tarPath); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to download helmfile: %w", err)
					}

					self.log("Extracting Helmfile")

					// Extract the file
					cmd = exec.CommandContext(ctx, "tar", "-xzf", tarPath, "-C", tempDir)
					if out, err := cmd.CombinedOutput(); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to extract helmfile: %w, output: %s", err, string(out))
					}

					// Install helmfile binary
					sourcePath := filepath.Join(tempDir, "helmfile")
					destPath := filepath.Join(binPath, "helmfile")

					input, err := os.ReadFile(sourcePath)
					if err != nil {
						close(factsDone)
						return fmt.Errorf("failed to read helmfile binary: %w", err)
					}

					if err = os.WriteFile(destPath, input, 0755); err != nil {
						close(factsDone)
						return fmt.Errorf("failed to install helmfile: %w", err)
					}

					// Verify helmfile installation
					cmd = exec.CommandContext(ctx, destPath, "--version")
					out, err = cmd.CombinedOutput()
					if err != nil {
						close(factsDone)
						return fmt.Errorf("helmfile installation verification failed: %w", err)
					}

					self.log(fmt.Sprintf("Helmfile successfully installed: %s", strings.TrimSpace(string(out))))
				}

				close(factsDone) // Stop showing facts
				return nil
			},
		},
		{
			Description: "Waiting for kubeconfig to be created",
			Progress:    0.70,
			Action: func(ctx context.Context) error {
				kubeconfigPath = "/etc/rancher/k3s/k3s.yaml"

				maxKubeRetries := 6
				for retry := 0; retry < maxKubeRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking for kubeconfig (attempt %d/%d)...", retry+1, maxKubeRetries))
					if _, err := os.Stat(kubeconfigPath); err == nil {
						self.log("Kubeconfig file found")
						return nil
					}

					if retry == maxKubeRetries-1 {
						errMsg := "Kubeconfig file not created after multiple attempts"
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()

						return fmt.Errorf("K3S installed but kubeconfig not created: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Installing Longhorn storage system",
			Progress:    0.85, // Larger allocation since Longhorn installation takes significant time
			Action: func(ctx context.Context) error {
				// Start showing educational facts during Longhorn installation
				factsDone := make(chan struct{})
				go func() {
					// Show first fact immediately
					fact := self.factRotator.GetNext()
					self.sendFact(fact)

					ticker := time.NewTicker(5 * time.Second) // Show a new fact every 5 seconds
					defer ticker.Stop()

					for {
						select {
						case <-ticker.C:
							fact := self.factRotator.GetNext()
							self.sendFact(fact)
						case <-factsDone:
							return
						case <-ctx.Done():
							return
						}
					}
				}()

				// Enable and start iSCSI daemon (required for Longhorn)
				self.log("Enabling iSCSI daemon for Longhorn storage...")
				iscsidCmd := exec.CommandContext(ctx, "systemctl", "enable", "--now", "iscsid")
				if output, err := iscsidCmd.CombinedOutput(); err != nil {
					// Log warning but don't fail the installation
					self.log(fmt.Sprintf("Warning: Failed to enable iscsid service: %v, output: %s", err, string(output)))
				} else {
					self.log("iSCSI daemon enabled successfully")
				}

				// Add Longhorn Helm repo
				self.log("Adding Longhorn Helm repository...")
				repoCmd := exec.CommandContext(ctx, "helm", "repo", "add", "longhorn", "https://charts.longhorn.io")
				if output, err := repoCmd.CombinedOutput(); err != nil {
					close(factsDone)
					return fmt.Errorf("failed to add Longhorn Helm repo: %w, output: %s", err, string(output))
				}

				// Update Helm repos
				self.log("Updating Helm repositories...")
				updateCmd := exec.CommandContext(ctx, "helm", "repo", "update")
				if output, err := updateCmd.CombinedOutput(); err != nil {
					close(factsDone)
					return fmt.Errorf("failed to update Helm repos: %w, output: %s", err, string(output))
				}

				// Remove default annotation from existing StorageClasses
				self.log("Removing default annotation from existing StorageClasses...")
				patchCmd := exec.CommandContext(ctx, "kubectl", "patch", "storageclass", "--type=json", "-p",
					`[{"op": "replace", "path": "/metadata/annotations/storageclass.kubernetes.io~1is-default-class", "value": "false"}]`,
					"--selector=storageclass.kubernetes.io/is-default-class=true")
				if output, err := patchCmd.CombinedOutput(); err != nil {
					self.log(fmt.Sprintf("Warning: Failed to remove default annotation from StorageClasses: %s", string(output)))
				}

				// Install Longhorn
				self.log("Installing Longhorn...")
				installCmd := exec.CommandContext(ctx, "helm", "install", "longhorn", "longhorn/longhorn",
					"--namespace", "longhorn-system",
					"--create-namespace",
					"--version", "1.9.0",
					"--set", "defaultSettings.defaultReplicaCount=1",
					"--set", "defaultSettings.replicaSoftAntiAffinity=true",
					"--set", "defaultSettings.replicaAutoBalance=disabled",
					"--set", "defaultSettings.disableRevisionCounter=true",
					"--set", "defaultSettings.upgradeChecker=false",
					"--set", "defaultSettings.autoSalvage=true",
					"--set", "defaultSettings.storageOverProvisioningPercentage=150",
					"--set", "defaultSettings.storageMinimalAvailablePercentage=10",
					"--set", "defaultSettings.concurrentReplicaRebuildPerNodeLimit=0",
					"--set", "defaultSettings.concurrentVolumeBackupRestorePerNodeLimit=0",
					"--set", "defaultSettings.concurrentAutomaticEngineUpgradePerNodeLimit=0",
					"--set", "defaultSettings.guaranteedInstanceManagerCPU=0",
					"--set", "defaultSettings.kubernetesClusterAutoscalerEnabled=false",
					"--set", "defaultSettings.autoCleanupSystemGeneratedSnapshot=true",
					"--set", "defaultSettings.disableSchedulingOnCordonedNode=true",
					"--set", "defaultSettings.fastReplicaRebuildEnabled=false",
					"--set", "longhornUI.enabled=false",
					"--set", "enableShareManager=false",
					"--set", "enableUpgradeChecker=false",
					"--set", "enablePSP=false",
					"--set", "longhornDriverDeployer.enabled=false",
					"--set", "driver.debug=false",
					"--set", "longhornManager.resources.requests.cpu=20m",
					"--set", "longhornManager.resources.requests.memory=64Mi",
					"--set", "longhornManager.resources.limits.cpu=100m",
					"--set", "longhornManager.resources.limits.memory=256Mi",
					"--set", "instanceManager.resources.requests.cpu=40m",
					"--set", "instanceManager.resources.requests.memory=64Mi",
					"--set", "instanceManager.resources.limits.cpu=200m",
					"--set", "instanceManager.resources.limits.memory=256Mi",
					"--set", "csi.attacherReplicaCount=1",
					"--set", "csi.provisionerReplicaCount=1",
					"--set", "csi.resizerReplicaCount=1",
					"--set", "csi.snapshotterReplicaCount=0",
					"--set", "csi.kubeletPlugin.resources.requests.cpu=10m",
					"--set", "csi.kubeletPlugin.resources.requests.memory=32Mi",
					"--set", "csi.kubeletPlugin.resources.limits.cpu=50m",
					"--set", "csi.kubeletPlugin.resources.limits.memory=128Mi",
					"--set", "persistence.defaultClass=true",
					"--set", "persistence.defaultClassReplicaCount=1",
					"--set", "persistence.defaultDataLocality=best-effort",
					"--set", "persistence.reclaimPolicy=Retain",
				)

				// Set KUBECONFIG environment variable
				installCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

				if output, err := installCmd.CombinedOutput(); err != nil {
					close(factsDone)
					return fmt.Errorf("failed to install Longhorn: %w, output: %s", err, string(output))
				}

				// Wait for Longhorn to be ready
				self.log("Waiting for Longhorn to be ready...")
				waitCmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=ready", "pod", "-l", "app=longhorn-manager", "-n", "longhorn-system", "--timeout=300s")
				waitCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
				if output, err := waitCmd.CombinedOutput(); err != nil {
					close(factsDone)
					return fmt.Errorf("failed waiting for Longhorn to be ready: %w, output: %s", err, string(output))
				}

				// Remove default annotation from local-path storage class
				self.log("Removing default annotation from local-path storage class...")
				patchCmd = exec.CommandContext(ctx, "kubectl", "patch", "storageclass", "local-path", "--type=json", "-p",
					`[{"op": "replace", "path": "/metadata/annotations/storageclass.kubernetes.io~1is-default-class", "value": "false"}]`)
				patchCmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))
				if output, err := patchCmd.CombinedOutput(); err != nil {
					self.log(fmt.Sprintf("Warning: Failed to remove default annotation from local-path storage class: %s", string(output)))
				}

				close(factsDone) // Stop showing facts
				return nil
			},
		},
		{
			Description: "Pre-fetching common container images",
			Progress:    0.90, // Moderate allocation since this can run in parallel
			Action: func(ctx context.Context) error {
				// List of common images to pre-fetch
				images := []string{
					"ghcr.io/zalando/postgres-operator:v1.14.0",
					"ghcr.io/unbindapp/spilo:17-latest",
					"docker.io/bitnami/valkey:8.1.1-debian-12-r0",
					"docker.io/registry:2",
					"registry.k8s.io/ingress-nginx/controller:v1.12.2",
					"unbindapp/dex:master-14777142866",
					"ghcr.io/unbindapp/kube-oidc-proxy:master-14884925050",
				}

				self.log(fmt.Sprintf("Starting pre-fetch of %d container images...", len(images)))

				// Create a channel to collect errors
				errChan := make(chan error, len(images))
				// Create a channel to track completion
				doneChan := make(chan struct{}, len(images))
				// Create a semaphore to limit concurrent pulls
				sem := make(chan struct{}, 5) // Limit to 5 concurrent pulls

				// Launch goroutines for each image
				for _, image := range images {
					go func(img string) {
						sem <- struct{}{}
						defer func() {
							<-sem
							doneChan <- struct{}{}
						}()

						cmd := exec.CommandContext(ctx, "ctr", "images", "pull", img)
						if output, err := cmd.CombinedOutput(); err != nil {
							errChan <- fmt.Errorf("failed to pull image %s: %w, output: %s", img, err, string(output))
							return
						}
						self.log(fmt.Sprintf("Successfully pre-fetched image: %s", img))
					}(image)
				}

				// Wait for all goroutines to complete
				for i := 0; i < len(images); i++ {
					select {
					case err := <-errChan:
						// Silently log the error without affecting the process
						self.log(fmt.Sprintf("Image pre-fetch failed: %v", err))
					case <-doneChan:
						// Image pull completed
					case <-ctx.Done():
						return ctx.Err()
					}
				}

				self.log("Container image pre-fetch completed")
				return nil
			},
		},
		{
			Description: "Verifying K3S installation by checking nodes",
			Progress:    0.95,
			Action: func(ctx context.Context) error {
				maxNodeRetries := 6
				for retry := 0; retry < maxNodeRetries; retry++ {
					time.Sleep(5 * time.Second)

					self.log(fmt.Sprintf("Checking K3S nodes (attempt %d/%d)...", retry+1, maxNodeRetries))
					kubeCmd := exec.CommandContext(ctx, "k3s", "kubectl", "get", "nodes")
					kubeOutput, err := kubeCmd.CombinedOutput()

					if err == nil {
						successMsg := fmt.Sprintf("K3S nodes: %s", string(kubeOutput))
						self.log(successMsg)
						self.log("K3S installation verified successfully")
						return nil
					}

					if retry == maxNodeRetries-1 {
						errMsg := fmt.Sprintf("Error checking K3S nodes: %s", string(kubeOutput))
						self.log(errMsg)

						serviceError := self.collectServiceDiagnostics()

						return fmt.Errorf("K3S installed but not functioning correctly: %w", serviceError)
					}
				}
				return nil
			},
		},
		{
			Description: "Finalizing installation",
			Progress:    0.98,
			Action: func(ctx context.Context) error {
				// Add KUBECONFIG to ~/.profile
				self.log("Adding KUBECONFIG to ~/.profile...")

				// Read existing .profile content
				profilePath := filepath.Join(os.Getenv("HOME"), ".profile")
				profileContent, err := os.ReadFile(profilePath)
				if err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("failed to read .profile: %w", err)
				}

				// Check if KUBECONFIG is already set
				kubeconfigLine := fmt.Sprintf("\nexport KUBECONFIG=%s\n", kubeconfigPath)
				if !strings.Contains(string(profileContent), kubeconfigLine) {
					// Append KUBECONFIG export if it doesn't exist
					f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if err != nil {
						return fmt.Errorf("failed to open .profile: %w", err)
					}
					defer f.Close()

					if _, err := f.WriteString(kubeconfigLine); err != nil {
						return fmt.Errorf("failed to write to .profile: %w", err)
					}
					self.log("KUBECONFIG environment variable added to ~/.profile")
				} else {
					self.log("KUBECONFIG environment variable already exists in ~/.profile")
				}

				return nil
			},
		},
	}

	// Execute all installation steps
	for _, step := range steps {
		// Log the current step
		self.logProgress(step.Progress, "installing", step.Description, nil)

		// Execute the step's action
		if err := step.Action(ctx); err != nil {
			// Set end time and send failure update
			self.state.endTime = time.Now()
			self.logProgress(step.Progress, "failed", fmt.Sprintf("Failed: %s", step.Description), err)
			return "", err
		}

		// Don't log completion messages as they're confusing - just move to the next step
		// The progress bar itself shows completion status
	}

	// Set end time and send final progress update
	self.state.endTime = time.Now()
	self.logProgress(1.0, "completed", "K3S installation completed successfully", nil)

	return kubeconfigPath, nil
}

// GetLastUpdateMessage returns current status
func (self *Installer) GetLastUpdateMessage() K3SUpdateMessage {
	return self.state.lastMsg
}

// GetInstallationState returns status with timing info
func (self *Installer) GetInstallationState() (status string, startTime time.Time, endTime time.Time) {
	return self.state.status, self.state.startTime, self.state.endTime
}

// checkServiceStatus checks k3s service state
func (self *Installer) checkServiceStatus() (string, error) {
	checkCmd := exec.Command("systemctl", "is-active", "k3s.service")
	statusOutput, err := checkCmd.CombinedOutput()
	statusStr := strings.TrimSpace(string(statusOutput))

	return statusStr, err
}

// collectServiceDiagnostics gathers info for troubleshooting
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

// downloadFile grabs remote files
func (self *Installer) downloadFile(url, filepath string) error {
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

	// Copy the data
	_, err = io.Copy(out, resp.Body)
	return err
}

// canWriteToDir tests directory write permissions
func canWriteToDir(dir string) bool {
	testFile := filepath.Join(dir, ".helm_write_test")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return false
	}
	os.Remove(testFile)
	return true
}
