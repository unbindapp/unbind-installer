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
cd "$TEMP_DIR"

# Download the gzipped binary and its checksum
echo -e "${GREEN}Downloading installer...${NC}"
curl -L "https://github.com/unbindapp/unbind-installer/releases/download/$LATEST_VERSION/unbind-installer-$ARCH.gz" -o "installer.gz"
curl -L "https://github.com/unbindapp/unbind-installer/releases/download/$LATEST_VERSION/unbind-installer-$ARCH.gz.sha256" -o "expected.sha256"

# Get the expected checksum
EXPECTED_CHECKSUM=$(cat expected.sha256 | awk '{print $1}')

# Calculate actual checksum
echo -e "${GREEN}Verifying checksum...${NC}"
ACTUAL_CHECKSUM=$(sha256sum installer.gz | awk '{print $1}')

# Compare checksums
if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
    echo -e "${RED}Error: Checksum verification failed${NC}"
    echo "Expected: $EXPECTED_CHECKSUM"
    echo "Got:      $ACTUAL_CHECKSUM"
    echo "The downloaded file may be corrupted or tampered with"
    cd - > /dev/null
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Decompress the binary
echo -e "${GREEN}Decompressing installer...${NC}"
gunzip installer.gz
chmod +x installer

# Execute the installer
echo -e "${GREEN}Running installer...${NC}"
./installer

# Cleanup
cd - > /dev/null
rm -rf "$TEMP_DIR" 