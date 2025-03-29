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
    
    # Set up K3S installation environment variables with flannel disabled and traefik disabled
    export INSTALL_K3S_EXEC='--flannel-backend=none --disable-network-policy --disable=traefik --disable=servicelb'
    
    # Download and run K3S install script
    curl -sfL https://get.k3s.io | sh -
    
    log "K3S installed successfully"
}

# Function to install and configure Cilium
install_cilium() {
    log "Installing Cilium CLI..."
    
    # Install Cilium CLI exactly as in the guide
    CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
    CLI_ARCH=amd64
    if [ "$(uname -m)" = "aarch64" ]; then CLI_ARCH=arm64; fi
    curl -L --fail --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CLI_ARCH}.tar.gz{,.sha256sum}
    sha256sum --check cilium-linux-${CLI_ARCH}.tar.gz.sha256sum
    tar xzvfC cilium-linux-${CLI_ARCH}.tar.gz /usr/local/bin
    rm cilium-linux-${CLI_ARCH}.tar.gz{,.sha256sum}
    
    log "Cilium CLI installed successfully"
    
    # Set KUBECONFIG
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    
    # Wait for K3S to be ready
    sleep 10
    
    log "Installing Cilium..."
    # Install Cilium using the CLI with correct K3S PodCIDR and LoadBalancer configuration
    cilium install --version 1.17.2 \
      --set ipam.operator.clusterPoolIPv4PodCIDRList="10.42.0.0/16" \
      --set kubeProxyReplacement=true \
      --set loadBalancer.standalone=true \
      --set loadBalancer.algorithm=maglev \
      --set bpf.masquerade=true
    
    # Wait for Cilium to be ready
    log "Waiting for Cilium to be ready..."
    cilium status --wait
    
    log "Cilium installed successfully"
    
    # Configure the Cilium LoadBalancer IP pool
    log "Configuring Cilium LoadBalancer IP pool..."
    
    # Get network info directly using ip command to ensure proper format
    PRIMARY_INTERFACE=$(ip route | grep default | awk '{print $5}')
    if [ -z "$PRIMARY_INTERFACE" ]; then
        PRIMARY_INTERFACE=$(ip -o -4 addr show | grep -v "127.0.0.1" | head -n 1 | awk '{print $2}')
    fi
    
    # Get IP address and network
    NETWORK_INFO=$(ip -o -4 addr show "$PRIMARY_INTERFACE" | awk '{print $4}' | head -n 1)
    NETWORK_PREFIX=$(echo "$NETWORK_INFO" | cut -d'/' -f1 | awk -F. '{print $1"."$2"."$3}')
    
    # Create a range of IPs for the LB
    START_IP="${NETWORK_PREFIX}.240"
    END_IP="${NETWORK_PREFIX}.250"
    
    # Store the range for later use
    export LB_RANGE="${START_IP}-${END_IP}"
    
    # Create Cilium LoadBalancer IP Pool using the correct structure for Cilium 1.17.2
    cat <<EOF | kubectl apply -f -
apiVersion: "cilium.io/v2alpha1"
kind: CiliumLoadBalancerIPPool
metadata:
  name: "default-pool"
spec:
  blocks:
  - start: "${START_IP}"
    end: "${END_IP}"
EOF
    
    log "Cilium LoadBalancer IP pool configured with range: $LB_RANGE"
    
    # Run Cilium connectivity test
    log "Running Cilium connectivity test..."
    cilium connectivity test || log_warning "Connectivity test failed, but continuing installation"
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
    
    # Add annotation to the LoadBalancer service to use our Cilium IP Pool
    kubectl annotate service -n ingress-nginx ingress-nginx-controller "io.cilium/lb-ipam-ips=$(echo $LB_RANGE | cut -d'-' -f1)" --overwrite
    
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
  annotations:
    io.cilium/lb-ipam-ips: "$(echo $LB_RANGE | cut -d'-' -f1)"
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
    install_k3s
    
    # Set KUBECONFIG environment variable
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
    
    install_cilium
    create_unbind_namespace
    install_nginx_ingress
    verify_installation
    
    # Print summary
    log "K3S installation completed successfully"
    log "Cilium LoadBalancer IP pool: ${LB_RANGE}"
    log "Namespaces created: unbind-system, ingress-nginx (Cilium installed in kube-system)"
    log "KUBECONFIG is available at: /etc/rancher/k3s/k3s.yaml"
    log "To use kubectl with this cluster, you can either:"
    log "  1. Run commands as root (sudo kubectl get pods)"
    log "  2. Copy KUBECONFIG to your user: mkdir -p ~/.kube && sudo cp /etc/rancher/k3s/k3s.yaml ~/.kube/config && sudo chown $(id -u):$(id -g) ~/.kube/config"
    log "To verify LoadBalancer functionality, create a service of type LoadBalancer"
}

# Run main function
main