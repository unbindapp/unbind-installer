package osinfo

import (
	"fmt"
	"runtime"
	"slices"

	"github.com/unbindapp/unbind-installer/internal/errdefs"
)

// SupportedDistros is a list of supported Linux distributions
var AllSupportedDistros = []string{
	"ubuntu",
}

var AllSupportedDistrosVersions = map[string][]string{
	"ubuntu": {
		"22.04",
		"24.04",
	},
}

// OSInfo holds information about the operating system
type OSInfo struct {
	Distribution string
	Version      string
	VersionID    string
	PrettyName   string
	Architecture string
}

// Allow mock
var getOoosFunc = getGoos

func getGoos() string {
	return runtime.GOOS
}

var getArchFunc = getGoarch

func getGoarch() string {
	return runtime.GOARCH
}

// GetOSInfo detects the current OS and returns information about it
func GetOSInfo() (*OSInfo, error) {
	if getOoosFunc() != "linux" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeNotLinux, fmt.Sprintf("Only linux is supported, but I found %s", runtime.GOOS))
	}

	if getGoarch() != "amd64" && getGoarch() != "arm64" {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeInvalidArchitecture, fmt.Sprintf("Only amd64 and arm64 are supported, but I found %s", runtime.GOARCH))
	}

	// Start with basic OS info from Go runtime
	info := &OSInfo{
		Architecture: getArchFunc(),
	}

	err := detectLinuxDistro(info)
	if err != nil {
		return nil, err
	}

	// Check if the detected distribution is supported
	if !slices.Contains(AllSupportedDistros, info.Distribution) {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedDistribution, fmt.Sprintf("Unsupported distribution: %s", info.Distribution))
	}

	// Check if the detected version is supported
	if versions, ok := AllSupportedDistrosVersions[info.Distribution]; ok {
		if !slices.Contains(versions, info.VersionID) {
			return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedVersion, fmt.Sprintf("Unsupported version: %s", info.VersionID))
		}
	} else {
		return nil, errdefs.NewCustomError(errdefs.ErrTypeUnsupportedVersion, fmt.Sprintf("Unsupported version: %s", info.VersionID))
	}

	return info, nil
}
