package pkgmanager

import "fmt"

// PackageManager defines the interface for package managers
type PackageManager interface {
	InstallPackages(packages []string) error
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
