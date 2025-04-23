package tui

import "time"

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateWelcome ApplicationState = iota
	StateCheckK3s
	StateConfirmUninstallK3s
	StateUninstallingK3s
	StateDebugLogs
	StateLoading
	StateRootDetection
	StateOSInfo
	StateCheckingSwap
	StateConfirmCreateSwap
	StateEnterSwapSize
	StateCreatingSwap
	StateSwapCreated
	StateInstallingPackages
	StateInstallComplete
	StateError
	StateDetectingIPs
	StateDNSConfig
	StateDNSValidation
	StateDNSSuccess
	StateDNSFailed
	StateInstallingK3S
	StateInstallingUnbind
)

// Additional model fields for DNS setup
type dnsInfo struct {
	InternalIP         string
	ExternalIP         string
	CIDR               string
	Domain             string
	ValidationStarted  bool
	ValidationSuccess  bool
	CloudflareDetected bool
	RegistryIssue      bool
	TestingStartTime   time.Time
	ValidationDuration time.Duration
}
