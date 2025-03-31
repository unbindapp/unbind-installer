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

// IPInfo holds detected IP address information.
type IPInfo struct {
	InternalIP string
	ExternalIP string
}

// DetectIPs attempts to detect the internal and external IP addresses.
func DetectIPs(logFn func(string)) (*IPInfo, error) {
	ipInfo := &IPInfo{}

	logFn("Detecting internal IP address...")
	internalIP, err := detectInternalIP()
	if err != nil {
		logFn(fmt.Sprintf("Warning: could not auto-detect internal IP: %v", err))
	} else {
		ipInfo.InternalIP = internalIP
		logFn(fmt.Sprintf("Detected internal IP: %s", internalIP))
	}

	logFn("Detecting external IP address...")
	externalIP, err := detectExternalIP()
	if err != nil {
		logFn(fmt.Sprintf("Warning: could not auto-detect external IP: %v", err))
	} else {
		ipInfo.ExternalIP = externalIP
		logFn(fmt.Sprintf("Detected external IP: %s", externalIP))
	}

	return ipInfo, nil
}

// detectInternalIP tries to detect the machine's internal IP using a few different methods.
func detectInternalIP() (string, error) {
	// 1) Use a UDP dial to a public IP (like 8.8.8.8) – a common trick to figure out default interface.
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		localAddr := conn.LocalAddr().(*net.UDPAddr)
		return localAddr.IP.String(), nil
	}

	// 2) Fallback: Parse the output of `ip route get 1`.
	cmd := exec.Command("ip", "route", "get", "1")
	output, err := cmd.Output()
	if err == nil {
		re := regexp.MustCompile(`src\s+(\d+\.\d+\.\d+\.\d+)`)
		if matches := re.FindSubmatch(output); len(matches) > 1 {
			return string(matches[1]), nil
		}
	}

	// 3) As a last fallback, iterate network interfaces.
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("could not list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		// Skip loopback or interfaces that are down.
		if (iface.Flags&net.FlagLoopback) != 0 || (iface.Flags&net.FlagUp) == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				v4 := ipNet.IP.To4()
				if v4 != nil && v4[0] != 127 { // Skip 127.x.x.x
					return v4.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("could not detect internal IP address")
}

// detectExternalIP attempts to detect the external IP address via HTTP-based services.
func detectExternalIP() (string, error) {
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
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
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

// ValidateIP checks if a provided IP address is valid.
func ValidateIP(ip string) bool {
	return net.ParseIP(ip) != nil
}

// ValidateDNS checks if a domain (or wildcard domain) points to a given expected IP.
// It uses our robust DNS lookup method as a single source of truth.
func ValidateDNS(domain, expectedIP string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Validating DNS for %s...", domain))

	isWildcard := strings.HasPrefix(domain, "*.")
	testDomain := domain
	baseDomain := domain

	// If wildcard, test a random subdomain as well.
	if isWildcard {
		baseDomain = strings.TrimPrefix(domain, "*.")
		testDomain = fmt.Sprintf("test-%d.%s", time.Now().Unix(), baseDomain)
		logFn(fmt.Sprintf("Testing wildcard DNS record using subdomain: %s", testDomain))
	}

	ips, err := resolveDomainIPs(testDomain, logFn)
	if err != nil {
		logFn(fmt.Sprintf("DNS resolution failed for %s: %v", testDomain, err))
		return false
	}
	if len(ips) == 0 {
		logFn(fmt.Sprintf("No IP addresses resolved for %s", testDomain))
		return false
	}

	ipList := make([]string, 0, len(ips))
	for _, ip := range ips {
		ipList = append(ipList, ip.String())
	}
	logFn(fmt.Sprintf("%s resolves to: %s", testDomain, strings.Join(ipList, ", ")))

	// Check if any of the resolved IPs matches the expected IP.
	for _, ip := range ips {
		if ip.String() == expectedIP {
			// If it's a wildcard, mention that specifically.
			if isWildcard {
				logFn(fmt.Sprintf("Wildcard DNS for *.%s correctly points to %s ✓", baseDomain, expectedIP))
			} else {
				logFn(fmt.Sprintf("Domain %s correctly points to %s ✓", domain, expectedIP))
			}
			return true
		}
	}

	// If we get here, no match found.
	if isWildcard {
		logFn(fmt.Sprintf("Wildcard DNS for *.%s is not correctly configured", baseDomain))
	} else {
		logFn(fmt.Sprintf("Domain %s does not point to the expected IP", domain))
	}
	logFn(fmt.Sprintf("Expected: %s, found: %s", expectedIP, strings.Join(ipList, ", ")))
	return false
}

// resolveDomainIPs attempts to resolve a domain's IP addresses with fallback methods:
// 1) dig command (if available)
// 2) nslookup command (if available)
// 3) net.LookupIP (Go's system resolver)
func resolveDomainIPs(domain string, logFn func(string)) ([]net.IP, error) {
	logFn(fmt.Sprintf("Resolving IP for domain: %s", domain))

	// 1) Try dig command:
	if ips, err := resolveWithDig(domain); err == nil && len(ips) > 0 {
		logFn(fmt.Sprintf("Found %d IP(s) via dig", len(ips)))
		return ips, nil
	}

	// 2) Try nslookup:
	if ips, err := resolveWithNslookup(domain); err == nil && len(ips) > 0 {
		logFn(fmt.Sprintf("Found %d IP(s) via nslookup", len(ips)))
		return ips, nil
	}

	// 3) Fallback to net.LookupIP:
	ips, err := net.LookupIP(domain)
	if err != nil {
		return nil, fmt.Errorf("system resolver lookup failed: %w", err)
	}
	return ips, nil
}

// resolveWithDig uses the "dig +short" command to get A and AAAA records.
func resolveWithDig(domain string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var allIPs []net.IP

	// A records
	cmd := exec.CommandContext(ctx, "dig", "+short", domain, "A")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if ip := net.ParseIP(line); ip != nil {
				allIPs = append(allIPs, ip)
			}
		}
	}

	// AAAA records
	cmd = exec.CommandContext(ctx, "dig", "+short", domain, "AAAA")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if ip := net.ParseIP(line); ip != nil {
				allIPs = append(allIPs, ip)
			}
		}
	}

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("dig command failed or found no IPs")
	}
	return allIPs, nil
}

