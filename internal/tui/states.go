package tui

import "time"

// ApplicationState represents the current state of the application
type ApplicationState int

const (
	StateWelcome ApplicationState = iota
	StateDebugLogs
	StateLoading
	StateRootDetection
	StateOSInfo
	StateInstallingPackages
	StateInstallComplete
	StateError
	StateDetectingIPs
	StateDNSConfig
	StateDNSValidation
	StateDNSSuccess
	StateDNSFailed

	// Add new states here as you expand the application
	// StateConfigureNetwork
	// StateConfigureStorage
	// StateDeployingServices
	// etc.
)

// Additional model fields for DNS setup
type dnsInfo struct {
	InternalIP         string
	ExternalIP         string
	Domain             string
	ValidationStarted  bool
	ValidationSuccess  bool
	CloudflareDetected bool
	TestingStartTime   time.Time
	ValidationDuration time.Duration
}
