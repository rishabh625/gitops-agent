---
name: update-image-tag
description: Update container image tags in GitOps repositories for Helm or Kustomize workloads and create a pull request for promotion. Use when a new image version must be rolled out without direct cluster mutation.
license: Apache-2.0
compatibility: Requires MCP connectivity to Git, Argo, and Kubernetes servers. No CLI tools required.
metadata:
  owner: platform-engineering
  category: release-management
  version: "1.0.0"
allowed-tools: mcp__git__* mcp__argo__* mcp__k8s__*
---

# Update Image Tag

## Inputs

- `repo`: Git repository identifier.
- `path`: workload path.
- `application`: Argo CD application name.
- `manifest_mode`: `helm` or `kustomize`.
- `image_name`: container image repository.
- `new_tag`: target image tag.
- `environment`: target environment name.

## Outputs

- `modified_files`: list of files updated with new tag.
- `diff_summary`: logical summary of image changes.
- `pull_request`: PR URL or identifier.
- `argo_status`: pre/post sync and health snapshot.

## Tool Bindings

- **Git MCP**: locate image references, edit files, commit changes, open PR.
- **Argo MCP**: verify application status before and after merge.
- **Kubernetes MCP**: confirm running image and workload mapping for validation.

## Steps

1. Resolve workload and source format
   - Use Git MCP to identify Helm vs Kustomize structure.
   - Locate image references (`values.yaml`, chart templates, `kustomization.yaml`, image patches).
2. Validate target change
   - Ensure `new_tag` differs from current desired tag.
   - Confirm workload in Kubernetes MCP maps to intended application and namespace.
3. Update manifests through Git MCP
   - Create branch using release-safe naming.
   - Update only relevant image fields for selected environment.
   - Keep unrelated values unchanged.
4. Open pull request
   - Include before/after tag values and rollback tag.
   - Attach Argo app context and expected rollout behavior.
5. Verify after merge
   - Use Argo MCP to confirm synced and healthy.
   - Use Kubernetes MCP to verify running image tag matches desired state.

## Guardrails

- No direct cluster mutation commands.
- All changes must be submitted and merged via PR.
- Fail if file updates would affect multiple unintended apps.

## Example Input

```json
{
  "repo": "acme/platform-apps",
  "path": "apps/checkout/overlays/prod",
  "application": "checkout-prod",
  "manifest_mode": "kustomize",
  "image_name": "ghcr.io/acme/checkout",
  "new_tag": "v2.14.3",
  "environment": "prod"
}
```

## Example Output

```json
{
  "modified_files": [
    "apps/checkout/overlays/prod/kustomization.yaml"
  ],
  "diff_summary": "Updated ghcr.io/acme/checkout tag from v2.14.2 to v2.14.3",
  "pull_request": "https://git.example.com/acme/platform-apps/pulls/129",
  "argo_status": "Pre-merge Healthy/Synced. Post-merge rollout completed Healthy/Synced."
}
```