// resolveWithNslookup uses the "nslookup" command to query domain A/AAAA records
// from a couple of known DNS servers (e.g. 1.1.1.1, 8.8.8.8).
func resolveWithNslookup(domain string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dnsServers := []string{"1.1.1.1", "8.8.8.8"}
	var allIPs []net.IP

	for _, server := range dnsServers {
		cmd := exec.CommandContext(ctx, "nslookup", domain, server)
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Typically looks like: "Address: 1.2.3.4"
			if strings.HasPrefix(line, "Address:") && !strings.Contains(line, "#") {
				fields := strings.Fields(line)
				ipStr := fields[len(fields)-1]
				if ip := net.ParseIP(ipStr); ip != nil {
					allIPs = append(allIPs, ip)
				}
			}
		}
		if len(allIPs) > 0 {
			return allIPs, nil
		}
	}

	if len(allIPs) == 0 {
		return nil, fmt.Errorf("nslookup command failed or found no IPs")
	}
	return allIPs, nil
}

// TestWildcardDNS tests if both the base domain and a random subdomain
// for your wildcard DNS point to the given external IP.
func TestWildcardDNS(baseDomain, externalIP string, logFn func(string)) bool {
	if !ValidateDNS(baseDomain, externalIP, logFn) {
		logFn(fmt.Sprintf("Base domain %s is not pointing to %s", baseDomain, externalIP))
		return false
	}
	logFn(fmt.Sprintf("Base domain %s is correctly configured ✓", baseDomain))

	// Test random subdomain for wildcard
	randomSubdomain := fmt.Sprintf("test-%d.%s", time.Now().Unix(), baseDomain)
	if !ValidateDNS(randomSubdomain, externalIP, logFn) {
		logFn(fmt.Sprintf("Wildcard DNS for *.%s is not correctly configured", baseDomain))
		return false
	}
	logFn(fmt.Sprintf("Wildcard DNS for *.%s is correctly configured ✓", baseDomain))
	return true
}

