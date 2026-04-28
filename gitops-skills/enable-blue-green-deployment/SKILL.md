---
name: enable-blue-green-deployment
description: Enable a blue-green deployment strategy for a GitOps-managed application by proposing safe manifest updates, opening a pull request, and validating Argo CD sync readiness.
license: Apache-2.0
compatibility: Requires ADK-Go orchestrator and execution agent with MCP access to Git, Argo, and Kubernetes servers.
metadata:
  owner: platform-engineering
  domain: deployment-strategy
  intent: blue-green
  template_version: "1.0.0"
allowed-tools: mcp__git__* mcp__argo__* mcp__k8s__*
---

# Enable Blue-Green Deployment

## Objective

Convert an application rollout to a blue-green strategy through GitOps-only changes and provide post-merge sync guidance.

## Inputs

- `repo_url`: Git repository URL that contains the GitOps application manifests.
- `app_path`: Relative path to the application manifests in the repository.
- `cluster`: Target Kubernetes cluster name.
- `strategy`: Deployment strategy to apply, use `blueGreen`.

## Outputs

- `pr_link`: Pull request URL for blue-green manifest change.
- `deployment_instructions`: Ordered instructions to apply and verify deployment.
- `argocd_sync_steps`: Argo CD CLI steps to sync and verify health.

## Steps

1. Inspect GitOps manifests
   - Confirm the current workload and rollout strategy configuration in repository.
2. Prepare blue-green change
   - Draft the minimum manifest diff required to set rollout strategy to blue-green.
3. Open pull request
   - Create branch, commit change, and open a PR with rollback notes.
4. Validate readiness
   - Confirm Argo CD application can sync the proposed change and rollout checks are defined.
5. Return operator guidance
   - Provide PR link, deployment instructions, and Argo CD sync commands.

## Guardrails

- Do not mutate live cluster resources directly.
- Route all changes through Git pull requests.
- Include rollback criteria with every proposed change.
