# ADK-Go Agent Runtime Contract

## Agent topology

- `orchestrator` agent
  - Owns workflow control, policy checks, and final output synthesis.
- `executor` agent
  - Executes tool-bound tasks and returns evidence.

## Skill loading

- Skills are discovered under `gitops-skills/`.
- Orchestrator chooses skill by matching task intent to skill `description`.
- Active skill instructions are forwarded to executor with narrowed task context.

## MCP-only tool policy

- Allowed tool domains:
  - Argo MCP
  - Kubernetes MCP
  - Git MCP
- Disallowed:
  - Standalone service APIs not exposed via MCP
  - Custom microservices for orchestration

## Execution pattern

1. Orchestrator classifies request.
2. Orchestrator loads skill and emits step tasks.
3. Executor runs MCP calls and returns evidence.
4. Orchestrator evaluates guardrails and decides continue/rollback/escalate.
5. Orchestrator produces final incident or change response.
