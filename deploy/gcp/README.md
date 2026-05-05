# Deploy gitops-agent on Google Cloud (Cloud Run + Vertex AI)

This repository’s chat UI (`cmd/adk-chat`, image `Dockerfile.adk-chat`) is designed to run as a **container**. On GCP the usual pattern is **Artifact Registry** + **Cloud Run**. **Vertex AI Agent Engine** is Google’s managed agent runtime; it is **Python / `adk deploy`–centric**. This Go ADK app is deployed as a **custom container on Cloud Run** and calls **Gemini** (API key or Vertex AI, depending on how you configure auth).

## Architecture

| Component | Role |
|-----------|------|
| **Cloud Run** | Runs `adk-chat` (web + API). Scales to zero; use min instances for steady latency. |
| **Artifact Registry** | Stores the `adk-chat` image built by Cloud Build (or local `docker build`). |
| **Secret Manager** | Recommended for `GOOGLE_API_KEY`, GitHub token, Argo token, etc. |
| **Vertex AI** | Optional: use Gemini via Vertex instead of an API key (requires code/IAM changes or keeping API key in secrets). |
| **Agent Engine** | Managed runtime + playground for agents deployed with `adk deploy agent_engine` (typically Python). For **Go**, treat **Cloud Run as the runtime** and use the [Agent Engine overview](https://cloud.google.com/vertex-ai/generative-ai/docs/agent-engine/overview) for hybrid patterns (e.g. sessions/memory) if you adopt them later. |

## Prerequisites

1. **GCP project** with billing and APIs enabled:
   - Cloud Run, Artifact Registry, Cloud Build, Secret Manager (optional), IAM.
2. **Image build**: `Dockerfile.adk-chat` expects **`vendor/`** present (`GOOGLEFLAGS=-mod=vendor`). From repo root:
   ```bash
   go mod vendor
   ```
3. **MCP endpoints**: `adk-chat` requires reachable HTTP **Streamable MCP** URLs for git, Argo, and k8s (`GIT_MCP_URL`, `ARGO_MCP_URL`, `K8S_MCP_URL`). The compose-local **git-mcp** uses **Docker socket**, which **does not apply on Cloud Run**. You must either:
   - run MCP servers elsewhere (GKE, VMs, or Cloud Run–compatible images) and expose **HTTPS** URLs, or  
   - use **Serverless VPC Access** so Cloud Run reaches private MCP URLs on your VPC.

## One-time: Artifact Registry repository

```bash
export PROJECT_ID=your-project
export REGION=us-central1
export REPO=gitops-agent

gcloud config set project "$PROJECT_ID"
gcloud artifacts repositories create "$REPO" \
  --repository-format=docker \
  --location="$REGION" \
  --description="gitops-agent images"
```

## Build and push (Cloud Build)

From the repository root:

```bash
export PROJECT_ID=your-project
export REGION=us-central1
export REPO=gitops-agent
export IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/adk-chat:latest"

gcloud builds submit --config=deploy/gcp/cloudbuild.yaml \
  --substitutions=_IMAGE="${IMAGE}" .
```

## Deploy to Cloud Run

The container uses **`/app/cloud-run-entrypoint.sh`** on Cloud Run so the ADK **web** + **webui** flags receive a consistent **`ADK_PUBLIC_ORIGIN`** (set automatically from the service URL after deploy). Local **`docker compose`** is unchanged (still invokes `adk-chat` directly).

Use the helper script (port **8080**, Secret Manager for the Gemini API key):

```bash
export PROJECT_ID=your-project
export REGION=us-central1
export IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/adk-chat:latest"

# Create secrets first (example names — adjust to match script defaults or flags):
# echo -n "$GOOGLE_API_KEY" | gcloud secrets create gitops-google-api-key --data-file=-
# echo -n "$GITHUB_TOKEN" | gcloud secrets create gitops-github-token --data-file=-

./scripts/deploy-gcp-cloud-run.sh
```

Review `./scripts/deploy-gcp-cloud-run.sh` for required flags and secret names. At minimum you must supply **`GOOGLE_API_KEY`** (or change the app to use Vertex-only auth) and **MCP URLs** that Cloud Run can reach.

## Terraform option

Use `deploy/gcp/terraform` for IaC-managed Cloud Run + IAM + Secret access.

```bash
cd deploy/gcp/terraform
cp terraform.tfvars.example terraform.tfvars
# edit values (image + MCP URLs + project/region)

terraform init
terraform apply
```

Then set `adk_public_origin` to the first apply output and apply once more:

```bash
terraform output -raw cloud_run_service_url
# set adk_public_origin in terraform.tfvars
terraform apply
```

### IAM

The Cloud Run service account (default compute SA or a dedicated SA) needs:

- **`secretmanager.secretAccessor`** on each secret you mount.
- **`roles/aiplatform.user`** if you later switch the app to **Vertex AI Gemini** with Application Default Credentials.

### Networking

- **Public MCP URLs**: simplest for a trial; lock down with auth on MCP side if exposed.
- **Private MCP on VPC**: create a **VPC connector** and deploy Cloud Run with `--vpc-connector` and appropriate egress.

## Vertex AI Agent Engine (managed) vs this repo

- **Agent Engine** (`adk deploy agent_engine`, Python): Google hosts the agent process and UI integration patterns differ.
- **This Go service**: you **own the container** on **Cloud Run**. That is still the recommended hosting path for **ADK-Go** until your team standardizes on a specific Agent Engine + Go bridge.

## Related

- [Host AI agents on Cloud Run](https://cloud.google.com/run/docs/ai-agents)
- [Vertex AI Agent Engine](https://cloud.google.com/vertex-ai/generative-ai/docs/agent-engine/overview)
- Existing **GKE + Terraform** path for MCP + agents: `terraform/` and `kubernetes/`.
