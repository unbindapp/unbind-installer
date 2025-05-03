package installer

import (
	"fmt"
	"os"
	"path/filepath"
)

const managementScriptContent = `#!/bin/bash

# ANSI color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Box drawing characters
BOX_TOP="╔════════════════════════════════════════════════════════════╗"
BOX_MID="║"
BOX_BOT="╚════════════════════════════════════════════════════════════╝"

# Function to print the Unbind banner
print_banner() {
    echo -e "${GREEN}"
    echo " _   _       _     _           _"
    echo "| | | |_ __ | |__ (_)_ __   __| |"
    echo "| | | | '_ \| '_ \| | '_ \ / _  |"
    echo "| |_| | | | | |_) | | | | | (_| |"
    echo " \___/|_| |_|_.__/|_|_| |_|\__,_|"
    echo -e "${NC}"
}

# Unbind Management Script
# This script provides management functions for Unbind

# Configuration file location
CONFIG_FILE="/etc/unbind/config"

# Function to print a boxed message
print_box() {
    local message="$1"
    local color="$2"
    echo -e "${color}${BOX_TOP}${NC}"
    echo -e "${color}${BOX_MID}${NC} ${message} ${color}${BOX_MID}${NC}"
    echo -e "${color}${BOX_BOT}${NC}"
}

# Function to check if Unbind is installed
check_installation() {
    if [ ! -f "/usr/local/bin/k3s-uninstall.sh" ]; then
        print_banner
        print_box "Error: No Unbind installation detected." "$RED"
        echo -e "${RED}This script should only be run on a server with Unbind installed.${NC}"
        exit 1
    fi
}

# Function to show usage
show_usage() {
    print_banner
    print_box "Unbind Management Script" "$BLUE"
    echo -e "${BOLD}Usage:${NC} unbind-manage <command>"
    echo ""
    echo -e "${BOLD}Commands:${NC}"
    echo -e "  ${CYAN}uninstall${NC}    - Uninstall Unbind (${RED}WARNING: This will permanently delete all data${NC})"
    echo -e "  ${CYAN}add-node${NC}     - Show instructions for adding a new node"
    # echo ""
    # echo -e "${MAGENTA}For more information, visit https://unbind.app/docs${NC}"
}

# Function to handle uninstallation
handle_uninstall() {
    check_installation

    print_banner
    print_box "WARNING: Unbind Uninstallation" "$RED"
    echo -e "${RED}This will permanently delete all Unbind data and configurations.${NC}"
    echo -e "${RED}This action cannot be undone.${NC}"
    echo ""
    read -p "Are you sure you want to continue? (y/N) " -n 1 -r
    echo ""
    
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo -e "${YELLOW}Uninstalling Unbind...${NC}"
        /usr/local/bin/k3s-uninstall.sh
        print_banner
        print_box "Unbind has been uninstalled successfully." "$GREEN"
    else
        print_banner
        print_box "Uninstallation cancelled." "$BLUE"
    fi
}

# Function to show add node instructions
handle_add_node() {
    check_installation

    # Get the node token
    token=$(cat /var/lib/rancher/k3s/server/node-token 2>/dev/null)
    if [ -z "$token" ]; then
        print_banner
        print_box "Error: Could not find node token." "$RED"
        echo -e "${RED}Is K3S running?${NC}"
        exit 1
    fi

    # Get the server URL from config file
    if [ ! -f "$CONFIG_FILE" ]; then
        print_banner
        print_box "Error: Could not find Unbind configuration file." "$RED"
        echo -e "${RED}The cluster IP address is not available.${NC}"
        exit 1
    fi

    # Read the cluster IP from the config file
    cluster_ip=$(grep "^CLUSTER_IP=" "$CONFIG_FILE" | cut -d'=' -f2)
    if [ -z "$cluster_ip" ]; then
        print_banner
        print_box "Error: Could not find cluster IP in configuration." "$RED"
        exit 1
    fi

    server_url="https://${cluster_ip}:6443"

    print_banner
    print_box "Add Node Instructions" "$BLUE"
    echo -e "${BOLD}To add a new node to your Unbind cluster, run the following command on the new server:${NC}"
    echo ""
    echo -e "${CYAN}curl -sfL https://get.k3s.io | K3S_URL=$server_url K3S_TOKEN=$token sh -${NC}"
    echo ""
    echo -e "${YELLOW}Note:${NC} Make sure the new server can reach this server on port 6443"
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
