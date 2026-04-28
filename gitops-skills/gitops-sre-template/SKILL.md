---
name: gitops-sre-template
description: Investigate and remediate GitOps production incidents using Argo CD, Argo Rollouts, Kubernetes, and Git provider MCP tools. Use when rollout health degrades, sync status drifts, or a GitOps change must be proposed and verified.
license: Apache-2.0
compatibility: Requires ADK-Go agents with MCP connections to Argo, Kubernetes, and Git provider servers.
metadata:
  owner: platform-engineering
  template_version: "1.0.0"
  domain: gitops-sre
allowed-tools: mcp__argo__* mcp__k8s__* mcp__git__*
---

# GitOps SRE Skill Template

## Objective

Provide a reusable incident workflow that starts from runtime symptoms, validates GitOps source of truth, proposes a safe change, and confirms cluster recovery.

## Inputs

- `environment`: target environment such as `prod`, `staging`, or `dev`.
- `application`: Argo CD application name.
- `namespace`: Kubernetes namespace for the workload.
- `incident_signal`: primary signal (for example, degraded rollout, failing health checks, sync drift, or elevated error rate).
- `change_window`: whether production changes are currently allowed.

## Outputs

- `diagnosis`: concise root-cause statement backed by MCP evidence.
- `remediation_plan`: ordered actions with risk notes.
- `git_change_spec`: branch, file targets, and intended manifest change.
- `verification_report`: post-change health checks across Argo, Kubernetes, and rollout status.

## Tool Bindings

- `argo-mcp`
  - Use for Argo CD application health, sync status, history, and rollback data.
  - Use for Argo Rollouts canary/blue-green status, analysis runs, and abort/promote decisions.
- `k8s-mcp`
  - Use for live cluster state validation: workloads, pods, events, services, and ingress.
- `git-mcp`
  - Use for repository inspection, branch creation, commit proposal, and pull request drafting.
  - Supported providers: GitHub, GitLab, and Gitea via public HTTPS APIs.

## Steps

1. Triage and scope
   - Confirm `environment`, `application`, and blast radius.
   - Gather current rollout and Argo app status before proposing changes.
2. Build hypothesis
   - Compare desired state (Git) with actual state (cluster + Argo sync status).
   - Identify whether failure source is config drift, bad release, or infra dependency.
3. Propose safest fix
   - Prefer minimal manifest deltas that restore healthy state.
   - If risk is high, prefer rollback or rollout abort before forward fix.
4. Execute GitOps change flow
   - Prepare branch and commit proposal with explicit rationale.
   - Open pull request with validation notes and rollback plan.
5. Verify recovery
   - Confirm Argo app is healthy and synced.
   - Confirm rollout converges and pod-level health stabilizes.
   - Record final incident summary and remaining risks.

## Guardrails

- Do not bypass GitOps by editing live resources unless emergency policy explicitly allows it.
- Do not merge changes without validation evidence from Argo and Kubernetes MCP tools.
- Always include rollback criteria in remediation output.

## Edge Cases

- If Git and cluster both appear healthy but incident persists, escalate as dependency or external outage.
- If provider API access fails on one Git provider, continue read-only diagnosis and report credential or network blocker.
