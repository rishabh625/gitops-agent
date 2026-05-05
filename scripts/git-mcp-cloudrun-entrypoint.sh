#!/bin/sh
# GitHub MCP over Streamable HTTP (no host Docker / no ghcr inner container).
# Uses supergateway to bridge stdio MCP → HTTP. Override package via MCP_GITHUB_PACKAGE.
set -e
TOKEN="${GITHUB_PERSONAL_ACCESS_TOKEN:-${GITHUB_TOKEN:-}}"
if [ -z "$TOKEN" ]; then
  echo "Set GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN" >&2
  exit 1
fi
export GITHUB_PERSONAL_ACCESS_TOKEN="$TOKEN"
API_URL="${GITHUB_API_URL:-https://api.github.com}"
API_URL="${API_URL%:}"
API_URL="${API_URL%/}"
export GITHUB_API_URL="$API_URL"

PKG="${MCP_GITHUB_PACKAGE:-@modelcontextprotocol/server-github}"

exec npx -y supergateway \
  --stdio "npx -y ${PKG}" \
  --outputTransport streamableHttp \
  --port 8080
