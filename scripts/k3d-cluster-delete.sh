#!/usr/bin/env bash
# Delete the local k3d cluster for a full fresh restart.
# After this, run ./scripts/k3s-local-setup.sh then ./start.sh
# Usage: ./scripts/k3d-cluster-delete.sh

set -e

CLUSTER_NAME="${K3D_CLUSTER_NAME:-dbaas-local}"

echo "Deleting k3d cluster: ${CLUSTER_NAME}"
k3d cluster delete "${CLUSTER_NAME}" 2>/dev/null || true
echo "Done. For a fresh start run: ./scripts/k3s-local-setup.sh && ./start.sh"
