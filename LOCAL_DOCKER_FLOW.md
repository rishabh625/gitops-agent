# Local End-to-End Flow (Docker Compose)

This project uses:

- ADK-Go agents only (`orchestrator-agent`, `execution-agent`)
- MCP servers only (`git-mcp`, `argo-mcp`, `k8s-mcp`)
- No standalone APIs or custom microservices

## 1) Prepare environment

```bash
cp docker-compose.env.example .env
```

Edit `.env` and set valid MCP server images and credentials/tokens.

## 2) Start full runtime

```bash
docker compose up -d --build
docker compose ps
```

Services:

- `orchestrator-agent`
- `execution-agent`
- `git-mcp`
- `argo-mcp`
- `k8s-mcp`
- `adk-chat` (interactive CLI/WebUI launcher)

## 3) Check probe logs

```bash
docker compose logs --tail=50 orchestrator-agent execution-agent
```

Sample probe logs:

```text
orchestrator-agent  | level=INFO msg="mcp connectivity ok" server=git
orchestrator-agent  | level=INFO msg="mcp connectivity ok" server=argo
orchestrator-agent  | level=INFO msg="mcp connectivity ok" server=k8s
orchestrator-agent  | level=INFO msg="orchestrator probe passed" skills_loaded=2
execution-agent     | level=INFO msg="mcp connectivity ok" server=git
execution-agent     | level=INFO msg="mcp connectivity ok" server=argo
execution-agent     | level=INFO msg="mcp connectivity ok" server=k8s
execution-agent     | level=INFO msg="execution probe passed" skills_loaded=2
```

ADK chat web UI logs:

```bash
docker compose logs --tail=100 adk-chat
```

Open `http://localhost:8083/ui/` for the ADK web UI.

## 4) Flow: "Enable blue-green deployment"

### Step A: Orchestrator generates dynamic form

```bash
docker compose run --rm orchestrator-agent \
  -mode form \
  -task "Enable blue-green deployment" \
  -skills-root /app/gitops-skills
```

### Optional interactive chat (CLI + web)

Run interactive console chat:

```bash
docker compose run --rm adk-chat console
```

Web UI is already started by `docker compose up` at `http://localhost:8083/ui/`.

Expected form shape:

```json
{
  "task": "Enable Blue-Green Deployment",
  "fields": [
    {"name": "repo_url", "type": "string", "required": true},
    {"name": "app_path", "type": "string", "required": true},
    {"name": "cluster", "type": "string", "required": true},
    {"name": "strategy", "type": "select", "options": ["blueGreen"], "required": true}
  ]
}
```

### Step B: User submits form inputs; orchestrator delegates to execution agent logic

```bash
docker compose run --rm orchestrator-agent \
  -mode run \
  -task "Enable blue-green deployment" \
  -skills-root /app/gitops-skills \
  -inputs '{"repo_url":"https://github.com/acme/platform-gitops","app_path":"apps/payments","cluster":"gke-prod","strategy":"blueGreen","application":"payments-prod","repo":"acme/platform-gitops","path":"apps/payments","manifest_mode":"kustomize","service_names":"payments-active,payments-preview"}'
```

Execution includes skill-driven steps such as rollout transformation (`transform-to-argo-rollout` when matched by planning) and Git PR creation through MCP calls.

### Step C: Final output

Expected output shape:

```json
{
  "task": "Enable Blue-Green Deployment",
  "status": "success",
  "output": {
    "pr_link": "https://github.com/acme/platform-gitops/pull/123",
    "deployment_instructions": [
      "Review generated pull request changes and get required approvals.",
      "Merge the pull request only after CI and policy checks pass.",
      "Verify Service selectors route traffic to the blue-green active color."
    ],
    "argocd_sync_steps": [
      "argocd app get payments-prod",
      "argocd app sync payments-prod",
      "argocd app wait payments-prod --health --operation --timeout 600"
    ]
  }
}
```

## 5) Tear down

```bash
docker compose down -v
```

---

# Single Terraform Apply to GKE

Terraform in `terraform/` now applies:

1. MCP server stack in `mcp-system` namespace (`git`, `argo`, `k8s`)
2. Agent runtime stack in `agent-platform` namespace:
   - `orchestrator-agent` deployment
   - `execution-agent` deployment

The agent containers run probe loops and verify MCP reachability through internal cluster DNS endpoints.

## Apply

```bash
cd terraform
cp terraform.tfvars.example terraform.tfvars
# edit terraform.tfvars with images, tokens, and kube context
terraform init
terraform apply
```
