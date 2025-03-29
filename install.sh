#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Function to log messages
log() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Function to check if script is run as root
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root or with sudo"
    fi
}

# Function to detect OS and version
detect_os() {
    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS=$NAME
        VERSION=$VERSION_ID
        log "Detected OS: $OS $VERSION"
        
        # Check if OS is supported
        if [ "$OS" != "Ubuntu" ]; then
            log_error "Unsupported OS: $OS. This script only supports Ubuntu."
        fi
        
        # Check if version is supported
        if [ "$VERSION" != "22.04" ] && [ "$VERSION" != "24.04" ]; then
            log_error "Unsupported Ubuntu version: $VERSION. This script only supports Ubuntu 22.04 and 24.04."
        fi
    else
        log_error "Cannot detect OS. /etc/os-release file not found."
    fi
}

# Function to install prerequisites
install_prerequisites() {
    log "Installing prerequisites..."
    apt-get update
    apt-get install -y curl wget apt-transport-https gnupg lsb-release net-tools iproute2 \
                       linux-headers-$(uname -r) make iptables
}

# Function to install K3S
install_k3s() {
    log "Installing K3S..."
    
    # Set up K3S installation environment variables
    export INSTALL_K3S_EXEC="--disable=traefik --disable=servicelb"
    export INSTALL_K3S_SKIP_ENABLE="true"  # Don't enable auto-updates
    
    # Download and run K3S install script
    curl -sfL https://get.k3s.io | sh -
    
    # Enable but don't start K3S service (we'll start it after additional configuration)
    systemctl enable k3s
    
    log "K3S installed successfully"
}

# Function to detect suitable IP range for load balancers
detect_ip_range() {
    log "Detecting IP range for Cilium LoadBalancer..."
    
    # Get the primary interface
    PRIMARY_INTERFACE=$(ip route | grep default | awk '{print $5}')
    
    if [ -z "$PRIMARY_INTERFACE" ]; then
        log_warning "Could not detect primary interface, trying to find any interface with an IP"
        PRIMARY_INTERFACE=$(ip -o -4 addr show | grep -v "127.0.0.1" | awk '{print $2}' | head -n 1)
        
        if [ -z "$PRIMARY_INTERFACE" ]; then
            log_error "Could not detect any network interface with an IP address"
        fi
    fi
    
    log "Detected primary interface: $PRIMARY_INTERFACE"
    
    # Get the subnet address
    SUBNET_INFO=$(ip -o -4 addr show "$PRIMARY_INTERFACE" | awk '{print $4}')
    
    if [ -z "$SUBNET_INFO" ]; then
        log_error "Could not detect subnet information for interface $PRIMARY_INTERFACE"
    fi
    
    # Extract network address and prefix
    NETWORK_ADDR=$(echo "$SUBNET_INFO" | cut -d'/' -f1)
    PREFIX=$(echo "$SUBNET_INFO" | cut -d'/' -f2)
    
    log "Network address: $NETWORK_ADDR, Prefix: $PREFIX"
    
    # Calculate base address for the range
    IFS='.' read -r -a IP_PARTS <<< "$NETWORK_ADDR"
    
    # Set a default range of 10 IPs at the upper end of the subnet
    START_IP="${IP_PARTS[0]}.${IP_PARTS[1]}.${IP_PARTS[2]}.240"
    END_IP="${IP_PARTS[0]}.${IP_PARTS[1]}.${IP_PARTS[2]}.250"
    
    log "Calculated IP range for LoadBalancer: $START_IP-$END_IP"
    
    # Export variables to be used later
    export LB_START_IP=$START_IP
    export LB_END_IP=$END_IP
}

# Function to install and configure Cilium
install_cilium() {
    log "Installing Cilium..."
    
    # Wait for K3S to be ready
    sleep 10
    
    # Install Cilium CLI
    CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
    CLI_ARCH=amd64
    if [ "$(uname -m)" = "aarch64" ]; then CLI_ARCH=arm64; fi
    curl -L --fail --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CLI_ARCH}.tar.gz
    tar xzvfC cilium-linux-${CLI_ARCH}.tar.gz /usr/local/bin
    rm cilium-linux-${CLI_ARCH}.tar.gz
    
    # Install Helm (required for Cilium installation)
    curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
    
    # Add Cilium Helm repository
    helm repo add cilium https://helm.cilium.io/
    helm repo update
    
    # Install Cilium with LoadBalancer capability
    helm install cilium cilium/cilium --version 1.15.1 \
      --namespace kube-system \
      --set kubeProxyReplacement=strict \
      --set k8sServiceHost=$(hostname -I | awk '{print $1}') \
      --set k8sServicePort=6443 \
      --set externalIPs.enabled=true \
      --set nodePort.enabled=true \
      --set hostPort.enabled=true \
      --set bpf.masquerade=true \
      --set ipam.mode=kubernetes \
      --set loadBalancer.mode=dsr \
      --set loadBalancer.algorithm=maglev \
      --set loadBalancer.acceleration=native \
      --set loadBalancer.serviceTopology=true \
      --set ipv4NativeRoutingCIDR="10.0.0.0/8" \
      --set ipv4.enabled=true \
      --set ipv6.enabled=false
    
    # Wait for Cilium to be ready
    sleep 30
    
    # Create a simplified IP pool definition with CIDR block instead of individual IPs
    # Calculate the network CIDR that includes our range
    # Extract the first three octets of the start IP
    NETWORK_PREFIX=$(echo "$LB_START_IP" | cut -d. -f1-3)
    
    # Create the Cilium LoadBalancer IP Pool with a /24 subnet
    cat <<EOF | kubectl apply -f -
