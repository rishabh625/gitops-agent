package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/genai"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model/gemini"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"

	"gitops-agent/internal/enterprisesearch"
	"gitops-agent/internal/executor"
	"gitops-agent/internal/mcpadapter"
	"gitops-agent/internal/orchestrator"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/skills"
)

type chatRuntime struct {
	orchestrator *orchestrator.Orchestrator
	mcp          *mcpadapter.Adapter
}

type gitopsMCPQueryArgs struct {
	Server         string         `json:"server"`
	Intent         string         `json:"intent"`
	ToolArguments  map[string]any `json:"tool_arguments,omitempty"`
}

type generateFormArgs struct {
	Task string `json:"task"`
}

type callExecutionArgs struct {
	Task   string         `json:"task"`
	Inputs map[string]any `json:"inputs"`
}

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	loadedSkills, err := loadSkills(ctx, logger, getEnv("SKILLS_ROOT", "gitops-skills"))
	if err != nil {
		log.Fatalf("load skills failed: %v", err)
	}

	adapter := mcpadapter.New(mcpadapter.Config{
		Servers: []mcpadapter.ServerConfig{
			{
				Name:       mcpadapter.ServerGit,
				Endpoint:   getEnv("GIT_MCP_URL", "http://git-mcp:8080/mcp"),
				MaxRetries: intEnv("MCP_MAX_RETRIES", 2),
				Timeout:    durationEnv("MCP_TIMEOUT", 20*time.Second),
			},
			{
				Name:       mcpadapter.ServerArgo,
				Endpoint:   getEnv("ARGO_MCP_URL", "http://argo-mcp:8080/mcp"),
				MaxRetries: intEnv("MCP_MAX_RETRIES", 2),
				Timeout:    durationEnv("MCP_TIMEOUT", 20*time.Second),
			},
			{
				Name:       mcpadapter.ServerK8s,
				Endpoint:   getEnv("K8S_MCP_URL", "http://k8s-mcp:8080/mcp"),
				MaxRetries: intEnv("MCP_MAX_RETRIES", 2),
				Timeout:    durationEnv("MCP_TIMEOUT", 20*time.Second),
			},
		},
	}, logger)

	defer adapter.Close()

	if err := mcpStartupProbe(ctx, adapter, logger); err != nil {
		logger.Error("adk-chat exiting: MCP connectivity check failed", "error", err)
		os.Exit(1)
	}

	entSearch, err := enterprisesearch.NewFromEnv(logger)
	if err != nil {
		log.Fatalf("enterprise search configuration invalid: %v", err)
	}

	exec := executor.New(logger, planner.New(), adapter, loadedSkills, entSearch)
	rt := &chatRuntime{
		orchestrator: orchestrator.New(exec, planner.New(), loadedSkills, entSearch),
		mcp:          adapter,
	}

	model, err := gemini.NewModel(ctx, getEnv("ADK_MODEL", "gemini-2.5-flash"), &genai.ClientConfig{
		APIKey: mustEnv("GOOGLE_API_KEY"),
	})
	if err != nil {
		log.Fatalf("create model failed: %v", err)
	}

	mcpQueryTool, err := functiontool.New(functiontool.Config{
		Name: "gitops_mcp_query",
		Description: strings.TrimSpace(`
Query live systems through MCP: Argo CD (server=argo), Kubernetes (server=k8s), Git (server=git).
Call this proactively to answer factual questions (app health, sync state, resources, namespaces, pods, rollouts) before asking the user.
Use intent as short keywords describing the action (examples: "list argocd applications", "application status health sync", "kubernetes pods deployments namespace", "repository pull request").
Pass tool_arguments when you know exact parameter names from the task or from a prior tool response (application name, namespace, repo, etc.).
`),
	}, rt.gitopsMCPQuery)
	if err != nil {
		log.Fatalf("build gitops_mcp_query tool failed: %v", err)
	}

	generateFormTool, err := functiontool.New(functiontool.Config{
		Name:        "generate_dynamic_form",
		Description: "Build the skill input schema for a GitOps change task. Prefer gitops_mcp_query first to discover names and state; call this when you still need a structured form or before call_execution_agent.",
	}, rt.generateDynamicForm)
	if err != nil {
		log.Fatalf("build generate_dynamic_form tool failed: %v", err)
	}

	callExecutionTool, err := functiontool.New(functiontool.Config{
		Name:        "call_execution_agent",
		Description: "Run the skill pipeline with validated inputs. Populate inputs using facts from gitops_mcp_query whenever possible; only ask the user for values MCP cannot infer.",
	}, rt.callExecutionAgent)
	if err != nil {
		log.Fatalf("build call_execution_agent tool failed: %v", err)
	}

	rootAgent, err := llmagent.New(llmagent.Config{
		Name:        "gitops_orchestrator_agent",
		Model:       model,
		Description: "GitOps orchestrator that reads Argo CD and Kubernetes via MCP, answers directly when possible, and runs skills with minimal user prompting.",
		Instruction: strings.TrimSpace(`
You are the GitOps orchestrator.

Default behavior:
1) For questions about cluster or Argo CD state (what is running, health, sync, resources, versions, namespaces, apps): call gitops_mcp_query as many times as needed and answer from tool output. Do not ask the user for names or confirmation unless MCP cannot resolve them after a reasonable attempt.
2) For tasks that change Git or the cluster (PRs, rollouts, manifests): first use gitops_mcp_query to discover application/repo/namespace/path details, then either call call_execution_agent with a complete inputs map, or call generate_dynamic_form only for fields that remain unknown after MCP.
3) Ask the user only for information that is not discoverable via MCP (for example business choices, approvals, or secrets you must not read from the cluster).
4) When execution completes, summarize pr_link, deployment_instructions, and argocd_sync_steps from tool results.

Constraints:
- Use only the provided tools for facts and execution.
- Do not invent cluster or Argo CD state; cite what gitops_mcp_query returned.
- Keep answers concise and operational.
`),
		Tools: []tool.Tool{
			mcpQueryTool,
			generateFormTool,
			callExecutionTool,
		},
	})
	if err != nil {
		log.Fatalf("create orchestrator agent failed: %v", err)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(rootAgent),
	}
	l := full.NewLauncher()
	if err := l.Execute(ctx, config, os.Args[1:]); err != nil {
		log.Fatalf("run failed: %v\n\n%s", err, l.CommandLineSyntax())
	}
}

