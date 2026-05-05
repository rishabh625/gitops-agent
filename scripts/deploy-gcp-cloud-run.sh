#!/usr/bin/env bash
# Deploy gitops-agent adk-chat to Cloud Run.
# Prerequisites: image in Artifact Registry, Secret Manager secrets (optional but recommended), MCP URLs reachable from Cloud Run.
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-}"
REGION="${REGION:-us-central1}"
SERVICE_NAME="${SERVICE_NAME:-gitops-adk-chat}"
IMAGE="${IMAGE:-}"

# MCP endpoints (must be reachable from Cloud Run — use HTTPS or VPC connector for private endpoints)
GIT_MCP_URL="${GIT_MCP_URL:-}"
ARGO_MCP_URL="${ARGO_MCP_URL:-}"
K8S_MCP_URL="${K8S_MCP_URL:-}"

# Secret names in Secret Manager (latest version mounted as env var)
SECRET_GOOGLE_API_KEY="${SECRET_GOOGLE_API_KEY:-gitops-google-api-key}"

ADK_MODEL="${ADK_MODEL:-gemini-2.5-flash}"
MIN_INSTANCES="${MIN_INSTANCES:-0}"
MAX_INSTANCES="${MAX_INSTANCES:-10}"

usage() {
  cat <<'EOF'
Usage:
  PROJECT_ID=my-proj IMAGE=us-central1-docker.pkg.dev/my-proj/repo/adk-chat:latest \
  GIT_MCP_URL=https://git-mcp.example.com/mcp \
  ARGO_MCP_URL=https://argo-mcp.example.com/mcp \
  K8S_MCP_URL=https://k8s-mcp.example.com/mcp \
  ./scripts/deploy-gcp-cloud-run.sh

Required env:
  PROJECT_ID   GCP project ID
  IMAGE        Full Artifact Registry image reference for adk-chat
  GIT_MCP_URL  Git MCP Streamable HTTP endpoint (/mcp)
  ARGO_MCP_URL Argo MCP endpoint
  K8S_MCP_URL  Kubernetes MCP endpoint

Optional:
  REGION, SERVICE_NAME, SECRET_GOOGLE_API_KEY, ADK_MODEL, MIN_INSTANCES, MAX_INSTANCES
EOF
}

if [[ -z "${PROJECT_ID}" || -z "${IMAGE}" || -z "${GIT_MCP_URL}" || -z "${ARGO_MCP_URL}" || -z "${K8S_MCP_URL}" ]]; then
  usage
  exit 1
fi

gcloud config set project "${PROJECT_ID}"

# GOOGLE_API_KEY from Secret Manager (recommended). Change flag to --set-env-vars if you inject differently.
SECRETS_ARG=(--set-secrets="GOOGLE_API_KEY=${SECRET_GOOGLE_API_KEY}:latest")

# Use Cloud Run entrypoint so the ADK web UI gets correct /api and webui wiring (see scripts/cloud-run-entrypoint.sh).
gcloud run deploy "${SERVICE_NAME}" \
  --image="${IMAGE}" \
  --region="${REGION}" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --memory=2Gi \
  --cpu=2 \
  --min-instances="${MIN_INSTANCES}" \
  --max-instances="${MAX_INSTANCES}" \
  --timeout=3600 \
  --command=/app/cloud-run-entrypoint.sh \
  "${SECRETS_ARG[@]}" \
  --set-env-vars="ADK_MODEL=${ADK_MODEL},SKILL_SOURCE=local,SKILLS_ROOT=/app/gitops-skills,GIT_MCP_URL=${GIT_MCP_URL},ARGO_MCP_URL=${ARGO_MCP_URL},K8S_MCP_URL=${K8S_MCP_URL},MCP_TIMEOUT=20s,MCP_MAX_RETRIES=2"

# Browser clients need the public HTTPS origin; set it from the service URL (first deploy used loopback fallback).
URL="$(gcloud run services describe "${SERVICE_NAME}" --region="${REGION}" --format='value(status.url)')"
gcloud run services update "${SERVICE_NAME}" --region="${REGION}" \
  --update-env-vars="ADK_PUBLIC_ORIGIN=${URL}"

echo "Deployed ${SERVICE_NAME}. URL:"
echo "${URL}"
