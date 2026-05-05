#!/bin/sh
# ADK web UI: api_server_address and webui_address must match how browsers reach this service.
# Set ADK_PUBLIC_ORIGIN after deploy (e.g. https://YOUR-SERVICE-xxxxx.run.app). Until then
# we fall back to loopback so the process can start for the first revision.
set -e
PORT="${PORT:-8080}"
ORIGIN="${ADK_PUBLIC_ORIGIN:-http://127.0.0.1:${PORT}}"
# Strip trailing slash; webui_address is host[:port] without scheme (matches local compose style).
HOSTPORT=$(echo "$ORIGIN" | sed -e 's|^https\?://||' -e 's|/$||')
exec /app/adk-chat web --port "$PORT" webui \
  -api_server_address="${ORIGIN}/api" \
  api \
  -webui_address="${HOSTPORT}"
