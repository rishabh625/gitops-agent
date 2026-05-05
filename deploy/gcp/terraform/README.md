# Terraform: Cloud Run deployment for adk-chat

This stack deploys `cmd/adk-chat` container to **Cloud Run** and wires:

- API enablement (`run`, `artifactregistry`, `secretmanager`, `iam`)
- Optional Artifact Registry repository
- Dedicated Cloud Run service account
- Secret Manager access for `GOOGLE_API_KEY`
- Cloud Run service + optional public invoker IAM

## Prerequisites

1. Build and push image first (see `deploy/gcp/cloudbuild.yaml`).
2. Have a secret named `gitops-google-api-key` (or override variable) with at least one version:
   ```bash
   echo -n "$GOOGLE_API_KEY" | gcloud secrets versions add gitops-google-api-key --data-file=-
   ```
3. Provide reachable MCP URLs (`git_mcp_url`, `argo_mcp_url`, `k8s_mcp_url`).

## Usage

```bash
cd deploy/gcp/terraform
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars

terraform init
terraform plan
terraform apply
```

After first apply, set `adk_public_origin` to output `cloud_run_service_url`, then apply again:

```bash
terraform output -raw cloud_run_service_url
# paste into terraform.tfvars as adk_public_origin
terraform apply
```

This second apply ensures ADK web UI uses the real public Cloud Run origin for `/api` routing.

## Notes

- `create_google_api_key_secret = true` creates the secret resource only; you still need to add a secret version.
- If you set `allow_unauthenticated = false`, grant `roles/run.invoker` to the callers you trust.
- For private MCP backends, attach a Serverless VPC connector and egress settings (not included in this minimal stack).
