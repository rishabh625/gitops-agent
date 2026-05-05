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
echo "GITHUB_PERSONAL_ACCESS_TOKEN: $TOKEN"

API_URL="${GITHUB_API_URL:-}"
if [ -z "$API_URL" ]; then
  API_URL="${GIT_GITHUB_API_URL:-}"
fi
if [ -z "$API_URL" ]; then
  API_URL="https://api.github.com"
fi
API_URL="${API_URL%:}"
API_URL="${API_URL%/}"
export GITHUB_API_URL="$API_URL"
export GIT_GITHUB_API_URL="$API_URL"

node - <<'NODE'
const http = require('http');
const https = require('https');

const token = process.env.GITHUB_PERSONAL_ACCESS_TOKEN || '';
const apiUrl = process.env.GITHUB_API_URL || 'https://api.github.com';

if (!token) {
  console.error('Missing GITHUB_PERSONAL_ACCESS_TOKEN');
  process.exit(1);
}

let url;
try {
  url = new URL(apiUrl);
} catch (e) {
  console.error(`Invalid GITHUB_API_URL: ${apiUrl}`);
  process.exit(1);
}

const client = url.protocol === 'http:' ? http : https;
const req = client.request(
  {
    method: 'GET',
    hostname: url.hostname,
    port: url.port || (url.protocol === 'http:' ? 80 : 443),
    path: `${url.pathname.replace(/\/$/, '')}/user`,
    headers: {
      'Authorization': `Bearer ${token}`,
      'User-Agent': 'gitops-agent-git-mcp-gateway',
      'Accept': 'application/vnd.github+json',
    },
    timeout: 10000,
  },
  (res) => {
    let body = '';
    res.setEncoding('utf8');
    res.on('data', (d) => (body += d));
    res.on('end', () => {
      if (res.statusCode >= 200 && res.statusCode < 300) {
        process.exit(0);
      }
      console.error(`GitHub auth probe failed: GET ${apiUrl.replace(/\/$/, '')}/user -> ${res.statusCode}`);
      if (body) {
        console.error(body);
      }
      process.exit(1);
    });
  },
);
req.on('timeout', () => req.destroy(new Error('timeout')));
req.on('error', (err) => {
  console.error(`GitHub auth probe request failed: ${err.message}`);
  process.exit(1);
});
req.end();
NODE

# Optional: set DOCKER_DEFAULT_PLATFORM=linux/amd64 on Apple Silicon if the GitHub image fails to start.
DRUN="docker run --rm -i"
if [ -n "${GIT_MCP_DOCKER_PLATFORM:-}" ]; then
  DRUN="$DRUN --platform ${GIT_MCP_DOCKER_PLATFORM}"
fi
# -e NAME (no value) copies PAT from this process env into the inner container.
DRUN="$DRUN -e GITHUB_PERSONAL_ACCESS_TOKEN -e GITHUB_API_URL -e GIT_GITHUB_API_URL ghcr.io/github/github-mcp-server"

exec npx -y supergateway \
  --stdio "$DRUN" \
  --outputTransport streamableHttp \
  --port 8080
