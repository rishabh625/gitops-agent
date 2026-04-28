---
name: validate-gitops-repo
description: Validate GitOps repository consistency against Argo CD application targets and live Kubernetes objects for Helm or Kustomize layouts. Use when checking drift risk, structure correctness, and promotion readiness before opening or merging PRs.
license: Apache-2.0
compatibility: Requires MCP connectivity to Git, Argo, and Kubernetes servers. No CLI tools required.
metadata:
  owner: platform-engineering
  category: repo-validation
  version: "1.0.0"
allowed-tools: mcp__git__* mcp__argo__* mcp__k8s__*
---

# Validate GitOps Repo

## Inputs

- `repo`: Git repository identifier.
- `path`: application path or environment path.
- `application`: Argo CD application name.
- `manifest_mode`: `helm` or `kustomize`.
- `environment`: validation target environment.

## Outputs

- `validation_status`: `pass` or `fail`.
- `findings`: structured list of blocking and non-blocking issues.
- `recommended_actions`: ordered remediation suggestions.
- `pr_readiness`: whether changes are ready for PR merge.

## Tool Bindings

- **Git MCP**: inspect repository layout, manifests, and referenced files.
- **Argo MCP**: read app source path, sync state, and health.
- **Kubernetes MCP**: compare expected objects with live cluster objects.

## Steps

1. Validate source layout
   - For Helm: verify chart structure and environment values mapping.
   - For Kustomize: verify base/overlay references and image/patch declarations.
2. Validate Argo mapping
   - Confirm Argo app source repo/path/target revision points to expected location.
   - Confirm project and destination scope match intended environment.
3. Validate desired vs actual
   - Compare key resources in Git with live Kubernetes state via MCP evidence.
   - Flag mismatch patterns likely to break sync or rollout behavior.
4. Produce readiness report
   - Classify findings by severity.
   - Mark whether PR can proceed or requires fixes first.

## Guardrails

- No direct in-cluster mutations during validation.
- Do not mark `pass` when blocking findings exist.
- Always include concrete file/resource references in findings.

## Example Input

```json
{
  "repo": "acme/platform-apps",
  "path": "apps/inventory/overlays/staging",
  "application": "inventory-staging",
  "manifest_mode": "kustomize",
  "environment": "staging"
}
```

## Example Output

```json
{
  "validation_status": "fail",
  "findings": [
    "Blocking: kustomization references missing patch file apps/inventory/patches/hpa.yaml",
    "Non-blocking: Argo targetRevision uses floating branch instead of pinned release branch"
  ],
  "recommended_actions": [
    "Add missing patch file or remove stale reference",
    "Pin targetRevision for promotion safety"
  ],
  "pr_readiness": "not-ready"
}
```
