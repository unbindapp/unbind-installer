package pkgmanager

// PackageMapping defines the mapping of common package names to distribution-specific package names
var PackageMapping = map[string]map[string]string{
	"git": {
		"ubuntu":   "git",
		"debian":   "git",
		"fedora":   "git",
		"centos":   "git",
		"opensuse": "git",
	},
	"curl": {
		"ubuntu":   "curl",
		"debian":   "curl",
		"fedora":   "curl",
		"centos":   "curl",
		"opensuse": "curl",
	},
	"wget": {
		"ubuntu":   "wget",
		"debian":   "wget",
		"fedora":   "wget",
		"centos":   "wget",
		"opensuse": "wget",
	},
	"ca-certificates": {
		"ubuntu":   "ca-certificates",
		"debian":   "ca-certificates",
		"fedora":   "ca-certificates",
		"centos":   "ca-certificates",
		"opensuse": "ca-certificates",
	},
	"apt-transport-https": {
		"ubuntu":   "apt-transport-https",
		"debian":   "apt-transport-https",
		"fedora":   "", // Not needed on Fedora
		"centos":   "", // Not needed on CentOS
		"opensuse": "", // Not needed on OpenSUSE
	},
	"apache2-utils": {
		"ubuntu":   "apache2-utils",
		"debian":   "apache2-utils",
		"fedora":   "httpd-tools",
		"centos":   "httpd-tools",
		"opensuse": "apache2-utils",
	},
}

// GetDistributionPackages converts a list of common package names to distribution-specific package names
func GetDistributionPackages(distribution string, packages []string) []string {
	var result []string
	for _, pkg := range packages {
		if distPkg, ok := PackageMapping[pkg][distribution]; ok && distPkg != "" {
			result = append(result, distPkg)
		}
	}
	return result
}