const mcpResponseMaxRunes = 32000

func (r *chatRuntime) gitopsMCPQuery(ctx tool.Context, args gitopsMCPQueryArgs) (map[string]any, error) {
	if r.mcp == nil {
		return nil, fmt.Errorf("mcp adapter not configured")
	}
	srv, err := parseMCPServer(args.Server)
	if err != nil {
		return nil, err
	}
	intent := strings.TrimSpace(args.Intent)
	if intent == "" {
		return nil, fmt.Errorf("intent is required")
	}
	toolArgs := args.ToolArguments
	if toolArgs == nil {
		toolArgs = map[string]any{}
	}
	text, err := r.mcp.CallByIntent(ctx, srv, intent, toolArgs)
	if err != nil {
		return nil, err
	}
	text = truncateMCPText(text, mcpResponseMaxRunes)
	return map[string]any{
		"server":   strings.ToLower(strings.TrimSpace(args.Server)),
		"intent":   intent,
		"response": text,
	}, nil
}

func parseMCPServer(s string) (mcpadapter.ServerName, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "argo", "argocd":
		return mcpadapter.ServerArgo, nil
	case "k8s", "kubernetes", "kube":
		return mcpadapter.ServerK8s, nil
	case "git", "github":
		return mcpadapter.ServerGit, nil
	default:
		return "", fmt.Errorf("server must be argo, k8s, or git (got %q)", s)
	}
}

func truncateMCPText(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes]) + "\n... (truncated)"
}

func (r *chatRuntime) generateDynamicForm(_ tool.Context, args generateFormArgs) (map[string]any, error) {
	form, err := r.orchestrator.GenerateForm(args.Task)
	if err != nil {
		return nil, err
	}
	return toMap(form)
}

func (r *chatRuntime) callExecutionAgent(_ tool.Context, args callExecutionArgs) (map[string]any, error) {
	res, err := r.orchestrator.SubmitAndRun(context.Background(), args.Task, args.Inputs)
	if err != nil {
		m, _ := toMap(res)
		if m == nil {
			return map[string]any{"status": "failed", "error": err.Error()}, nil
		}
		m["error"] = err.Error()
		return m, nil
	}
	return toMap(res)
}

func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// mcpStartupProbe lists tools on each configured MCP server (git, argo, k8s) before serving traffic.
// On any failure the process exits with code 1 (see main); adk-chat does not start console/web until this passes.
func mcpStartupProbe(ctx context.Context, adapter *mcpadapter.Adapter, log *slog.Logger) error {
	log.Info("adk-chat: checking MCP connectivity", "servers", "git,argo,k8s")
	results := adapter.CheckConnectivity(ctx)
	order := []mcpadapter.ServerName{mcpadapter.ServerGit, mcpadapter.ServerArgo, mcpadapter.ServerK8s}
	var failed []string
	for _, name := range order {
		err := results[name]
		if err != nil {
			log.Error("mcp connectivity failed", "server", name, "error", err)
			failed = append(failed, string(name))
			continue
		}
		log.Info("mcp connectivity ok", "server", name)
	}
	if len(failed) > 0 {
		return fmt.Errorf("MCP probe failed for %s — fix endpoints/credentials and retry", strings.Join(failed, ", "))
	}

	gitAuthProbeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := adapter.CallByIntent(gitAuthProbeCtx, mcpadapter.ServerGit, "get current user whoami", map[string]any{}); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no suitable mcp tool match") {
			return fmt.Errorf("git MCP auth probe failed: %w", err)
		}
		log.Warn("git MCP auth probe skipped (no suitable tool found)")
	}

	log.Info("adk-chat: MCP startup probe passed")
	return nil
}

func loadSkills(ctx context.Context, logger *slog.Logger, skillsRoot string) ([]skills.Skill, error) {
	loaderCfg := skills.LoaderConfig{
		Source:      skills.SourceType(getEnv("SKILL_SOURCE", "local")),
		LocalRoot:   skillsRoot,
		GitHubOwner: os.Getenv("SKILL_GITHUB_OWNER"),
		GitHubRepo:  os.Getenv("SKILL_GITHUB_REPO"),
		GitHubRef:   getEnv("SKILL_GITHUB_REF", "main"),
		GitHubPath:  getEnv("SKILL_GITHUB_PATH", "gitops-skills"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
	}
	return skills.NewLoader(loaderCfg, logger).LoadAll(ctx)
}

func mustEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		log.Fatalf("missing required env: %s", name)
	}
	return v
}

func getEnv(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}

func intEnv(name string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	var i int
	if _, err := fmt.Sscanf(v, "%d", &i); err != nil {
		return fallback
	}
	return i
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
