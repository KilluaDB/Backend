#!/usr/bin/env bash
# Run Kubernetes locally using k3d (k3s) and install CloudNativePG and MongoDB Community operators.
# Usage: ./scripts/k3s-local-setup.sh
# Requires: kubectl, Helm, Docker, k3d.
#
# Fresh restart: ./scripts/k3d-cluster-delete.sh then run this script, then ./start.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info()  { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()    { echo -e "${GREEN}[OK]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
err()   { echo -e "${RED}[ERROR]${NC} $1"; }

command_exists() { command -v "$1" >/dev/null 2>&1; }

CLUSTER_NAME="dbaas-local"
K3D_CONTEXT="k3d-${CLUSTER_NAME}"

# Ensure we're in repo root
cd "$(dirname "$0")/.."

info "Checking prerequisites..."
if ! command_exists kubectl; then
  err "kubectl is required. Install from https://kubernetes.io/docs/tasks/tools/"
  exit 1
fi
if ! command_exists helm; then
  err "Helm is required. Install from https://helm.sh/docs/intro/install/"
  exit 1
fi
if ! command_exists docker; then
  err "Docker is required for k3d. Install from https://docs.docker.com/get-docker/"
  exit 1
fi
if ! command_exists k3d; then
  warn "k3d is required for the local k3s cluster but was not found. Attempting automatic install..."
  if command_exists curl; then
    curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash || {
      err "Automatic k3d install failed. Please install it manually from https://k3d.io/"
      exit 1
    }
  else
    err "curl is not available to download k3d. Please install k3d manually from https://k3d.io/"
    exit 1
  fi

  if ! command_exists k3d; then
    err "k3d installation seems to have failed. Please install it manually from https://k3d.io/"
    exit 1
  fi
fi
ok "kubectl, Helm, Docker and k3d found"

info "Ensuring k3d cluster '${CLUSTER_NAME}' is running..."
if ! kubectl config get-contexts "${K3D_CONTEXT}" >/dev/null 2>&1; then
  info "k3d context ${K3D_CONTEXT} not found. Creating cluster '${CLUSTER_NAME}'..."
  k3d cluster create "${CLUSTER_NAME}" --wait --timeout 5m
else
  info "k3d context ${K3D_CONTEXT} already exists"
fi

# Make sure the cluster is started and context is selected
kubectl config use-context "${K3D_CONTEXT}" >/dev/null 2>&1 || {
  warn "Failed to switch kubectl context to ${K3D_CONTEXT}. Attempting to start k3d cluster '${CLUSTER_NAME}'..."
  k3d cluster start "${CLUSTER_NAME}"
  kubectl config use-context "${K3D_CONTEXT}" >/dev/null 2>&1 || {
    err "Unable to use kubectl context ${K3D_CONTEXT}. Check your k3d installation."
    exit 1
  }
}

if ! kubectl cluster-info &>/dev/null; then
  err "k3d cluster '${CLUSTER_NAME}' is not reachable even after creation/start. Please check k3d status."
  exit 1
fi
ok "k3d cluster '${CLUSTER_NAME}' is reachable"

# Namespaces: DB instances in dedicated namespaces; operators in their own namespaces
info "Creating namespaces..."
kubectl create namespace postgres-operator 2>/dev/null || true
kubectl create namespace postgres-instances 2>/dev/null || true
kubectl create namespace mongodb-instances 2>/dev/null || true
ok "Namespaces postgres-operator, postgres-instances, mongodb-instances ready"

# CloudNativePG: operator in postgres-operator, watches all namespaces (instances go in postgres-instances)
info "Adding CloudNativePG Helm repo..."
helm repo add cnpg https://cloudnative-pg.github.io/charts 2>/dev/null || true
helm repo update cnpg

info "Installing CloudNativePG operator in postgres-operator (cluster-wide watch; instances in postgres-instances)..."
helm upgrade --install cnpg cnpg/cloudnative-pg \
  --namespace postgres-operator \
  --create-namespace \
  --set config.clusterWide=true \
  --set rbac.create=true \
  --wait --timeout 3m

ok "CloudNativePG installed in postgres-operator"

# cert-manager (required by MongoDB Community Operator for TLS)
if ! kubectl get namespace cert-manager &>/dev/null 2>&1; then
  info "Installing cert-manager (required by MongoDB operator)..."
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
  info "Waiting for cert-manager to be ready..."
  sleep 10
  kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=120s 2>/dev/null || true
  ok "cert-manager installed"
else
  ok "cert-manager already present"
fi

# MongoDB Community Operator (watches only mongodb-instances namespace)
info "Adding MongoDB Helm repo..."
helm repo add mongodb https://mongodb.github.io/helm-charts 2>/dev/null || true
helm repo update mongodb

info "Installing MongoDB Community Operator in mongodb-operator (watches mongodb-instances only)..."
if ! helm upgrade --install community-operator mongodb/community-operator \
  --namespace mongodb-operator \
  --create-namespace \
  --set operator.watchNamespace="mongodb-instances" \
  --timeout 5m; then
  warn "Helm install/upgrade failed. Run with --debug to see the error."
else
  if ! kubectl wait --for=condition=Available deployment/mongodb-kubernetes-operator -n mongodb-operator --timeout=180s 2>/dev/null; then
    warn "MongoDB operator deployment not ready."
    kubectl get pods -n mongodb-operator -l name=mongodb-kubernetes-operator -o wide 2>/dev/null || true
    kubectl describe deployment mongodb-kubernetes-operator -n mongodb-operator 2>/dev/null | tail -30
  else
    ok "MongoDB Community Operator installed in mongodb-operator"
  fi
fi

# Backend RBAC; MongoDB operator RBAC (only in mongodb-instances)
if [ -f deploy/rbac.yaml ]; then
  info "Applying backend RBAC (postgres-instances, mongodb-instances)..."
  kubectl apply -f deploy/rbac.yaml || warn "Backend RBAC apply failed"
fi
if [ -f deploy/mongodb-operator-mongodb-instances-rbac.yaml ]; then
  info "Applying MongoDB operator RBAC (mongodb-instances only)..."
  kubectl apply -f deploy/mongodb-operator-mongodb-instances-rbac.yaml || warn "MongoDB operator RBAC apply failed"
fi

echo ""
ok "Local k3s (k3d) setup done."
echo ""
echo "  k3d cluster:"
echo "    Name: ${CLUSTER_NAME}"
echo "    Context: ${K3D_CONTEXT}"
echo ""
echo "  Next steps to run the backend inside k3s:"
echo "    ./start.sh"
echo ""
echo "  Check operators:"
echo "    kubectl get pods -n postgres-operator    # CloudNativePG"
echo "    kubectl get pods -n mongodb-operator    # MongoDB Community Operator"
echo ""

