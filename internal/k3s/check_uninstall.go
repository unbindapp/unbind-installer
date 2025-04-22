package k3s

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/unbindapp/unbind-installer/internal/errdefs"
)

const (
	// K3sUninstallScriptPath is the default location of the K3s uninstall script.
	K3sUninstallScriptPath = "/usr/local/bin/k3s-uninstall.sh"
)

// CheckResult holds the result of the K3s check.
type CheckResult struct {
	IsInstalled     bool
	UninstallScript string // Path to the uninstall script if found
}

// CheckInstalled checks if a K3s installation (specifically the uninstall script) exists.
func CheckInstalled() (*CheckResult, error) {
	result := &CheckResult{}

	// Check for server uninstall script
	if _, err := os.Stat(K3sUninstallScriptPath); err == nil {
		result.IsInstalled = true
		result.UninstallScript = K3sUninstallScriptPath
		return result, nil
	} else if !os.IsNotExist(err) {
		// Other error (e.g., permissions)
		return nil, fmt.Errorf("error checking for %s: %w", K3sUninstallScriptPath, err)
	}

	// Not found
	result.IsInstalled = false
	return result, nil
}

// runCommand executes an external command and logs its output.
func runCommand(logChan chan<- string, command string, args ...string) error {
	cmdStr := command + " " + strings.Join(args, " ")
	logChan <- fmt.Sprintf("Executing: %s", cmdStr)

	cmd := exec.Command(command, args...)
	var stdOut, stdErr bytes.Buffer
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr

	err := cmd.Run()

	if stdOut.Len() > 0 {
		logChan <- fmt.Sprintf("Stdout from '%s':\n%s", cmdStr, stdOut.String())
	}
	// Log stderr only if there's an error or stderr output exists
	if err != nil || stdErr.Len() > 0 {
		logChan <- fmt.Sprintf("Stderr from '%s':\n%s", cmdStr, stdErr.String())
	}

	if err != nil {
		logChan <- fmt.Sprintf("Error executing '%s': %v", cmdStr, err)
		return fmt.Errorf("failed to execute '%s': %w", cmdStr, err)
	}

	logChan <- fmt.Sprintf("Successfully executed: %s", cmdStr)
	return nil
}

// Uninstall executes the K3s uninstall script.
func Uninstall(uninstallScriptPath string, logChan chan<- string) error {
	if os.Geteuid() != 0 {
		return errdefs.ErrNotRoot // Ensure we run as root
	}
	if _, err := os.Stat(uninstallScriptPath); os.IsNotExist(err) {
		return fmt.Errorf("k3s uninstall script not found at %s", uninstallScriptPath)
	}

	logChan <- fmt.Sprintf("Executing K3s uninstall script: %s", uninstallScriptPath)

	cmd := exec.Command(uninstallScriptPath)
	var stdOut, stdErr bytes.Buffer
	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr

	err := cmd.Run()

	// Log output even if there's an error
	if stdOut.Len() > 0 {
		logChan <- "Uninstall script stdout:"
		logChan <- stdOut.String()
	}
	if stdErr.Len() > 0 {
		logChan <- "Uninstall script stderr:"
		logChan <- stdErr.String()
	}

	if err != nil {
		logChan <- fmt.Sprintf("Error running K3s uninstall script: %v", err)
		return errdefs.NewCustomError(errdefs.ErrTypeK3sUninstallFailed, fmt.Sprintf("failed to run K3s uninstall script %s: %v\nStderr: %s", uninstallScriptPath, err, stdErr.String()))
	}

	logChan <- "K3s uninstall script executed successfully."

	// --- Clean up IPTables ---
	logChan <- "Attempting to flush iptables rules and delete chains..."
	iptablesCleanupSuccess := true // Track if cleanup succeeds

	// Commands to run
	iptablesCommands := [][]string{
		{"iptables", "-F"},                 // Flush all rules in filter table
		{"iptables", "-t", "nat", "-F"},    // Flush all rules in nat table
		{"iptables", "-t", "mangle", "-F"}, // Flush all rules in mangle table
		{"iptables", "-X"},                 // Delete all non-default chains
	}

	for _, cmdArgs := range iptablesCommands {
		err := runCommand(logChan, cmdArgs[0], cmdArgs[1:]...)
		if err != nil {
			// Log the error but continue trying other cleanup commands
			logChan <- fmt.Sprintf("Warning: Failed iptables cleanup command '%s': %v", strings.Join(cmdArgs, " "), err)
			iptablesCleanupSuccess = false // Mark cleanup as potentially incomplete
		}
	}

	if iptablesCleanupSuccess {
		logChan <- "iptables cleanup commands executed."
	} else {
		logChan <- "Warning: One or more iptables cleanup commands failed. Manual inspection might be needed."
	}

	logChan <- "K3s uninstall process finished."
	return nil
}
