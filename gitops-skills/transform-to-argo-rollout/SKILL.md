---
name: transform-to-argo-rollout
description: Convert Kubernetes Deployments into Argo Rollout manifests with blue-green or canary strategies while preserving GitOps flow. Use when migration to progressive delivery is requested for Helm or Kustomize repositories.
license: Apache-2.0
compatibility: Requires MCP connectivity to Git, Argo, and Kubernetes servers. No CLI tools required.
metadata:
  owner: platform-engineering
  category: gitops-migration
  version: "1.0.0"
allowed-tools: mcp__git__* mcp__argo__* mcp__k8s__*
---

# Transform Deployment To Argo Rollout

## Inputs

- `repo`: Git repository identifier and default branch.
- `path`: target app path in the repo.
- `application`: Argo CD application name.
- `strategy`: `blue-green` or `canary`.
- `traffic_router`: optional routing integration name.
- `manifest_mode`: `helm` or `kustomize`.
- `service_names`: active/preview services for blue-green or stable/canary services for canary.

## Outputs

- `change_plan`: migration summary and touched files.
- `rollout_spec`: key rollout strategy fields introduced.
- `pull_request`: URL or identifier for the created PR.
- `validation_report`: Argo and Kubernetes verification findings.

## Tool Bindings

- **Git MCP**: inspect repo, create branch, edit manifests, commit, open PR.
- **Argo MCP**: read app sync and health, verify post-change status.
- **Kubernetes MCP**: verify target workload shape and service references before migration.

## Steps

1. Discover current deployment model
   - Use Kubernetes MCP to inspect the current `Deployment` and service selectors.
   - Use Git MCP to detect whether the path is Helm (`Chart.yaml`, `values.yaml`, templates) or Kustomize (`kustomization.yaml`).
2. Build rollout conversion plan
   - Map Deployment fields (`replicas`, selectors, pod template, probes, resources) into Rollout spec.
   - For `blue-green`, configure active/preview service references.
   - For `canary`, configure steps (setWeight/pause) and optional analysis hooks.
3. Apply repository changes via Git MCP
   - Create a feature branch.
   - Replace or transform Deployment manifests to Rollout manifests.
   - Update Helm templates/values or Kustomize resources/patches accordingly.
4. Validate GitOps safety
   - Confirm changed manifests remain renderable in selected `manifest_mode`.
   - Confirm Argo application target path and objects remain aligned.
5. Open pull request
   - Create PR with migration rationale, risk notes, and rollback instructions.
   - Do not apply live mutations outside GitOps.
6. Post-PR verification
   - Use Argo MCP to monitor sync/health after PR merge.
   - Use Kubernetes MCP to verify rollout controller objects and service routing state.

## Guardrails

- Never mutate live workloads directly; all updates go through Git PRs.
- Preserve labels/selectors to avoid service disconnects.
- Keep strategy-specific defaults conservative unless explicit overrides are provided.

## Example Input

```json
{
  "repo": "acme/platform-apps",
  "path": "apps/payments",
  "application": "payments-prod",
  "strategy": "canary",
  "traffic_router": "nginx",
  "manifest_mode": "kustomize",
  "service_names": {
    "stable": "payments-stable",
    "canary": "payments-canary"
  }
}
```

## Example Output

```json
{
  "change_plan": "Converted Deployment payments to Rollout with canary steps and service split.",
  "rollout_spec": {
    "strategy": "canary",
    "steps": ["setWeight:20", "pause:5m", "setWeight:50", "pause:10m", "setWeight:100"]
  },
  "pull_request": "https://git.example.com/acme/platform-apps/pulls/128",
  "validation_report": "Argo app remains synced post-merge; rollout progressed to Healthy."
}
```
