package network

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// IPInfo holds detected IP address information
type IPInfo struct {
	InternalIP string
	ExternalIP string
}

// DetectIPs attempts to detect the internal and external IP addresses
func DetectIPs(logFn func(string)) (*IPInfo, error) {
	ipInfo := &IPInfo{}

	// Detect internal IP
	logFn("Detecting internal IP address...")
	internalIP, err := detectInternalIP()
	if err != nil {
		logFn(fmt.Sprintf("Warning: Could not auto-detect internal IP: %v", err))
	} else {
		ipInfo.InternalIP = internalIP
		logFn(fmt.Sprintf("Detected internal IP: %s", internalIP))
	}

	// Detect external IP
	logFn("Detecting external IP address...")
	externalIP, err := detectExternalIP()
	if err != nil {
		logFn(fmt.Sprintf("Warning: Could not auto-detect external IP: %v", err))
	} else {
		ipInfo.ExternalIP = externalIP
		logFn(fmt.Sprintf("Detected external IP: %s", externalIP))
	}

	return ipInfo, nil
}

// detectInternalIP attempts to detect the internal IP address
func detectInternalIP() (string, error) {
	// First try: Use Go's net package
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String(), nil
	}

	// Second try: Run ip route command
	cmd := exec.Command("ip", "route", "get", "1")
	output, err := cmd.Output()
	if err == nil {
		re := regexp.MustCompile(`src\s+(\d+\.\d+\.\d+\.\d+)`)
		matches := re.FindSubmatch(output)
		if len(matches) > 1 {
			return string(matches[1]), nil
		}
	}

	// Third try: Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		// Skip loopback and not up interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			v4 := ipNet.IP.To4()
			if v4 == nil || v4[0] == 127 { // Skip loopback addresses
				continue
			}

			return v4.String(), nil
		}
	}

	return "", fmt.Errorf("could not detect internal IP address")
}

// detectExternalIP attempts to detect the external IP address
func detectExternalIP() (string, error) {
	// List of services to try
	services := []string{
		"https://ifconfig.me",
		"https://api.ipify.org",
		"https://ipinfo.io/ip",
		"https://checkip.amazonaws.com",
	}

	ipRegex := regexp.MustCompile(`^(\d{1,3}\.){3}\d{1,3}$`)
	client := &http.Client{Timeout: 5 * time.Second}

	for _, service := range services {
		resp, err := client.Get(service)
		if err != nil {
			continue
		}

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		ip := strings.TrimSpace(string(body))
		if ipRegex.MatchString(ip) {
			return ip, nil
		}
	}

	return "", fmt.Errorf("could not detect external IP address")
}

// ValidateIP checks if a provided IP address is valid
func ValidateIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// ValidateDNS checks if a domain is correctly pointing to an IP
func ValidateDNS(domain, expectedIP string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Validating DNS for %s...", domain))

	// Check if domain resolves to the expected IP
	ips, err := net.LookupHost(domain)
	if err != nil {
		logFn(fmt.Sprintf("DNS lookup failed: %v", err))
		return false
	}

	logFn(fmt.Sprintf("Domain %s resolves to: %v", domain, ips))

	for _, ip := range ips {
		if ip == expectedIP {
			logFn(fmt.Sprintf("Domain correctly points to %s ✓", expectedIP))
			return true
		}
	}

	logFn(fmt.Sprintf("Domain does not point to expected IP %s", expectedIP))
	return false
}

// CheckCloudflareProxy attempts to detect if a domain is behind Cloudflare
func CheckCloudflareProxy(domain string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Checking if %s is using Cloudflare...", domain))

	url := fmt.Sprintf("https://%s", domain)
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Don't follow redirects
		},
	}

	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		logFn(fmt.Sprintf("Error creating request: %v", err))
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		logFn(fmt.Sprintf("Error checking domain: %v", err))
		return false
	}
	defer resp.Body.Close()

	cfRay := resp.Header.Get("CF-RAY")
	cfCache := resp.Header.Get("CF-Cache-Status")

	if cfRay != "" || cfCache != "" {
		logFn("Cloudflare detected ✓")
		return true
	}

	logFn("No Cloudflare headers detected")
	return false
}

// TestWildcardDNS tests if a wildcard DNS is correctly configured
func TestWildcardDNS(baseDomain string, externalIP string, logFn func(string)) bool {
	// Test the base domain
	if ValidateDNS(baseDomain, externalIP, logFn) {
		logFn(fmt.Sprintf("Base domain %s is correctly configured ✓", baseDomain))
	} else {
		logFn(fmt.Sprintf("Base domain %s is not pointing to %s", baseDomain, externalIP))
		return false
	}

	// Test a random subdomain to verify wildcard configuration
	randomSubdomain := fmt.Sprintf("test-%d.%s", time.Now().Unix(), baseDomain)
	if ValidateDNS(randomSubdomain, externalIP, logFn) {
		logFn(fmt.Sprintf("Wildcard DNS for *.%s is correctly configured ✓", baseDomain))
		return true
	} else {
		logFn(fmt.Sprintf("Wildcard DNS for *.%s is not correctly configured", baseDomain))
		return false
	}
}

// RunNetworkCommand executes a network-related command and returns its output
func RunNetworkCommand(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
