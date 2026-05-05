#!/usr/bin/env bash
#
# One-shot: configure env, build all images (Cloud Build), create/update secrets,
# deploy git-mcp, argo-mcp, k8s-mcp, and adk-chat to Cloud Run, then set ADK_PUBLIC_ORIGIN.
#
# Usage:
#   export PROJECT_ID=my-project
#   export GOOGLE_API_KEY=...
#   export GITHUB_TOKEN=...
#   export ARGOCD_SERVER=https://argocd.example.com   # no trailing slash
#   export ARGOCD_AUTH_TOKEN=...
#   export KUBECONFIG_PATH="$HOME/.kube/config"
#   ./scripts/deploy-all-gcp-cloud-run.sh
#
# Optional: REGION, REPO_NAME, MCP_GITHUB_PACKAGE, GITHUB_API_URL, skip build (SKIP_BUILD=1).
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

### --- Required configuration -------------------------------------------------
PROJECT_ID="${PROJECT_ID:?Set PROJECT_ID}"
REGION="${REGION:-us-central1}"
REPO_NAME="${REPO_NAME:-gitops-agent}"

# Registry image prefix (Artifact Registry Docker repo must exist or use CREATE_ARTIFACT_REPO=1)
REGISTRY="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO_NAME}"

GOOGLE_API_KEY="${GOOGLE_API_KEY:-}"
GITHUB_TOKEN="${GITHUB_TOKEN:-${GITHUB_PERSONAL_ACCESS_TOKEN:-}}"
ARGOCD_SERVER="${ARGOCD_SERVER:-}"
ARGOCD_AUTH_TOKEN="${ARGOCD_AUTH_TOKEN:-${ARGOCD_TOKEN:-}}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-}"

SECRET_GOOGLE_API_KEY="${SECRET_GOOGLE_API_KEY:-gitops-google-api-key}"
SECRET_GITHUB_TOKEN="${SECRET_GITHUB_TOKEN:-gitops-github-token}"
SECRET_ARGOCD_TOKEN="${SECRET_ARGOCD_TOKEN:-gitops-argocd-token}"
SECRET_KUBECONFIG="${SECRET_KUBECONFIG:-gitops-kubeconfig}"

SERVICE_GIT="${SERVICE_GIT:-gitops-mcp-git}"
SERVICE_ARGO="${SERVICE_ARGO:-gitops-mcp-argo}"
SERVICE_K8S="${SERVICE_K8S:-gitops-mcp-k8s}"
SERVICE_ADK="${SERVICE_ADK:-gitops-adk-chat}"

CREATE_ARTIFACT_REPO="${CREATE_ARTIFACT_REPO:-1}"
SKIP_BUILD="${SKIP_BUILD:-0}"

GITHUB_API_URL="${GITHUB_API_URL:-https://api.github.com}"
MCP_GITHUB_PACKAGE="${MCP_GITHUB_PACKAGE:-@modelcontextprotocol/server-github}"

ADK_MODEL="${ADK_MODEL:-gemini-2.5-flash}"
MCP_TIMEOUT="${MCP_TIMEOUT:-20s}"
MCP_MAX_RETRIES="${MCP_MAX_RETRIES:-2}"

MIN_INSTANCES="${MIN_INSTANCES:-0}"
MAX_INSTANCES="${MAX_INSTANCES:-10}"

# Optional: Gemini Enterprise / Discovery Engine search fallback for GitOps failures.
DISCOVERY_ENGINE_SEARCH_ENABLED="${DISCOVERY_ENGINE_SEARCH_ENABLED:-}"
DISCOVERY_ENGINE_LOCATION="${DISCOVERY_ENGINE_LOCATION:-}"
DISCOVERY_ENGINE_SERVING_CONFIG="${DISCOVERY_ENGINE_SERVING_CONFIG:-}"
DISCOVERY_ENGINE_SESSION="${DISCOVERY_ENGINE_SESSION:-}"
DISCOVERY_ENGINE_TIME_ZONE="${DISCOVERY_ENGINE_TIME_ZONE:-}"
DISCOVERY_ENGINE_GIT_FALLBACK="${DISCOVERY_ENGINE_GIT_FALLBACK:-}"

