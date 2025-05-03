package installer

import (
	"fmt"
	"os"
	"path/filepath"
)

const managementScriptContent = `#!/bin/bash

# Unbind Management Script
# This script provides management functions for Unbind

# Configuration file location
CONFIG_FILE="/etc/unbind/config"

# Function to check if Unbind is installed
check_installation() {
    if [ ! -f "/usr/local/bin/k3s-uninstall.sh" ]; then
        echo "Error: No Unbind installation detected."
        echo "This script should only be run on a server with Unbind installed."
        exit 1
    fi
}

# Function to show usage
show_usage() {
    echo "Usage: unbind-manage <command>"
    echo ""
    echo "Commands:"
    echo "  uninstall    - Uninstall Unbind (WARNING: This will permanently delete all data)"
    echo "  add-node     - Show instructions for adding a new node"
    echo ""
    echo "For more information, visit https://unbind.app/docs"
}

# Function to handle uninstallation
handle_uninstall() {
    check_installation

    echo "WARNING: This will permanently delete all Unbind data and configurations."
    echo "This action cannot be undone."
    echo ""
    read -p "Are you sure you want to continue? (y/N) " -n 1 -r
    echo ""
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Uninstalling Unbind..."
        /usr/local/bin/k3s-uninstall.sh
        echo "Unbind has been uninstalled."
    else
        echo "Uninstallation cancelled."
    fi
}

# Function to show add node instructions
handle_add_node() {
    check_installation

    # Get the node token
    token=$(cat /var/lib/rancher/k3s/server/node-token 2>/dev/null)
    if [ -z "$token" ]; then
        echo "Error: Could not find node token. Is K3S running?"
        exit 1
    fi

    # Get the server URL from config file
    if [ ! -f "$CONFIG_FILE" ]; then
        echo "Error: Could not find Unbind configuration file."
        echo "The cluster IP address is not available."
        exit 1
    fi

    # Read the cluster IP from the config file
    cluster_ip=$(grep "^CLUSTER_IP=" "$CONFIG_FILE" | cut -d'=' -f2)
    if [ -z "$cluster_ip" ]; then
        echo "Error: Could not find cluster IP in configuration."
        exit 1
    fi

    server_url="https://${cluster_ip}:6443"

    echo "To add a new node to your Unbind cluster, run the following command on the new server:"
    echo ""
    echo "curl -sfL https://get.k3s.io | K3S_URL=$server_url K3S_TOKEN=$token sh -"
    echo ""
    echo "Note: Make sure the new server can reach this server on port 6443"
}

# Main script logic
case "$1" in
    "uninstall")
        handle_uninstall
        ;;
    "add-node")
        handle_add_node
        ;;
    *)
        show_usage
        exit 1
        ;;
esac
`

// InstallManagementScript installs the management script to the system
func InstallManagementScript(clusterIP string) error {
	// Create config directory if it doesn't exist
	configDir := "/etc/unbind"
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Create config file with cluster IP
	configPath := filepath.Join(configDir, "config")
	configContent := fmt.Sprintf("CLUSTER_IP=%s\n", clusterIP)
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	// Create the script file
	scriptPath := "/usr/local/bin/unbind-manage"
	if err := os.WriteFile(scriptPath, []byte(managementScriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write management script: %w", err)
	}

	return nil
}
