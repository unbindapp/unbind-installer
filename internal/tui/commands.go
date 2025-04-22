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

// installRequiredPackages is a command that installs the required packages
func (self Model) installRequiredPackages() tea.Cmd {
	return func() tea.Msg {
		// Packages to install
		packages := []string{
			"curl",
			"wget",
			"ca-certificates",
			"apt-transport-https",
		}

		// Create a new apt installer
		installer := pkgmanager.NewAptInstaller(self.logChan)

		// Install the packages
		err := installer.InstallPackages(packages)
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

		ipInfo, err := network.DetectIPs(func(msg string) {
			self.logChan <- msg
		})

		if err != nil {
			self.logChan <- "Error detecting IPs: " + err.Error()
			return errMsg{err: errdefs.ErrNetworkDetectionFailed}
		}

		return detectIPsCompleteMsg{ipInfo: ipInfo}
	}
}

// startDNSValidation starts the DNS validation process
func (self Model) startDNSValidation() tea.Cmd {
	return func() tea.Msg {
		if self.dnsInfo == nil || self.dnsInfo.Domain == "" {
			return errMsg{err: nil}
		}

		baseDomain := strings.Replace(self.dnsInfo.Domain, "*.", "", 1)

		testDomains := []string{
			"unbind." + baseDomain,
			"dex." + baseDomain,
		}

		// Log the validation attempt
		self.logChan <- "Starting DNS validation..."

		// Check for Cloudflare first
		cloudflareSuccessCount := 0
		for _, domain := range testDomains {
			cloudflare := network.CheckCloudflareProxy(domain, func(msg string) {
				self.logChan <- msg
			})

			// If Cloudflare is detected, consider it successful
			if cloudflare {
				cloudflareSuccessCount++
			}
		}

		if cloudflareSuccessCount == 2 {
			// Check registry
			cloudflareRegistry := network.CheckCloudflareProxy("registry."+baseDomain, func(msg string) {
				self.logChan <- msg
			})

			if cloudflareRegistry {
				return dnsValidationCompleteMsg{
					success:       false,
					cloudflare:    true,
					registryIssue: true,
				}
			}

			return dnsValidationCompleteMsg{
				success:    true,
				cloudflare: true,
			}
		}

		// Otherwise test the wildcard DNS configuration
		successCount := 0
		for _, domain := range testDomains {
			success := network.ValidateDNS(domain, self.dnsInfo.ExternalIP, func(msg string) {
				self.logChan <- msg
			})
			if success {
				successCount++
			}
		}

		if successCount == 2 {
			return dnsValidationCompleteMsg{
				success:    true,
				cloudflare: false,
			}
		}
		return dnsValidationCompleteMsg{
			success:    false,
			cloudflare: false,
		}
	}
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
			self.logChan <- fmt.Sprintf("K3S installation failed: %s", err.Error())
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("K3S installation failed: %s", err.Error()))}
		}

		// Create a new K8s client
		client, err := k3s.NewK8sClient(kubeConfig)
		if err != nil {
			self.logChan <- fmt.Sprintf("K8s client creation failed: %s", err.Error())
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("K8s client creation failed: %s", err.Error()))}
		}

		// create dependencies manager
		dm, err := unbindInstaller.NewUnbindInstaller(kubeConfig, self.logChan, self.unbindProgressChan)
		if err != nil {
			self.logChan <- fmt.Sprintf("Dependencies manager creation failed: %s", err.Error())
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("Dependencies manager creation failed: %s", err.Error()))}
		}

		return k3sInstallCompleteMsg{
			kubeConfig:      kubeConfig,
			kubeClient:      client,
			unbindInstaller: dm,
		}
	}
}

// installCilium is a command that installs Cilium
func (self Model) installCilium() tea.Cmd {
	return func() tea.Msg {
		// Create a new K3S installer
		installer := k3s.NewCiliumInstaller(self.logChan, self.k3sProgressChan, self.kubeConfig, self.kubeClient, self.dnsInfo.InternalIP, self.dnsInfo.CIDR)

		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Install K3S
		err := installer.Install(ctx)
		if err != nil {
			self.logChan <- fmt.Sprintf("Cilium installation failed: %s", err.Error())
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeK3sInstallFailed, fmt.Sprintf("K3S installation failed: %s", err.Error()))}
		}

		return ciliumInstallCompleteMsg{}
	}
}

// installUnbind installs the unbind helmfile
func (self Model) installUnbind() tea.Cmd {
	return func() tea.Msg {
		// Create a context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		// Install K3S
		err := self.unbindInstaller.SyncHelmfileWithSteps(ctx, unbindInstaller.SyncHelmfileOptions{
			BaseDomain: self.dnsInfo.Domain,
		})
		if err != nil {
			self.logChan <- fmt.Sprintf("Unbind installation failed: %s", err.Error())
			return errMsg{err: errdefs.NewCustomError(errdefs.ErrTypeUnbindInstallFailed, fmt.Sprintf("Unbind installation failed: %s", err.Error()))}
		}

		return unbindInstallCompleteMsg{}
	}
}
