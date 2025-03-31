package network

import (
	"bytes"
	"context"
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
// It tries system DNS first, then falls back to public DNS servers if needed
func ValidateDNS(domain, expectedIP string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Validating DNS for %s...", domain))

	// First check if this is a wildcard domain request
	isWildcard := strings.HasPrefix(domain, "*.")

	// If wildcard, modify domain for testing
	testDomain := domain
	baseDomain := domain
	if isWildcard {
		// Extract base domain for testing
		baseDomain = strings.TrimPrefix(domain, "*.")

		// Create a random subdomain for testing the wildcard
		randomStr := fmt.Sprintf("test-%d", time.Now().Unix())
		testDomain = randomStr + "." + baseDomain

		logFn(fmt.Sprintf("Testing wildcard *.%s using random subdomain: %s",
			baseDomain, testDomain))
	}

	// Try with Go's default resolver first
	logFn("Trying system DNS resolver...")
	ips, err := trySystemResolver(testDomain)

	// If system resolver fails, try public DNS servers
	if err != nil {
		logFn(fmt.Sprintf("System DNS resolver failed: %v", err))
		logFn("Falling back to public DNS servers...")
		ips, err = tryPublicDNSServers(testDomain, logFn)

		if err != nil {
			logFn(fmt.Sprintf("All DNS resolution attempts failed for %s: %v", testDomain, err))
			return false
		}
	}

	// If we get here, we have some IPs
	ipList := strings.Join(ips, ", ")
	logFn(fmt.Sprintf("Domain %s resolves to: %s", testDomain, ipList))

	// Check if any IP matches the expected IP
	for _, ip := range ips {
		if ip == expectedIP {
			if isWildcard {
				logFn(fmt.Sprintf("Wildcard DNS *.%s correctly points to %s ✓",
					baseDomain, expectedIP))
			} else {
				logFn(fmt.Sprintf("Domain %s correctly points to %s ✓",
					domain, expectedIP))
			}
			return true
		}
	}

	// If we get here, the domain doesn't point to the expected IP
	if isWildcard {
		logFn(fmt.Sprintf("Wildcard *.%s is not correctly configured", baseDomain))
		logFn(fmt.Sprintf("Expected: %s, found: %s", expectedIP, ipList))
		logFn(fmt.Sprintf("Please update your wildcard DNS record for *.%s", baseDomain))
	} else {
		logFn(fmt.Sprintf("Domain %s does not point to expected IP", domain))
		logFn(fmt.Sprintf("Expected: %s, found: %s", expectedIP, ipList))
		logFn("Please update your DNS A record")
	}

	return false
}

// trySystemResolver attempts to resolve a domain using the system's default resolver
func trySystemResolver(domain string) ([]string, error) {
	// Use context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try command-based resolution first (this might bypass Go's resolver issues)
	cmd := exec.CommandContext(ctx, "getent", "hosts", domain)
	output, err := cmd.Output()

	if err == nil && len(output) > 0 {
		// Parse getent output to extract IPs
		lines := strings.Split(string(output), "\n")
		var ips []string

		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				ips = append(ips, fields[0])
			}
		}

		if len(ips) > 0 {
			return ips, nil
		}
	}

	// If getent doesn't work, try Go's standard resolver
	return net.LookupHost(domain)
}

// tryPublicDNSServers attempts to resolve a domain using public DNS servers
func tryPublicDNSServers(domain string, logFn func(string)) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create a custom resolver using reliable public DNS servers
	for _, dnsServer := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Second * 5,
				}
				return d.DialContext(ctx, "udp", dnsServer)
			},
		}

		logFn(fmt.Sprintf("Trying DNS server: %s", dnsServer))
		ips, err := resolver.LookupHost(ctx, domain)

		if err == nil && len(ips) > 0 {
			logFn(fmt.Sprintf("Successfully resolved using %s", dnsServer))
			return ips, nil
		}

		if err != nil {
			logFn(fmt.Sprintf("Failed with %s: %v", dnsServer, err))
		}
	}

	// Last resort: Try to execute dig command directly
	logFn("Trying dig command as last resort...")
	cmd := exec.CommandContext(ctx, "dig", "+short", domain, "A")
	output, err := cmd.Output()

	if err == nil && len(output) > 0 {
		ips := strings.Split(strings.TrimSpace(string(output)), "\n")
		// Filter out empty lines
		var filteredIPs []string
		for _, ip := range ips {
			if ip != "" {
				filteredIPs = append(filteredIPs, ip)
			}
		}

		if len(filteredIPs) > 0 {
			logFn("Successfully resolved using dig command")
			return filteredIPs, nil
		}
	}

	return nil, fmt.Errorf("all resolution methods failed")
}

