#!/bin/sh
set -e

############################################
# 1. Detect in-cluster execution (BEST PATH)
############################################
if [ -f /var/run/secrets/kubernetes.io/serviceaccount/token ]; then
  echo "Detected in-cluster execution. Using in-cluster Kubernetes auth."
  exec npx -y kubernetes-mcp-server@latest \
    --port 8080 \
    --cluster-provider incluster
fi

############################################
# 2. Fallback to kubeconfig (external)
############################################
SRC="${KUBECONFIG_INPUT:-/kube/config.in}"

if [ ! -r "$SRC" ]; then
  echo "ERROR: kubeconfig not found at $SRC and not running in-cluster." >&2
  exit 1
fi

mkdir -p /tmp/kube
cp "$SRC" /tmp/kube/config

############################################
# 3. Rewrite localhost → host.docker.internal
############################################
if [ "${K8S_MCP_REWRITE_LOCALHOST:-1}" != "0" ]; then
  sed -i.bak \
    -e 's|server: https://127.0.0.1:|server: https://host.docker.internal:|g' \
    -e 's|server: https://localhost:|server: https://host.docker.internal:|g' \
    -e 's|server: https://0.0.0.0:|server: https://host.docker.internal:|g' \
    /tmp/kube/config 2>/dev/null || true
  rm -f /tmp/kube/config.bak
fi

############################################
# 4. Detect unsupported GKE exec auth
############################################

if grep -q "gke-gcloud-auth-plugin" "$KUBECONFIG"; then
  echo "kubeconfig requires gke-gcloud-auth-plugin; verifying availability..."

  if ! command -v gke-gcloud-auth-plugin >/dev/null 2>&1; then
    echo "ERROR: gke-gcloud-auth-plugin required but not found"
    echo "KUBECONFIG=$KUBECONFIG"
    echo "PATH=$PATH"
    exit 1
  fi

  echo "✅ gke-gcloud-auth-plugin found at $(command -v gke-gcloud-auth-plugin)"
fi


export KUBECONFIG=/tmp/kube/config

############################################
# 5. Connectivity check (non-fatal)
############################################
echo "Testing connection to Kubernetes API..."
if ! npx -y kubernetes-mcp-server@latest \
      --kubeconfig "$KUBECONFIG" \
      --list-tools \
      >/tmp/kube/probe.log 2>&1; then
  echo "Warning: Initial connectivity check failed:"
  cat /tmp/kube/probe.log
fi


echo 

############################################
# 6. Start MCP normally
############################################
exec npx -y kubernetes-mcp-server@latest \
  --port 8080 \
  --kubeconfig "$KUBECONFIG" \
  --cluster-provider kubeconfig
``