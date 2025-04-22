package tui

import (
	"github.com/unbindapp/unbind-installer/internal/installer"
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

// * Package manager messages
type installPackagesMsg struct{}

type installCompleteMsg struct{}

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

type ciliumInstallCompleteMsg struct{}

// * Dependencies
// unbindInstallCompleteMsg is sent when all dependencies are installed
type unbindInstallCompleteMsg struct{}
