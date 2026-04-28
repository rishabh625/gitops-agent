# Executor Agent

This ADK-Go agent performs concrete tool-driven steps delegated by the orchestrator.

## Responsibilities

- Run MCP tool calls for Argo, Kubernetes, and Git providers.
- Return structured evidence (status snapshots, diffs, validation checks).
- Execute the ordered steps declared by the active skill.
- Stop and report blockers with enough detail for orchestrator decisions.

## MCP usage policy

- `argo-mcp` for app sync and rollout status.
- `k8s-mcp` for pod/workload/event state checks.
- `git-mcp` for branch, commit, and PR operations against GitHub, GitLab, or Gitea.

## Contract with orchestrator

- Input: scoped task with expected output schema.
- Output: evidence + result status (`success`, `blocked`, `needs-approval`).
