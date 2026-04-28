# Orchestrator Agent

This ADK-Go agent is the control-plane coordinator for GitOps SRE workflows.

## Responsibilities

- Accept incident or change requests.
- Select and load the right skill from `gitops-skills/`.
- Delegate executable tasks to the `executor` agent.
- Enforce policy gates (risk, rollback criteria, change window).
- Produce final operator-facing remediation summary.

## MCP usage policy

- Uses MCP tools for read-heavy diagnosis first.
- Uses Git MCP write actions only after diagnosis is complete.
- Never exposes direct cluster mutation workflows outside GitOps path except approved emergency modes.

## Primary skill

- `gitops-sre-template` from `gitops-skills/gitops-sre-template/SKILL.md`
