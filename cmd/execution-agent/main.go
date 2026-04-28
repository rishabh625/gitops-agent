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

	"google.golang.org/adk/agent"

	"gitops-agent/internal/executor"
	"gitops-agent/internal/mcpadapter"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/skills"
)

var _ = agent.NewSingleLoader

func main() {
	var (
		mode       = flag.String("mode", "run", "run|probe")
		task       = flag.String("task", "", "task text to execute")
		inputsRaw  = flag.String("inputs", "", "comma-separated key=value entries")
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

	loaderCfg := skills.LoaderConfig{
		Source:      skills.SourceType(getEnv("SKILL_SOURCE", "local")),
		LocalRoot:   *skillsRoot,
		GitHubOwner: os.Getenv("SKILL_GITHUB_OWNER"),
		GitHubRepo:  os.Getenv("SKILL_GITHUB_REPO"),
		GitHubRef:   getEnv("SKILL_GITHUB_REF", "main"),
		GitHubPath:  getEnv("SKILL_GITHUB_PATH", "gitops-skills"),
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
	}
	skillLoader := skills.NewLoader(loaderCfg, logger)
	loadedSkills, err := skillLoader.LoadAll(ctx)
	if err != nil {
		logger.Error("load skills failed", "error", err)
		os.Exit(1)
	}
	logger.Info("skills loaded", "count", len(loadedSkills), "source", loaderCfg.Source)

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

	if strings.EqualFold(strings.TrimSpace(*mode), "probe") {
		results := adapter.CheckConnectivity(ctx)
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
			logger.Info("execution probe passed", "skills_loaded", len(loadedSkills))
			return
		}
		os.Exit(1)
	}

	exec := executor.New(logger, planner.New(), adapter, loadedSkills)
	res, err := exec.Run(ctx, executor.Request{
		Task:   *task,
		Inputs: parseInputs(*inputsRaw),
	})
	if err != nil {
		logger.Error("execution failed", "error", err)
		printJSON(res)
		os.Exit(1)
	}
	printJSON(res)
}

func parseInputs(raw string) map[string]any {
	out := map[string]any{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	for _, pair := range strings.Split(raw, ",") {
		parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
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
