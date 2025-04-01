package dependencies

import (
	"context"
	"fmt"
	"os"
	"time"
)

// A very minimal longhorn configuration for a single-node setup
const longhornValues = `
# Make Longhorn the default storage class
persistence:
 defaultClass: true
 defaultClassReplicaCount: 1 # Minimum possible - single copy only
 reclaimPolicy: Delete
# Set minimal resource usage
defaultSettings:
 # Core settings
 defaultReplicaCount: 1
 replicaSoftAntiAffinity: false
 replicaAutoBalance: "disabled"
 # Disable features to reduce resource usage
 upgradeChecker: false
 autoSalvage: false
 disableRevisionCounter: true
 # Storage configuration
 storageOverProvisioningPercentage: 100 # No over-provisioning
 storageMinimalAvailablePercentage: 0
 # Disable background operations
 concurrentReplicaRebuildPerNodeLimit: 0 # Disable background rebuild
 concurrentVolumeBackupRestorePerNodeLimit: 0 # Disable background restore
 concurrentAutomaticEngineUpgradePerNodeLimit: 0
 # Resource allocation
 guaranteedInstanceManagerCPU: 0
 priorityClass: ""
 # Other features to disable or minimize
 kubernetesClusterAutoscalerEnabled: false
 storageNetwork: ""
 # Snapshot/cleanup behavior
 autoCleanupSystemGeneratedSnapshot: true
 disableSchedulingOnCordonedNode: true
 fastReplicaRebuildEnabled: false
 replicaReplenishmentWaitInterval: 0
# Minimum CSI component replicas
csi:
 attacherReplicaCount: 1
 provisionerReplicaCount: 1
 resizerReplicaCount: 1
 snapshotterReplicaCount: 1
# UI and Manager minimized
longhornUI:
 replicas: 1
longhornManager:
 priorityClass: ""
longhornDriver:
 priorityClass: ""
`

// InstallationStep defines a step in the installation process
type InstallationStep struct {
	Description string
	Progress    float64
	Action      func(context.Context) error
}

// Example usage of the step-based approach for installing Longhorn
func (self *DependenciesManager) InstallLonghornWithSteps(ctx context.Context) error {
	var valuesFile *os.File

	return self.InstallDependencyWithSteps(ctx, "longhorn", []InstallationStep{
		{
			Description: "Creating values file",
			Progress:    0.05,
			Action: func(ctx context.Context) error {
				var err error
				valuesFile, err = os.CreateTemp("", "longhorn-values-*.yaml")
				return err
			},
		},
		{
			Description: "Writing configuration",
			Progress:    0.1,
			Action: func(ctx context.Context) error {
				if _, err := valuesFile.Write([]byte(longhornValues)); err != nil {
					return err
				}
				return valuesFile.Close()
			},
		},
		{
			Description: "Installing Helm chart",
			Progress:    0.15,
			Action: func(ctx context.Context) error {
				defer os.Remove(valuesFile.Name())

				opts := HelmChartOptions{
					RepoName:    "longhorn",
					RepoURL:     "https://charts.longhorn.io",
					ChartName:   "longhorn/longhorn",
					ReleaseName: "longhorn",
					Namespace:   "longhorn-system",
					Version:     "1.8.1",
					ValuesFile:  valuesFile.Name(),
					Timeout:     300 * time.Second,
					Wait:        true,
					CreateNS:    true,
				}

				// We don't call startInstallation again since it's already called by InstallDependencyWithSteps
				// We also don't call completeInstallation since that will be handled by InstallDependencyWithSteps

				return self.installLonghornChart(ctx, opts)
			},
		},
	})
}

// installLonghornChart is a helper method that installs the Longhorn chart without calling
// startInstallation or completeInstallation (for use with InstallDependencyWithSteps)
func (self *DependenciesManager) installLonghornChart(ctx context.Context, opts HelmChartOptions) error {
	// This is a variant of InstallHelmChart that doesn't call startInstallation or completeInstallation

	// Add helm repo
	self.logProgress(opts.ReleaseName, 0.2, "Adding Helm repository %s at %s", opts.RepoName, opts.RepoURL)
	if err := self.addHelmRepo(opts.RepoName, opts.RepoURL); err != nil {
		return fmt.Errorf("failed to add helm repo: %w", err)
	}

	// Update helm repos
	self.logProgress(opts.ReleaseName, 0.3, "Updating Helm repositories")
	if err := self.updateHelmRepos(); err != nil {
		return fmt.Errorf("failed to update helm repos: %w", err)
	}

	// Create namespace if needed
	if opts.CreateNS {
		self.logProgress(opts.ReleaseName, 0.4, "Creating namespace %s if needed", opts.Namespace)
		if err := self.createNamespaceIfNotExists(opts.Namespace); err != nil {
			return err
		}
	}

	// Install the chart
	self.logProgress(opts.ReleaseName, 0.5, "Installing Helm chart %s", opts.ChartName)

	// During the install phase, we'll periodically update progress
	installStartTime := time.Now()
	installDone := make(chan error, 1)

	// Start a goroutine to update progress during installation
	go func() {
		if opts.Wait {
			currentProgress := 0.5

			// Update progress every few seconds until the installation is done
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					if currentProgress < 0.9 {
						currentProgress += 0.05
						self.updateProgress(opts.ReleaseName, currentProgress)
						self.sendLog(fmt.Sprintf("Still installing %s (elapsed: %v)...",
							opts.ChartName, time.Since(installStartTime).Round(time.Second)))
					}
				case <-installDone:
					return
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Run the actual installation
	err := self.installChart(opts)

	// Signal that the installation is done
	close(installDone)

	if err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	// Log completion with timing info
	installDuration := time.Since(installStartTime).Round(time.Second)
	self.logProgress(opts.ReleaseName, 0.95,
		"Successfully installed %s in namespace %s (took %v)",
		opts.ChartName, opts.Namespace, installDuration)

	return nil
}