// CheckCloudflareProxy checks if a domain is proxied through Cloudflare
// by resolving its IP, then checking if that IP is within Cloudflare's known ranges.
func CheckCloudflareProxy(domain string, logFn func(string)) bool {
	logFn(fmt.Sprintf("Checking if %s is using Cloudflare proxy...", domain))

	ips, err := resolveDomainIPs(domain, logFn)
	if err != nil {
		logFn(fmt.Sprintf("Error resolving %s: %v", domain, err))
		return false
	}
	if len(ips) == 0 {
		logFn("No IP addresses found for domain.")
		return false
	}

	// Fetch Cloudflare official IP ranges (IPv4 and IPv6).
	ipv4Ranges, ipv6Ranges, err := fetchCloudflareIPRanges(logFn)
	if err != nil {
		logFn(fmt.Sprintf("Error fetching Cloudflare IP ranges: %v", err))
		return false
	}

	// Check whether any resolved IP is in Cloudflare's range.
	for _, ip := range ips {
		logFn(fmt.Sprintf("Resolved IP: %s", ip.String()))
		if ip.To4() != nil {
			// IPv4 check
			for _, cfNet := range ipv4Ranges {
				if cfNet.Contains(ip) {
					logFn(fmt.Sprintf("IP %s is in Cloudflare's IPv4 range ✓", ip.String()))
					return true
				}
			}
		} else {
			// IPv6 check
			for _, cfNet := range ipv6Ranges {
				if cfNet.Contains(ip) {
					logFn(fmt.Sprintf("IP %s is in Cloudflare's IPv6 range ✓", ip.String()))
					return true
				}
			}
		}
	}

	logFn("Domain is not using Cloudflare proxy.")
	return false
}

// fetchCloudflareIPRanges retrieves the official Cloudflare IPv4/IPv6 subnets.
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

	var parsedIPv4Ranges []*net.IPNet
	for _, cidr := range ipv4Ranges {
		_, ipNet, parseErr := net.ParseCIDR(cidr)
		if parseErr == nil {
			parsedIPv4Ranges = append(parsedIPv4Ranges, ipNet)
		} else {
			logFn(fmt.Sprintf("Error parsing IPv4 CIDR %s: %v", cidr, parseErr))
		}
	}

	var parsedIPv6Ranges []*net.IPNet
	for _, cidr := range ipv6Ranges {
		_, ipNet, parseErr := net.ParseCIDR(cidr)
		if parseErr == nil {
			parsedIPv6Ranges = append(parsedIPv6Ranges, ipNet)
		} else {
			logFn(fmt.Sprintf("Error parsing IPv6 CIDR %s: %v", cidr, parseErr))
		}
	}

	logFn(fmt.Sprintf("Fetched %d IPv4 ranges, %d IPv6 ranges", len(parsedIPv4Ranges), len(parsedIPv6Ranges)))
	return parsedIPv4Ranges, parsedIPv6Ranges, nil
}

// fetchIPRanges is a helper that grabs a newline-delimited list of CIDRs from the given URL.
func fetchIPRanges(url string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status code: %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(body), "\n")

	var cleanRanges []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanRanges = append(cleanRanges, line)
		}
	}
	return cleanRanges, nil
}

// RunNetworkCommand executes a network-related shell command and returns its output.
func RunNetworkCommand(command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}
