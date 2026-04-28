#!/usr/bin/env bash
# Start the full local stack (MCP servers, execution + orchestrator agents, ADK chat).
# See LOCAL_DOCKER_FLOW.md for credentials, probes, and example flows.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

usage() {
  echo "Usage: $(basename "$0") [-f|--foreground] [-h|--help]"
  echo "  -f, --foreground  Run docker compose in the foreground (stream logs; no -d)"
  echo "  -h, --help        Show this help"
}

DETACH=( -d )
while [[ $# -gt 0 ]]; do
  case "$1" in
    -f|--foreground) DETACH=(); shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed or not on PATH." >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose is not available (need Docker Compose v2)." >&2
  exit 1
fi

if [[ ! -f .env ]]; then
  if [[ -f docker-compose.env.example ]]; then
    cp docker-compose.env.example .env
    echo "Created .env from docker-compose.env.example — edit it with real credentials before relying on MCP calls." >&2
  else
    echo "No .env and no docker-compose.env.example found in $ROOT" >&2
    exit 1
  fi
fi

if [[ ! -f "${HOME}/.kube/config" ]]; then
  echo "Warning: ~/.kube/config not found; k8s-mcp may fail until kubeconfig exists." >&2
fi

echo "Starting stack from $ROOT ..."
docker compose up --build "${DETACH[@]}"

if [[ ${#DETACH[@]} -gt 0 ]]; then
  docker compose ps
  ui_url="http://localhost:8083/ui/"
  if mapped="$(docker compose port adk-chat 8080 2>/dev/null)"; then
    port="${mapped##*:}"
    ui_url="http://localhost:${port}/ui/"
  fi
  echo ""
  echo "ADK Web UI: ${ui_url}"
  echo "Tail agents: docker compose logs -f orchestrator-agent execution-agent adk-chat"
fi
