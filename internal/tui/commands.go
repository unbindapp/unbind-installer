package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/unbindapp/unbind-installer/internal/errdefs"
	unbindInstaller "github.com/unbindapp/unbind-installer/internal/installer"
	"github.com/unbindapp/unbind-installer/internal/k3s"
	"github.com/unbindapp/unbind-installer/internal/network"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"github.com/unbindapp/unbind-installer/internal/pkgmanager"
	"github.com/unbindapp/unbind-installer/internal/system"
)

// listenForLogs returns a command that listens for log messages
type tickMsg struct{}

func (self Model) listenForLogs() tea.Cmd {
	return func() tea.Msg {
		select {
		case msg, ok := <-self.logChan:
			if !ok {
				// Channel closed
				return nil
			}
			return logMsg{message: msg}
		default:
			// Don't block if no message is available
			return tickMsg{} // A dummy message to keep the command running
		}
	}
}

// checkK3sCommand checks for an existing K3s installation.
func checkK3sCommand() tea.Cmd {
	return func() tea.Msg {
		result, err := k3s.CheckInstalled()
		return k3sCheckResultMsg{checkResult: result, err: err}
	}
}

// uninstallK3sCommand runs the K3s uninstall script.
func (self Model) uninstallK3sCommand(scriptPath string) tea.Cmd {
	return func() tea.Msg {
		err := k3s.Uninstall(scriptPath, self.logChan) // Pass logChan
		return k3sUninstallCompleteMsg{err: err}
	}
}

// detectOSInfo is a command that gets OS information
func detectOSInfo() tea.Msg {
	if os.Geteuid() != 0 {
		return errMsg{err: errdefs.ErrNotRoot}
	}

	info, err := osinfo.GetOSInfo()
	if err != nil {
		return errMsg{err}
	}
	return osInfoMsg{info}
}

// checkSwapCommand checks if swap is active.
func (self Model) checkSwapCommand() tea.Cmd {
	return func() tea.Msg {
		isEnabled, err := system.CheckSwapActive(self.logChan)
		return swapCheckResultMsg{isEnabled: isEnabled, err: err}
	}
}

// getDiskSpaceCommand gets available disk space.
func (self Model) getDiskSpaceCommand() tea.Cmd {
	return func() tea.Msg {
		gb, err := system.GetAvailableDiskSpaceGB(self.logChan)
		return diskSpaceResultMsg{availableGB: gb, err: err}
	}
}

// createSwapCommand creates the swap file.
func (self Model) createSwapCommand(sizeGB int) tea.Cmd {
	return func() tea.Msg {
		err := system.CreateSwapFile(sizeGB, self.logChan)
		return swapCreateResultMsg{err: err}
	}
}

// installRequiredPackages is a command that installs the required packages
func (self Model) installRequiredPackages() tea.Cmd {
	return func() tea.Msg {
		// Common package names that we need
		commonPackages := []string{
			"curl",
			"wget",
			"ca-certificates",
			"apt-transport-https",
			"apache2-utils",
		}

		// Get distribution-specific package names
		packages := pkgmanager.GetDistributionPackages(self.osInfo.Distribution, commonPackages)

		// Create a new package manager
		installer, err := pkgmanager.NewPackageManager(self.osInfo.Distribution, self.logChan)
		if err != nil {
			return errMsg{err}
		}

		// Install the packages
		err = installer.InstallPackages(packages)
		if err != nil {
			return errMsg{err}
		}

		return installCompleteMsg{}
	}
}

// startDetectingIPs starts the IP detection process
func (self Model) startDetectingIPs() tea.Cmd {
	return func() tea.Msg {
		if self.dnsInfo == nil {
			self.dnsInfo = &dnsInfo{}
		}

		ipInfo, err := network.DetectIPs(self.log)

		if err != nil {
			self.log("Error detecting IPs: " + err.Error())
			return errMsg{err: errdefs.ErrNetworkDetectionFailed}
		}

		return detectIPsCompleteMsg{ipInfo: ipInfo}
	}
}

