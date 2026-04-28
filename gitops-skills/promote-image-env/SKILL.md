---
name: promote-image-env
description: Promote a validated image version across environments in a GitOps repository by creating pull requests and verifying Argo/Kubernetes state. Use when moving releases between dev, staging, and production for Helm or Kustomize applications.
license: Apache-2.0
compatibility: Requires MCP connectivity to Git, Argo, and Kubernetes servers. No CLI tools required.
metadata:
  owner: platform-engineering
  category: environment-promotion
  version: "1.0.0"
allowed-tools: mcp__git__* mcp__argo__* mcp__k8s__*
---

# Promote Image Across Environments

## Inputs

- `repo`: Git repository identifier.
- `application`: Argo CD application name prefix or set.
- `manifest_mode`: `helm` or `kustomize`.
- `image_name`: image repository being promoted.
- `image_tag`: release tag to promote.
- `source_env`: current validated environment.
- `target_env`: destination environment.
- `promotion_policy`: optional sequencing and approval policy metadata.

## Outputs

- `promotion_plan`: files/environments affected.
- `pull_request`: promotion PR URL or identifier.
- `verification_report`: post-merge Argo/Kubernetes checks for target environment.

## Tool Bindings

- **Git MCP**: inspect env configs, update target env references, create PR.
- **Argo MCP**: verify source env health before promotion and target env health after merge.
- **Kubernetes MCP**: verify running image tag in source and target namespaces.

## Steps

1. Confirm promotion eligibility
   - Verify source environment is healthy and running `image_tag`.
   - Verify promotion policy allows source-to-target transition.
2. Update target environment manifests via Git MCP
   - For Helm: update target values file or env-specific value block.
   - For Kustomize: update overlay image tag in target environment.
3. Create promotion PR
   - Include provenance from source environment validation.
   - Include rollback tag and rollback environment procedure.
4. Verify target after merge
   - Use Argo MCP for sync/health checks.
   - Use Kubernetes MCP to confirm target workload image tag.
5. Emit final report
   - Confirm success or return blocker details with remediation advice.

## Guardrails

- Promotions are PR-based only.
- Do not skip source environment validation.
- Do not promote when target environment has unresolved health issues.

## Example Input

```json
{
  "repo": "acme/platform-apps",
  "application": "billing",
  "manifest_mode": "helm",
  "image_name": "ghcr.io/acme/billing",
  "image_tag": "v4.8.1",
  "source_env": "staging",
  "target_env": "prod",
  "promotion_policy": "staging-green-before-prod"
}
```

## Example Output

```json
{
  "promotion_plan": "Promote billing image v4.8.1 from staging values file to prod values file.",
  "pull_request": "https://git.example.com/acme/platform-apps/pulls/131",
  "verification_report": "Prod Argo app synced and healthy; prod pods running ghcr.io/acme/billing:v4.8.1."
}
```
