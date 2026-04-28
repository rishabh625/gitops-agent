#!/bin/sh
set -e
# Accept either name from .env (Compose may pass one or both).
TOKEN="${GITHUB_PERSONAL_ACCESS_TOKEN:-}"
if [ -z "$TOKEN" ]; then
  TOKEN="${GITHUB_TOKEN:-}"
fi
if [ -z "$TOKEN" ]; then
  echo "Set GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN for git-mcp" >&2
  exit 1
fi
export GITHUB_PERSONAL_ACCESS_TOKEN="$TOKEN"

# Optional: set DOCKER_DEFAULT_PLATFORM=linux/amd64 on Apple Silicon if the GitHub image fails to start.
DRUN="docker run --rm -i"
if [ -n "${GIT_MCP_DOCKER_PLATFORM:-}" ]; then
  DRUN="$DRUN --platform ${GIT_MCP_DOCKER_PLATFORM}"
fi
# -e NAME (no value) copies PAT from this process env into the inner container.
DRUN="$DRUN -e GITHUB_PERSONAL_ACCESS_TOKEN ghcr.io/github/github-mcp-server"

exec npx -y supergateway \
  --stdio "$DRUN" \
  --outputTransport streamableHttp \
  --port 8080
