#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Check for root privileges
if [ "$(id -u)" != "0" ]; then
   echo -e "${RED}Error: This script must be run as root${NC}"
   echo "Please run with sudo or as root user"
   exit 1
fi

# Check if running on Linux
if [[ "$(uname)" != "Linux" ]]; then
    echo -e "${RED}Error: This installer only supports Linux systems${NC}"
    exit 1
fi

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${RED}Error: Unsupported architecture: $ARCH${NC}"
        echo "This installer only supports x86_64 (amd64) and aarch64 (arm64) architectures"
        exit 1
        ;;
esac

# Get the latest release version
LATEST_VERSION=$(curl -s https://api.github.com/repos/unbindapp/unbind-installer/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_VERSION" ]; then
    echo -e "${RED}Error: Could not fetch latest version${NC}"
    exit 1
fi

echo -e "${GREEN}Installing Unbind Installer version $LATEST_VERSION for $ARCH...${NC}"

# Download and install
TEMP_DIR=$(mktemp -d)
INSTALLER_PATH="$TEMP_DIR/unbind-installer"

# Download the gzipped binary
curl -L "https://github.com/unbindapp/unbind-installer/releases/download/$LATEST_VERSION/unbind-installer-$ARCH.gz" -o "$INSTALLER_PATH.gz"

# Decompress the binary
gunzip "$INSTALLER_PATH.gz"
chmod +x "$INSTALLER_PATH"

# Execute the installer
"$INSTALLER_PATH"

# Cleanup
rm -rf "$TEMP_DIR" 