apiVersion: "cilium.io/v2alpha1"
kind: CiliumLoadBalancerIPPool
metadata:
  name: "default-pool"
spec:
  cidrs:
  - cidr: "${NETWORK_PREFIX}.0/24"
EOF
    
    # Verify Cilium status
    cilium status --wait
    
    log "Cilium installed and configured successfully with LoadBalancer IP pool: ${NETWORK_PREFIX}.0/24"
}

# Function to create unbind-system namespace
create_unbind_namespace() {
    log "Creating unbind-system namespace..."
    
    # Create the namespace
    kubectl create namespace unbind-system
    
    # Create a basic configmap in the namespace to validate its creation
    kubectl create configmap unbind-config -n unbind-system --from-literal=created-by=install-script
    
    log "unbind-system namespace created successfully"
}

# Function to install and configure NGINX Ingress Controller
install_nginx_ingress() {
    log "Installing NGINX Ingress Controller..."
    
    # Apply NGINX Ingress Controller manifests
    kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.12.1/deploy/static/provider/cloud/deploy.yaml
    
    # Wait for NGINX Ingress Controller to be ready
    sleep 30
    
    log "NGINX Ingress Controller installed successfully"
}

# Function to verify installation
verify_installation() {
    log "Verifying installation..."
    
    # Check if K3S is running
    if systemctl is-active --quiet k3s; then
        log "K3S is running"
    else
        log_error "K3S is not running"
    fi
    
    # Check if Cilium is running
    if kubectl get pods -n kube-system -l k8s-app=cilium | grep -q Running; then
        log "Cilium is running"
    else
        log_error "Cilium is not running"
    fi
    
    # Check if Cilium LoadBalancer IP Pool is created
    if kubectl get ciliumloadbalancerippool default-pool &>/dev/null; then
        log "Cilium LoadBalancer IP Pool is created"
    else
        log_error "Cilium LoadBalancer IP Pool was not created"
    fi
    
    # Check if NGINX Ingress Controller is running
    if kubectl get pods -n ingress-nginx | grep -q Running; then
        log "NGINX Ingress Controller is running"
    else
        log_error "NGINX Ingress Controller is not running"
    fi
    
    # Check if unbind-system namespace exists
    if kubectl get namespace unbind-system &>/dev/null; then
        log "unbind-system namespace is created"
    else
        log_error "unbind-system namespace was not created"
    fi
    
    # Verify that LoadBalancer services can be created
    log "Testing LoadBalancer functionality..."
    
    # Create a test deployment and service
    cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-lb
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-lb
  template:
    metadata:
      labels:
        app: test-lb
    spec:
      containers:
      - name: nginx
        image: nginx:stable
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: test-lb-svc
  namespace: default
spec:
  type: LoadBalancer
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: test-lb
EOF
    
    # Wait for the service to get an external IP
    sleep 20
    
    # Check if the service got an external IP
    EXTERNAL_IP=$(kubectl get svc test-lb-svc -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    if [ -n "$EXTERNAL_IP" ]; then
        log "LoadBalancer test successful! External IP: $EXTERNAL_IP"
        # Clean up the test resources
        kubectl delete svc test-lb-svc
        kubectl delete deployment test-lb
    else
        log_warning "LoadBalancer service did not receive an external IP. You may need to troubleshoot Cilium load balancer configuration."
    fi
    
    log "Verification completed successfully"
}

# Main function
main() {
    log "Starting K3S installation script..."
    
    check_root
    detect_os
    install_prerequisites
    detect_ip_range
    install_k3s
    
    # Start K3S service
    systemctl start k3s
    
    # Wait for K3S to be ready
    sleep 30
    
    # Set KUBECONFIG environment variable
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    
    install_cilium
    create_unbind_namespace
    install_nginx_ingress
    verify_installation
    
    # Print summary
    log "K3S installation completed successfully"
}

# Run main function
main