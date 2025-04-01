package tui

import (
	"github.com/unbindapp/unbind-installer/internal/network"
	"github.com/unbindapp/unbind-installer/internal/osinfo"
	"k8s.io/client-go/dynamic"
)

// Basic message types
type errMsg struct {
	err error
}

type logMsg struct {
	message string
}

// State-specific messages
type osInfoMsg struct {
	info *osinfo.OSInfo
}

type installPackagesMsg struct{}

type installCompleteMsg struct{}

// DNS-related messages
type detectIPsMsg struct{}

type detectIPsCompleteMsg struct {
	ipInfo *network.IPInfo
}

type dnsValidationMsg struct{}

type autoAdvanceMsg struct{}

type dnsValidationCompleteMsg struct {
	success    bool
	cloudflare bool
}

type dnsValidationTimeoutMsg struct{}

type manualContinueMsg struct{}

type k3sInstallCompleteMsg struct {
	kubeConfig string
	kubeClient *dynamic.DynamicClient
}

type ciliumInstallCompleteMsg struct{}