### --- Validation -------------------------------------------------------------
[[ -n "$GOOGLE_API_KEY" ]] || { echo "Set GOOGLE_API_KEY"; exit 1; }
[[ -n "$GITHUB_TOKEN" ]] || { echo "Set GITHUB_TOKEN or GITHUB_PERSONAL_ACCESS_TOKEN"; exit 1; }
[[ -n "$ARGOCD_SERVER" ]] || { echo "Set ARGOCD_SERVER"; exit 1; }
[[ -n "$ARGOCD_AUTH_TOKEN" ]] || { echo "Set ARGOCD_AUTH_TOKEN or ARGOCD_TOKEN"; exit 1; }
[[ -n "$KUBECONFIG_PATH" && -f "$KUBECONFIG_PATH" ]] || { echo "Set KUBECONFIG_PATH to a readable kubeconfig file"; exit 1; }

### --- Helpers ----------------------------------------------------------------
ensure_secret() {
  local name="$1" value="$2"
  if gcloud secrets describe "$name" --project="$PROJECT_ID" &>/dev/null; then
    echo -n "$value" | gcloud secrets versions add "$name" --data-file=- --project="$PROJECT_ID"
  else
    echo -n "$value" | gcloud secrets create "$name" --data-file=- --project="$PROJECT_ID"
  fi
}

grant_secret_accessor() {
  local member="$1"
  local sec
  for sec in "$SECRET_GOOGLE_API_KEY" "$SECRET_GITHUB_TOKEN" "$SECRET_ARGOCD_TOKEN" "$SECRET_KUBECONFIG"; do
    gcloud secrets add-iam-policy-binding "$sec" \
      --project="$PROJECT_ID" \
      --member="$member" \
      --role="roles/secretmanager.secretAccessor" \
      --quiet &>/dev/null || true
  done
}

### --- gcloud -----------------------------------------------------------------
gcloud config set project "$PROJECT_ID"

APIS=(
  run.googleapis.com
  artifactregistry.googleapis.com
  cloudbuild.googleapis.com
  secretmanager.googleapis.com
  iam.googleapis.com
)
for api in "${APIS[@]}"; do
  gcloud services enable "$api" --project="$PROJECT_ID" --quiet
done

if [[ "$CREATE_ARTIFACT_REPO" == "1" ]]; then
  if ! gcloud artifacts repositories describe "$REPO_NAME" --location="$REGION" --project="$PROJECT_ID" &>/dev/null; then
    gcloud artifacts repositories create "$REPO_NAME" \
      --repository-format=docker \
      --location="$REGION" \
      --project="$PROJECT_ID" \
      --description="gitops-agent images"
  fi
fi

### --- Secrets ----------------------------------------------------------------
ensure_secret "$SECRET_GOOGLE_API_KEY" "$GOOGLE_API_KEY"
ensure_secret "$SECRET_GITHUB_TOKEN" "$GITHUB_TOKEN"
ensure_secret "$SECRET_ARGOCD_TOKEN" "$ARGOCD_AUTH_TOKEN"
ensure_secret "$SECRET_KUBECONFIG" "$(cat "$KUBECONFIG_PATH")"

PROJECT_NUMBER="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"
DEFAULT_RUN_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
CLOUD_RUN_SA="${CLOUD_RUN_SA:-$DEFAULT_RUN_SA}"
grant_secret_accessor "serviceAccount:${CLOUD_RUN_SA}"

### --- Build images -----------------------------------------------------------
if [[ "$SKIP_BUILD" != "1" ]]; then
  go mod vendor
  gcloud builds submit \
    --project="$PROJECT_ID" \
    --config=deploy/gcp/cloudbuild.all.yaml \
    --substitutions=_REGISTRY="$REGISTRY" \
    .
fi

IMG_GIT="${REGISTRY}/git-mcp:latest"
IMG_ARGO="${REGISTRY}/argo-mcp:latest"
IMG_K8S="${REGISTRY}/k8s-mcp:latest"
IMG_ADK="${REGISTRY}/adk-chat:latest"

### --- Deploy MCP services ----------------------------------------------------
gcloud run deploy "$SERVICE_GIT" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$IMG_GIT" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --cpu=1 \
  --memory=512Mi \
  --min-instances=0 \
  --max-instances="${MAX_INSTANCES}" \
  --service-account="$CLOUD_RUN_SA" \
  --set-secrets="GITHUB_PERSONAL_ACCESS_TOKEN=${SECRET_GITHUB_TOKEN}:latest" \
  --set-env-vars="GITHUB_API_URL=${GITHUB_API_URL},MCP_GITHUB_PACKAGE=${MCP_GITHUB_PACKAGE}"

