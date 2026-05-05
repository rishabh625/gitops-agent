package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"gitops-agent/internal/enterprisesearch"
	"gitops-agent/internal/executor"
	"gitops-agent/internal/mcpadapter"
	"gitops-agent/internal/orchestrator"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/skills"
)

func main() {
	var (
		mode       = flag.String("mode", "form", "form|run|probe")
		task       = flag.String("task", "", "natural language task to orchestrate")
		inputsRaw  = flag.String("inputs", "", "JSON object for run mode (example: '{\"repo_url\":\"...\"}')")
		logLevel   = flag.String("log-level", "info", "debug|info|warn|error")
		skillsRoot = flag.String("skills-root", "gitops-skills", "local skills directory (used with SKILL_SOURCE=local)")
	)
	flag.Parse()

	/*if strings.TrimSpace(*task) == "" && !strings.EqualFold(strings.TrimSpace(*mode), "probe") {
		fmt.Fprintln(os.Stderr, "missing -task")
		os.Exit(2)
	}*/

	logger := newLogger(*logLevel)
	ctx := context.Background()

	loadedSkills, err := loadSkills(ctx, logger, *skillsRoot)
	if err != nil {
		logger.Error("load skills failed", "error", err)
		os.Exit(1)
	}

	entSearch, err := enterprisesearch.NewFromEnv(logger)
	if err != nil {
		logger.Error("enterprise search configuration invalid", "error", err)
		os.Exit(1)
	}

	orc := orchestrator.New(nil, planner.New(), loadedSkills, entSearch)
	switch strings.ToLower(strings.TrimSpace(*mode)) {
	case "form":
		form, err := orc.GenerateForm(*task)
		if err != nil {
			logger.Error("form generation failed", "error", err)
			os.Exit(1)
		}
		printJSON(form)
	case "run":
		execAgent, err := newExecutionAgent(logger, loadedSkills, entSearch)
		if err != nil {
			logger.Error("executor init failed", "error", err)
			os.Exit(1)
		}
		defer execAgent.Close()

		orc = orchestrator.New(execAgent.executor, planner.New(), loadedSkills, entSearch)
		inputs, err := parseJSONInputs(*inputsRaw)
		if err != nil {
			logger.Error("invalid inputs", "error", err)
			os.Exit(2)
		}
		res, err := orc.SubmitAndRun(ctx, *task, inputs)
		printJSON(res)
		if err != nil {
			os.Exit(1)
		}
	case "probe":
		execAgent, err := newExecutionAgent(logger, loadedSkills, entSearch)
		if err != nil {
			logger.Error("executor init failed", "error", err)
			os.Exit(1)
		}
		defer execAgent.Close()

		results := execAgent.adapter.CheckConnectivity(ctx)
		ok := true
		for server, checkErr := range results {
			if checkErr != nil {
				ok = false
				logger.Error("mcp connectivity failed", "server", server, "error", checkErr)
				continue
			}
			logger.Info("mcp connectivity ok", "server", server)
		}
		if ok {
			logger.Info("orchestrator probe passed", "skills_loaded", len(loadedSkills))
			return
		}
		os.Exit(1)
	default:
		fmt.Fprintf(os.Stderr, "invalid -mode: %s (allowed: form|run|probe)\n", *mode)
		os.Exit(2)
	}
}

type executionAgentResources struct {
	executor *executor.Executor
	adapter  *mcpadapter.Adapter
}

func newExecutionAgent(logger *slog.Logger, loadedSkills []skills.Skill, knowledge executor.KnowledgeSearcher) (*executionAgentResources, error) {
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

	return &executionAgentResources{
		executor: executor.New(logger, planner.New(), adapter, loadedSkills, knowledge),
		adapter:  adapter,
	}, nil
}

func (r *executionAgentResources) Close() {
	if r != nil && r.adapter != nil {
		r.adapter.Close()
	}
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

func parseJSONInputs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	return slog.New(handler)
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

func mustEnv(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing required env: %s\n", name)
		os.Exit(2)
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