// CheckCloudflareProxy checks if a domain is proxied through Cloudflare by fetching
// Cloudflare's official IP ranges and checking if the domain's IPs fall within them
func CheckCloudflareProxy(domain string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Checking if %s is using Cloudflare...", domain))

	// Get IP addresses using the robust resolution method
	ips, err := resolveIPsRobustly(domain, logFn)
	if err != nil {
		logFn(fmt.Sprintf("Error looking up IP addresses: %v", err))
		return false
	}

	if len(ips) == 0 {
		logFn("No IP addresses found for domain")
		return false
	}

	// Fetch Cloudflare's official IP ranges
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

// resolveIPsRobustly attempts to resolve a domain's IP addresses using multiple methods
func resolveIPsRobustly(domain string, logFn func(string)) ([]net.IP, error) {
	// Try system resolver first
	logFn("Trying system DNS resolver...")
	ips, err := net.LookupIP(domain)
	if err == nil && len(ips) > 0 {
		return ips, nil
	}

	if err != nil {
		logFn(fmt.Sprintf("System DNS resolver failed: %v", err))
	} else {
		logFn("System DNS resolver returned no IPs")
	}

	// Try getent command
	logFn("Trying getent hosts command...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "getent", "hosts", domain)
	output, err := cmd.Output()

	if err == nil && len(output) > 0 {
		// Parse getent output to extract IPs
		lines := strings.Split(string(output), "\n")
		var parsedIPs []net.IP

		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				if ip := net.ParseIP(fields[0]); ip != nil {
					parsedIPs = append(parsedIPs, ip)
				}
			}
		}

		if len(parsedIPs) > 0 {
			logFn(fmt.Sprintf("Found %d IPs using getent", len(parsedIPs)))
			return parsedIPs, nil
		}
	}

	// Try public DNS servers
	logFn("Trying public DNS servers...")
	for _, dnsServer := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Second * 5,
				}
				return d.DialContext(ctx, "udp", dnsServer)
			},
		}

		logFn(fmt.Sprintf("Trying DNS server: %s", dnsServer))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		hostnames, err := resolver.LookupHost(ctx, domain)
		cancel()

		if err == nil && len(hostnames) > 0 {
			var resolvedIPs []net.IP
			for _, hostname := range hostnames {
				if ip := net.ParseIP(hostname); ip != nil {
					resolvedIPs = append(resolvedIPs, ip)
				}
			}

			if len(resolvedIPs) > 0 {
				logFn(fmt.Sprintf("Found %d IPs using %s", len(resolvedIPs), dnsServer))
				return resolvedIPs, nil
			}
		}
	}

	// Last resort: Try dig command
	logFn("Trying dig command as last resort...")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd = exec.CommandContext(ctx, "dig", "+short", domain, "A")
	output, err = cmd.Output()

	var digIPs []net.IP
	if err == nil && len(output) > 0 {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				if ip := net.ParseIP(line); ip != nil {
					digIPs = append(digIPs, ip)
				}
			}
		}
	}

	// Also try for AAAA records (IPv6)
	cmd = exec.CommandContext(ctx, "dig", "+short", domain, "AAAA")
	output, err = cmd.Output()

	if err == nil && len(output) > 0 {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				if ip := net.ParseIP(line); ip != nil {
					digIPs = append(digIPs, ip)
				}
			}
		}
	}

	if len(digIPs) > 0 {
		logFn(fmt.Sprintf("Found %d IPs using dig command", len(digIPs)))
		return digIPs, nil
	}

	return nil, fmt.Errorf("all resolution methods failed")
}

// fetchCloudflareIPRanges fetches the latest Cloudflare IP ranges from their official URLs
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
