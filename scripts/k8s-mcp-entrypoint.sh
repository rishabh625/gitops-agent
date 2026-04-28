#!/bin/sh
set -e
# Mounted read-only from the host; copy so we can rewrite server URLs for in-container DNS.
SRC="${KUBECONFIG_INPUT:-/kube/config.in}"
if [ ! -r "$SRC" ]; then
  echo "kubeconfig not found at $SRC (mount host ~/.kube/config)" >&2
  exit 1
fi
mkdir -p /tmp/kube
cp "$SRC" /tmp/kube/config

# From inside Docker, loopback in kubeconfig points at this container, not the host API.
# Rewrite common local patterns to host.docker.internal (requires extra_hosts: host-gateway).
if [ "${K8S_MCP_REWRITE_LOCALHOST:-1}" != "0" ]; then
  sed -i.bak \
    -e 's|server: https://127.0.0.1:|server: https://host.docker.internal:|g' \
    -e 's|server: https://localhost:|server: https://host.docker.internal:|g' \
    -e 's|server: https://0.0.0.0:|server: https://host.docker.internal:|g' \
    /tmp/kube/config 2>/dev/null || true
  rm -f /tmp/kube/config.bak
fi

export KUBECONFIG=/tmp/kube/config
exec npx -y kubernetes-mcp-server@latest --port 8080 --kubeconfig "$KUBECONFIG" --cluster-provider kubeconfig
