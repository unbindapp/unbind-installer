package tui

import (
	"github.com/unbindapp/unbind-installer/internal/installer"
	"github.com/unbindapp/unbind-installer/internal/k3s"
	"github.com/unbindapp/unbind-installer/internal/network"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"k8s.io/client-go/dynamic"
)

// * Common message types
type errMsg struct {
	err error
}

type logMsg struct {
	message string
}

// * OS check message
type osInfoMsg struct {
	info *osinfo.OSInfo
}

// * Swap check messages
type swapCheckResultMsg struct {
	isEnabled bool
	err       error
}

type diskSpaceResultMsg struct {
	availableGB float64
	err         error
}

type swapCreateResultMsg struct {
	err error
}

// * Package manager messages
type installPackagesMsg struct{}

type installCompleteMsg struct{}

type packageInstallProgressMsg struct {
	packageName string
	progress    float64
	step        string
	isComplete  bool
}

// * DNS-related messages
type detectIPsMsg struct{}

type detectIPsCompleteMsg struct {
	ipInfo *network.IPInfo
}

type dnsValidationMsg struct{}

type autoAdvanceMsg struct{}

type dnsValidationCompleteMsg struct {
	success       bool
	cloudflare    bool
	registryIssue bool
}

type dnsValidationTimeoutMsg struct{}

type manualContinueMsg struct{}

// * K3S

type k3sInstallCompleteMsg struct {
	kubeConfig      string
	kubeClient      *dynamic.DynamicClient
	unbindInstaller *installer.UnbindInstaller
}

type k3sCheckResultMsg struct {
	checkResult *k3s.CheckResult
	err         error
}

// k3sUninstallCompleteMsg is sent when the K3s uninstall process finishes.
type k3sUninstallCompleteMsg struct {
	err error
}

// * Dependencies
// unbindInstallCompleteMsg is sent when all dependencies are installed
type unbindInstallCompleteMsg struct{}

// registryValidationCompleteMsg is sent when registry credential validation completes
type registryValidationCompleteMsg struct {
	success bool
}