// startDNSValidation launches the DNS validation process.
//
// Validation rules:
//  1. unbind.<baseDomain> must resolve (Cloudflare proxy allowed but not required).
//  2. unbind-registry.<baseDomain> must resolve *without* being behind Cloudflare.
//  3. If an arbitrary sub‑domain resolves, wildcard DNS is assumed and the wildcard flag is set.
func (self Model) startDNSValidation() tea.Cmd {
	return func() tea.Msg {
		if self.dnsInfo == nil || self.dnsInfo.Domain == "" {
			return errMsg{err: nil}
		}

		self.log("Starting DNS validation…")

		base := strings.TrimPrefix(self.dnsInfo.Domain, "*.")

		/* -------------------------------------------------------------------- */
		// 1. unbind.<baseDomain>
		/* -------------------------------------------------------------------- */
		unbindValid, unbindCF := self.validateDomain("unbind."+base, true)

		/* -------------------------------------------------------------------- */
		// 2. unbind-registry.<baseDomain> (CF proxy *not* allowed)
		/* -------------------------------------------------------------------- */
		registryValid, registryCF := self.validateDomain("unbind-registry."+base, false)
		if registryCF {
			self.log("ERROR: Registry domain must not be behind Cloudflare proxy")
			return dnsValidationCompleteMsg{
				success:       false,
				cloudflare:    true,
				registryIssue: true,
			}
		}

		/* -------------------------------------------------------------------- */
		// 3. Wildcard detection via arbitrary sub‑domain
		/* -------------------------------------------------------------------- */
		wildcardValid, wildcardCF := self.detectWildcard(base)
		if wildcardValid {
			self.dnsInfo.IsWildcard = true
		}

		/* -------------------------------------------------------------------- */
		// Final decision matrix
		/* -------------------------------------------------------------------- */
		switch {
		case wildcardValid && registryValid:
			self.log("Wildcard domain detected and validated successfully")
			return dnsValidationCompleteMsg{
				success:    true,
				cloudflare: wildcardCF || unbindCF,
			}

		case !wildcardValid && (unbindValid || unbindCF) && registryValid:
			self.log("Individual A records validated successfully")
			return dnsValidationCompleteMsg{
				success:    true,
				cloudflare: unbindCF,
			}

		default:
			self.log("DNS validation failed")
			return dnsValidationCompleteMsg{
				success:    false,
				cloudflare: unbindCF || registryCF || wildcardCF,
			}
		}
	}
}

// validateDomain checks whether the domain resolves to the expected IP and whether it is
// behind Cloudflare. If allowCloudflare is false and the domain *is* behind Cloudflare,
// the domain is considered invalid.
func (self Model) validateDomain(domain string, allowCloudflare bool) (dnsValid, behindCF bool) {
	self.log(fmt.Sprintf("Checking %s…", domain))

	behindCF = network.CheckCloudflareProxy(domain, self.log)
	if behindCF && !allowCloudflare {
		return false, true
	}

	dnsValid = network.ValidateDNS(domain, self.dnsInfo.ExternalIP, self.log)
	return dnsValid, behindCF
}

// detectWildcard probes an arbitrary sub‑domain to infer wildcard DNS configuration.
// If the probe domain is behind Cloudflare the presence of wildcard is assumed true.
func (self Model) detectWildcard(base string) (dnsValid, behindCF bool) {
	probe := fmt.Sprintf("test%d.%s", time.Now().Unix(), base)
	self.log(fmt.Sprintf("Checking for wildcard domain with %s…", probe))

	behindCF = network.CheckCloudflareProxy(probe, self.log)
	if behindCF {
		return true, true // wildcard via Cloudflare
	}

	dnsValid = network.ValidateDNS(probe, self.dnsInfo.ExternalIP, self.log)
	return dnsValid, behindCF
}

// dnsValidationTimeout creates a timeout for DNS validation
func dnsValidationTimeout(duration time.Duration) tea.Cmd {
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return dnsValidationTimeoutMsg{}
	})
}

// installK3S is a command that installs K3S
func (self Model) installK3S() tea.Cmd {
	return func() tea.Msg {
		// Create a new K3S installer
		installer := k3s.NewInstaller(self.logChan, self.k3sProgressChan)

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Install K3S
		kubeConfig, err := installer.Install(ctx)
		if err != nil {
			self.log(fmt.Sprintf("K3S installation failed: %s", err.Error()))
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("K3S installation failed: %s", err.Error()))}
		}

		// Create a new K8s client
		client, err := k3s.NewK8sClient(kubeConfig)
		if err != nil {
			self.log(fmt.Sprintf("K8s client creation failed: %s", err.Error()))
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("K8s client creation failed: %s", err.Error()))}
		}

		// create dependencies manager
		dm, err := unbindInstaller.NewUnbindInstaller(kubeConfig, self.logChan, self.unbindProgressChan)
		if err != nil {
			self.log(fmt.Sprintf("Dependencies manager creation failed: %s", err.Error()))
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("Dependencies manager creation failed: %s", err.Error()))}
		}

		return k3sInstallCompleteMsg{
			kubeConfig:      kubeConfig,
			kubeClient:      client,
			unbindInstaller: dm,
		}
	}
}

// installUnbind installs the unbind helmfile
func (self Model) installUnbind() tea.Cmd {
	return func() tea.Msg {
		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Install Unbind
		opts := unbindInstaller.SyncHelmfileOptions{
			UnbindDomain:         self.dnsInfo.UnbindDomain,
			UnbindRegistryDomain: self.dnsInfo.RegistryDomain,
		}
		if self.dnsInfo.IsWildcard {
			opts.BaseDomain = self.dnsInfo.Domain
		}
		err := self.unbindInstaller.SyncHelmfileWithSteps(ctx, opts)
		if err != nil {
			self.log(fmt.Sprintf("Unbind installation failed: %s", err.Error()))
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeUnbindInstallFailed, fmt.Sprintf("Unbind installation failed: %s", err.Error()))}
		}

		return unbindInstallCompleteMsg{}
	}
}

func (self Model) log(msg string) {
	self.logChan <- msg
}
