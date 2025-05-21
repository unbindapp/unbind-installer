package pkgmanager

import "fmt"

// ProgressFunc is a function to report progress during package installation
type ProgressFunc func(packageName string, progress float64, step string, isComplete bool)

// PackageManager defines the interface for package managers
type PackageManager interface {
	InstallPackages(packages []string, progressFunc ProgressFunc) error
}

// NewPackageManager creates a new package manager based on the distribution
func NewPackageManager(distribution string, logChan chan<- string) (PackageManager, error) {
	switch distribution {
	case "ubuntu", "debian":
		return NewAptInstaller(logChan), nil
	case "fedora", "centos":
		return NewDNFInstaller(logChan), nil
	case "opensuse":
		return NewZypperInstaller(logChan), nil
	default:
		return nil, fmt.Errorf("unsupported distribution: %s", distribution)
	}
}