gcloud run deploy "$SERVICE_ARGO" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$IMG_ARGO" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --cpu=1 \
  --memory=512Mi \
  --min-instances=0 \
  --max-instances="${MAX_INSTANCES}" \
  --service-account="$CLOUD_RUN_SA" \
  --set-secrets="ARGOCD_API_TOKEN=${SECRET_ARGOCD_TOKEN}:latest" \
  --set-env-vars="ARGOCD_BASE_URL=${ARGOCD_SERVER}"

# Mount kubeconfig file from Secret Manager (path must match Dockerfile.k8s-mcp-cloudrun KUBECONFIG_INPUT)
gcloud run deploy "$SERVICE_K8S" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$IMG_K8S" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --cpu=1 \
  --memory=1Gi \
  --min-instances=0 \
  --max-instances="${MAX_INSTANCES}" \
  --service-account="$CLOUD_RUN_SA" \
  --set-secrets="/kube/config.in=${SECRET_KUBECONFIG}:latest" \
  --set-env-vars="K8S_MCP_REWRITE_LOCALHOST=0"

URL_GIT="$(gcloud run services describe "$SERVICE_GIT" --region="$REGION" --project="$PROJECT_ID" --format='value(status.url)')"
URL_ARGO="$(gcloud run services describe "$SERVICE_ARGO" --region="$REGION" --project="$PROJECT_ID" --format='value(status.url)')"
URL_K8S="$(gcloud run services describe "$SERVICE_K8S" --region="$REGION" --project="$PROJECT_ID" --format='value(status.url)')"

GIT_MCP_URL="${URL_GIT}/mcp"
ARGO_MCP_URL="${URL_ARGO}/mcp"
K8S_MCP_URL="${URL_K8S}/mcp"

### --- Deploy adk-chat --------------------------------------------------------
gcloud run deploy "$SERVICE_ADK" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$IMG_ADK" \
  --platform=managed \
  --allow-unauthenticated \
  --port=8080 \
  --cpu=2 \
  --memory=2Gi \
  --min-instances="${MIN_INSTANCES}" \
  --max-instances="${MAX_INSTANCES}" \
  --timeout=3600 \
  --service-account="$CLOUD_RUN_SA" \
  --command=/app/cloud-run-entrypoint.sh \
  --set-secrets="GOOGLE_API_KEY=${SECRET_GOOGLE_API_KEY}:latest" \
  --set-env-vars="ADK_MODEL=${ADK_MODEL},SKILL_SOURCE=local,SKILLS_ROOT=/app/gitops-skills,GIT_MCP_URL=${GIT_MCP_URL},ARGO_MCP_URL=${ARGO_MCP_URL},K8S_MCP_URL=${K8S_MCP_URL},MCP_TIMEOUT=${MCP_TIMEOUT},MCP_MAX_RETRIES=${MCP_MAX_RETRIES}${DISCOVERY_ENGINE_SEARCH_ENABLED:+,DISCOVERY_ENGINE_SEARCH_ENABLED=${DISCOVERY_ENGINE_SEARCH_ENABLED}}${DISCOVERY_ENGINE_LOCATION:+,DISCOVERY_ENGINE_LOCATION=${DISCOVERY_ENGINE_LOCATION}}${DISCOVERY_ENGINE_SERVING_CONFIG:+,DISCOVERY_ENGINE_SERVING_CONFIG=${DISCOVERY_ENGINE_SERVING_CONFIG}}${DISCOVERY_ENGINE_SESSION:+,DISCOVERY_ENGINE_SESSION=${DISCOVERY_ENGINE_SESSION}}${DISCOVERY_ENGINE_TIME_ZONE:+,DISCOVERY_ENGINE_TIME_ZONE=${DISCOVERY_ENGINE_TIME_ZONE}}${DISCOVERY_ENGINE_GIT_FALLBACK:+,DISCOVERY_ENGINE_GIT_FALLBACK=${DISCOVERY_ENGINE_GIT_FALLBACK}}"

URL_ADK="$(gcloud run services describe "$SERVICE_ADK" --region="$REGION" --project="$PROJECT_ID" --format='value(status.url)')"

gcloud run services update "$SERVICE_ADK" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --update-env-vars="ADK_PUBLIC_ORIGIN=${URL_ADK}"

echo ""
echo "Deployed:"
echo "  Git MCP:   $URL_GIT   (${GIT_MCP_URL})"
echo "  Argo MCP:  $URL_ARGO  (${ARGO_MCP_URL})"
echo "  K8s MCP:   $URL_K8S   (${K8S_MCP_URL})"
echo "  ADK chat:  $URL_ADK"
echo ""
echo "Open ADK UI: $URL_ADK"
