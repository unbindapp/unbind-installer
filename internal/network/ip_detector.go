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

// CheckCloudflareProxy checks if a domain is proxied through Cloudflare using official IP ranges
func CheckCloudflareProxy(domain string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Checking if %s is using Cloudflare...", domain))

	// Use Go's built-in DNS resolution
	ips, err := net.LookupIP(domain)
	if err != nil {
		logFn(fmt.Sprintf("Error looking up IP addresses: %v", err))
		return false
	}

	if len(ips) == 0 {
		logFn(fmt.Sprintf("No IP addresses found for %s", domain))
		return false
	}

	// Fetch Cloudflare's IP ranges
	ipv4Ranges, ipv6Ranges, err := fetchCloudflareIPRanges(logFn)
	if err != nil {
		logFn(fmt.Sprintf("Error fetching Cloudflare IP ranges: %v", err))
		return false
	}

	// Check if any IP is in Cloudflare's ranges
	for _, ip := range ips {
		ipStr := ip.String()
		logFn(fmt.Sprintf("Domain resolves to IP: %s", ipStr))

		// Check IPv4 or IPv6 based on IP type
		if ip.To4() != nil {
			// IPv4
			for _, ipNet := range ipv4Ranges {
				if ipNet.Contains(ip) {
					logFn(fmt.Sprintf("IP %s is in Cloudflare IPv4 range ✓", ipStr))
					return true
				}
			}
		} else {
			// IPv6
			for _, ipNet := range ipv6Ranges {
				if ipNet.Contains(ip) {
					logFn(fmt.Sprintf("IP %s is in Cloudflare IPv6 range ✓", ipStr))
					return true
				}
			}
		}
	}

	logFn("Domain is not using Cloudflare proxying")
	return false
}

// fetchCloudflareIPRanges fetches Cloudflare's IP ranges from their official URLs
func fetchCloudflareIPRanges(logFn func(string)) ([]*net.IPNet, []*net.IPNet, error) {
	ipv4URL := "https://www.cloudflare.com/ips-v4"
	ipv6URL := "https://www.cloudflare.com/ips-v6"

	logFn("Fetching Cloudflare IPv4 ranges...")
	ipv4Ranges, err := fetchIPRanges(ipv4URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch IPv4 ranges: %w", err)
	}

	logFn("Fetching Cloudflare IPv6 ranges...")
	ipv6Ranges, err := fetchIPRanges(ipv6URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch IPv6 ranges: %w", err)
	}

	// Parse CIDR ranges
	var parsedIPv4Ranges []*net.IPNet
	for _, cidr := range ipv4Ranges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logFn(fmt.Sprintf("Error parsing CIDR %s: %v", cidr, err))
			continue
		}
		parsedIPv4Ranges = append(parsedIPv4Ranges, ipNet)
	}

	var parsedIPv6Ranges []*net.IPNet
	for _, cidr := range ipv6Ranges {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logFn(fmt.Sprintf("Error parsing CIDR %s: %v", cidr, err))
			continue
		}
		parsedIPv6Ranges = append(parsedIPv6Ranges, ipNet)
	}

	logFn(fmt.Sprintf("Fetched %d IPv4 ranges and %d IPv6 ranges",
		len(parsedIPv4Ranges), len(parsedIPv6Ranges)))

	return parsedIPv4Ranges, parsedIPv6Ranges, nil
}

// fetchIPRanges fetches IP ranges from a URL and returns them as a slice of strings
func fetchIPRanges(url string) ([]string, error) {
	// Create HTTP client with reasonable timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Make the request
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Split by newlines and clean up
	ranges := strings.Split(string(body), "\n")
	var cleanRanges []string

	for _, r := range ranges {
		r = strings.TrimSpace(r)
		if r != "" {
			cleanRanges = append(cleanRanges, r)
		}
	}

	return cleanRanges, nil
}

// ValidateDNS checks if a domain is correctly pointing to an IP using Go's net package
func ValidateDNS(domain string, expectedIP string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Validating DNS for %s...", domain))

	// Use Go's built-in DNS lookup
	ips, err := net.LookupHost(domain)
	if err != nil {
		logFn(fmt.Sprintf("DNS lookup failed: %v", err))
		return false
	}

	if len(ips) == 0 {
		logFn(fmt.Sprintf("No IP addresses found for %s", domain))
		return false
	}

	// Log all found IPs
	logFn(fmt.Sprintf("Domain %s resolves to: %s", domain, strings.Join(ips, ", ")))

	// Check if any IP matches the expected IP
	for _, ip := range ips {
		if ip == expectedIP {
			logFn(fmt.Sprintf("Domain %s correctly points to %s ✓", domain, expectedIP))
			return true
		}
	}

	logFn(fmt.Sprintf("Domain does not point to expected IP %s", expectedIP))
	return false
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
