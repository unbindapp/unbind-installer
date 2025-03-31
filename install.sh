#!/bin/bash
set -e
# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
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

log_prompt() {
  echo -e "${BLUE}[INPUT]${NC} $1"
}

# Function to check if script is run as root
check_root() {
  if [ "$(id -u)" -ne 0 ]; then
    log_error "This script must be run as root or with sudo"
  fi
}

# Function to install prerequisites
install_prerequisites() {
  log "Installing prerequisites..."
  apt-get update
  apt-get install -y curl wget ca-certificates apt-transport-https
}

# Function to detect internal and external IP addresses
detect_ip_addresses() {
  log "Detecting IP addresses..."
  
  # Get the internal IP address
  INTERNAL_IP=$(ip route get 1 | awk '{print $(NF-2); exit}')
  if [ -z "$INTERNAL_IP" ]; then
    log_error "Could not detect internal IP address"
  fi
  log "Detected internal IP: $INTERNAL_IP"
  export INTERNAL_IP
  
  # Try to detect external IP address using multiple services for redundancy
  log "Detecting external IP address..."
  EXTERNAL_IP=""
  
  # Try multiple external IP detection services
  for service in "https://ifconfig.me" "https://api.ipify.org" "https://ipinfo.io/ip"; do
    if EXTERNAL_IP=$(curl -s "$service"); then
      if [[ $EXTERNAL_IP =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        log "Detected external IP: $EXTERNAL_IP"
        export EXTERNAL_IP
        break
      fi
    fi
  done
  
  if [ -z "$EXTERNAL_IP" ]; then
    log_warning "Could not auto-detect external IP address"
    log_prompt "Please enter your external/public IP address: "
    read -r EXTERNAL_IP
    if [[ ! $EXTERNAL_IP =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      log_error "Invalid IP address format"
    fi
    export EXTERNAL_IP
  fi
}

# Function to prompt for and validate wildcard domain, continuously check DNS
get_and_validate_domain() {
  # Prompt for wildcard domain
  log_prompt "Please enter your wildcard domain (e.g., *.example.com): "
  read -r WILDCARD_DOMAIN
  
  # Validate domain format (basic validation)
  if [[ ! $WILDCARD_DOMAIN =~ ^\*\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*\.[a-zA-Z]{2,}$ ]]; then
    log_error "Invalid wildcard domain format. It should be like: *.example.com or *.dev.example.com"
  fi

  # Extract the base domain (without the wildcard)
  BASE_DOMAIN=${WILDCARD_DOMAIN#\*.}
  export BASE_DOMAIN
  export WILDCARD_DOMAIN
  
  # Generate a random subdomain for testing
  TEST_SUBDOMAIN="verify-$(date +%s)"
  TEST_DOMAIN="${TEST_SUBDOMAIN}.${BASE_DOMAIN}"
  
  # Explain what DNS records should be configured
  log "To proceed, you need to configure your DNS with:"
  log "1. An A record: *.${BASE_DOMAIN} pointing to ${EXTERNAL_IP}"
  log "2. Or a CNAME record: *.${BASE_DOMAIN} pointing to ${BASE_DOMAIN}"
  log "   with an A record for ${BASE_DOMAIN} pointing to ${EXTERNAL_IP}"
  log "The script will now continuously check for proper DNS configuration."
  log_warning "You can press Ctrl+C at any time to exit this process."
  
  # Set up trap to catch Ctrl+C and clean up
  trap 'echo -e "\n${YELLOW}[ABORTED]${NC} DNS check interrupted. Exiting."; exit 1' INT
  
  # Continuously check for DNS resolution
  local dns_configured=false
  local check_count=0
  local check_interval=15 # seconds
  local spinner=('⠋' '⠙' '⠹' '⠸' '⠼' '⠴' '⠦' '⠧' '⠇' '⠏')
  local spin_idx=0
  
  log "Starting DNS verification for ${TEST_DOMAIN}..."
  log "Will check every ${check_interval} seconds until configured correctly."
  
  while ! $dns_configured; do
    # Clear line for spinner update
    echo -ne "\r${spinner[$spin_idx]} Checking DNS configuration... (attempt $check_count)"
    spin_idx=$(( (spin_idx + 1) % 10 ))
    
    RESOLVED_IP=""
    
    # Try with dig first
    if command -v dig &> /dev/null; then
      RESOLVED_IP=$(dig +short "${TEST_DOMAIN}" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1)
    fi
    
    # If dig failed, try with host
    if [ -z "$RESOLVED_IP" ] && command -v host &> /dev/null; then
      RESOLVED_IP=$(host "${TEST_DOMAIN}" | grep "has address" | head -n 1 | awk '{print $NF}')
    fi
    
    # If both failed, try with nslookup
    if [ -z "$RESOLVED_IP" ] && command -v nslookup &> /dev/null; then
      RESOLVED_IP=$(nslookup "${TEST_DOMAIN}" | grep "Address:" | tail -n 1 | awk '{print $2}')
    fi
    
    # If we got a CNAME, follow it and get the IP
    if [ -z "$RESOLVED_IP" ] && command -v dig &> /dev/null; then
      CNAME=$(dig +short "${TEST_DOMAIN}" CNAME | head -n 1)
      if [ -n "$CNAME" ]; then
        RESOLVED_IP=$(dig +short "${CNAME}" A | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1)
      fi
    fi
    
    # Check if DNS is configured correctly
    if [ -n "$RESOLVED_IP" ] && [ "$RESOLVED_IP" = "$EXTERNAL_IP" ]; then
      echo -e "\r\033[K" # Clear the spinner line
      log "DNS validation successful! ${TEST_DOMAIN} resolves to ${EXTERNAL_IP}"
      dns_configured=true
    else
      if [ -n "$RESOLVED_IP" ]; then
        echo -e "\r\033[K" # Clear the spinner line
        log_warning "DNS resolution for ${TEST_DOMAIN} returned ${RESOLVED_IP}, but expected ${EXTERNAL_IP}"
      fi
      
      # Only show this message every 10 checks to reduce verbosity
      if (( check_count % 10 == 0 )); then
        if [ -z "$RESOLVED_IP" ]; then
          echo -e "\r\033[K" # Clear the spinner line
          log_warning "DNS not yet configured. Waiting for ${TEST_DOMAIN} to resolve to ${EXTERNAL_IP}..."
        fi
      fi
      
      check_count=$((check_count + 1))
      sleep $check_interval
    fi
  done
  
  # Remove the trap once DNS is configured
  trap - INT
  
  log "DNS configuration validated successfully! Continuing with installation..."
}

# Function to install K3S
install_k3s() {
  log "Installing K3S..."
  # Define K3S installation flags
  K3S_INSTALL_FLAGS="--flannel-backend=none --disable-kube-proxy --disable=servicelb --disable-network-policy --disable-traefik"
  # Download and run K3S install script
  curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="$K3S_INSTALL_FLAGS" sh -
  log "K3S installed successfully"
  # Set KUBECONFIG environment variable
  export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
  # Wait for K3S to be ready
  log "Waiting for K3S to be ready..."
  sleep 10
}

# Function to install and configure Cilium
install_cilium() {
  log "Installing Cilium CLI..."
  # Install Cilium CLI
  CILIUM_CLI_VERSION=$(curl -s https://raw.githubusercontent.com/cilium/cilium-cli/main/stable.txt)
  CLI_ARCH=amd64
  if [ "$(uname -m)" = "aarch64" ]; then CLI_ARCH=arm64; fi
  curl -L --fail --remote-name-all https://github.com/cilium/cilium-cli/releases/download/${CILIUM_CLI_VERSION}/cilium-linux-${CLI_ARCH}.tar.gz{,.sha256sum}
  sha256sum --check cilium-linux-${CLI_ARCH}.tar.gz.sha256sum
  tar xzvfC cilium-linux-${CLI_ARCH}.tar.gz /usr/local/bin
  rm cilium-linux-${CLI_ARCH}.tar.gz{,.sha256sum}
  log "Cilium CLI installed successfully"
  
  log "Installing Cilium..."
  # Install Cilium with the specified configuration
  cilium install \
    --set k8sServiceHost="$INTERNAL_IP" \
    --set k8sServicePort=6443 \
    --set kubeProxyReplacement=true \
    --set ipam.operator.clusterPoolIPv4PodCIDRList="10.42.0.0/16" \
    --namespace kube-system
  
  # Wait for Cilium to be ready
  log "Waiting for Cilium to be ready..."
  cilium status --wait
  log "Cilium installed successfully"
  
  # Configure the Cilium LoadBalancer IP pool
  log "Configuring Cilium LoadBalancer IP pool..."
  # Get network info
  PRIMARY_INTERFACE=$(ip route | grep default | awk '{print $5}')
  if [ -z "$PRIMARY_INTERFACE" ]; then
    PRIMARY_INTERFACE=$(ip -o -4 addr show | grep -v "127.0.0.1" | head -n 1 | awk '{print $2}')
  fi
  # Get network info for the primary interface
  NETWORK_CIDR=$(ip -o -4 addr show "$PRIMARY_INTERFACE" | awk '{print $4}' | head -n 1)
  
  # Create Cilium LoadBalancer IP Pool
  cat <<EOF | kubectl apply -f -
apiVersion: "cilium.io/v2alpha1"
kind: CiliumLoadBalancerIPPool
metadata:
  name: "external"
spec:
  blocks:
  - cidr: "${NETWORK_CIDR}"
EOF
}

# Function to install NGINX Ingress Controller
install_nginx_ingress() {
  log "Installing NGINX Ingress Controller v1.12.1 with Helm..."
  
  # Create a values file for the Helm chart
  cat <<EOF > /tmp/ingress-nginx-values.yaml
controller:
  kind: DaemonSet
  image:
    registry: registry.k8s.io
    image: ingress-nginx/controller
    tag: "v1.12.1"
    digest: sha256:d2fbc4ec70d8aa2050dd91a91506e998765e86c96f32cffb56c503c9c34eed5b
  ingressClassResource:
    name: nginx
    default: true
  admissionWebhooks:
    enabled: true
  service:
    enabled: true
    type: LoadBalancer
    externalTrafficPolicy: Local
  resources:
    requests:
      cpu: 100m
      memory: 90Mi
  config:
    use-forwarded-headers: "true"
    compute-full-forwarded-for: "true"
    use-proxy-protocol: "false"
rbac:
  create: true
  scope: false
serviceAccount:
  create: true
EOF
  
  # Add Helm repository if it doesn't exist
  helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx || true
  helm repo update
  
  # Create default SSL certificate for HTTPS
  log "Creating default SSL certificate for HTTPS..."
  
  # Create self-signed certificate
  openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout /tmp/tls.key -out /tmp/tls.crt \
    -subj "/CN=*.${BASE_DOMAIN}/O=K3S Ingress" \
    -addext "subjectAltName = DNS:*.${BASE_DOMAIN}, DNS:${BASE_DOMAIN}"
  
  # Create namespace if it doesn't exist
  kubectl create namespace ingress-nginx --dry-run=client -o yaml | kubectl apply -f -
  
  # Install the chart with our values
  helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
    --version 4.12.1 \
    --namespace ingress-nginx \
    --create-namespace \
    --values /tmp/ingress-nginx-values.yaml
  
  # Wait for deployment to be available
  log "Waiting for NGINX Ingress controller to be ready..."
  kubectl wait --namespace ingress-nginx \
    --for=condition=ready pod \
    --selector=app.kubernetes.io/component=controller \
    --timeout=180s || log_warning "Timeout waiting for NGINX Ingress pods to be ready"
  
  log "NGINX Ingress Controller installed successfully"
  
  # Display Ingress Service IP
  INGRESS_IP=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null)
  if [ -n "$INGRESS_IP" ]; then
    log "NGINX Ingress Controller is available at: ${INGRESS_IP}"
  else
    log_warning "NGINX Ingress Controller LoadBalancer IP is not yet assigned. Check with: kubectl get svc -n ingress-nginx"
  fi
  
  # Create a test ingress to verify configuration
  log "Creating a test ingress resource to verify configuration..."
  
  # Create a test namespace
  kubectl create namespace test-ingress --dry-run=client -o yaml | kubectl apply -f -
  
  # Create a test deployment and service
  cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: test-ingress
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: test-app
        image: nginxdemos/hello:plain-text
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: test-app
  namespace: test-ingress
spec:
  ports:
  - port: 80
    targetPort: 80
  selector:
    app: test-app
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test-ingress
  namespace: test-ingress
  annotations:
    kubernetes.io/ingress.class: "nginx"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  tls:
  - hosts:
    - "test.${BASE_DOMAIN}"
    secretName: default-ssl-certificate
  rules:
  - host: "test.${BASE_DOMAIN}"
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: test-app
            port:
              number: 80
EOF
  
  # Wait for the test ingress to be ready
  kubectl wait --namespace test-ingress \
    --for=condition=available deployment/test-app \
    --timeout=60s || log_warning "Timeout waiting for test app deployment"
  
  log "Test ingress created: https://test.${BASE_DOMAIN}"
}

# Main function
main() {
  log "Starting enhanced K3S with Cilium and NGINX Ingress installation..."
  check_root
  install_prerequisites
  detect_ip_addresses
  get_and_validate_domain
  install_k3s
  install_cilium
  install_nginx_ingress
  
  log "K3S with Cilium and NGINX Ingress installation completed successfully!"
}

# Run main function
main