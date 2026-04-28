package mcpadapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ServerName string

const (
	ServerGit  ServerName = "git"
	ServerArgo ServerName = "argo"
	ServerK8s  ServerName = "k8s"
)

type ServerConfig struct {
	Command    string
	Args       []string
	Name       ServerName
	Endpoint   string
	MaxRetries int
	Timeout    time.Duration
}

type Config struct {
	Servers []ServerConfig
}

type callIntent struct {
	Server ServerName
	Intent string
	Args   map[string]any
}

type Adapter struct {
	cfg      Config
	log      *slog.Logger
	mu       sync.Mutex
	sessions map[ServerName]*mcp.ClientSession
	tools    map[ServerName][]*mcp.Tool
}

func New(cfg Config, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{
		cfg:      cfg,
		log:      logger,
		sessions: map[ServerName]*mcp.ClientSession{},
		tools:    map[ServerName][]*mcp.Tool{},
	}
}

func (a *Adapter) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for name, s := range a.sessions {
		if s != nil {
			_ = s.Close()
		}
		delete(a.sessions, name)
	}
}

func (a *Adapter) CallByIntent(ctx context.Context, server ServerName, intent string, args map[string]any) (string, error) {
	toolName, err := a.resolveTool(ctx, server, intent)
	if err != nil {
		return "", err
	}
	return a.callToolWithRetry(ctx, server, toolName, args)
}

// CheckConnectivity verifies that each configured MCP server is reachable and exposes tools.
func (a *Adapter) CheckConnectivity(ctx context.Context) map[ServerName]error {
	out := map[ServerName]error{}
	for _, cfg := range a.cfg.Servers {
		_, err := a.listTools(ctx, cfg.Name)
		out[cfg.Name] = err
	}
	return out
}

func (a *Adapter) resolveTool(ctx context.Context, server ServerName, intent string) (string, error) {
	tools, err := a.listTools(ctx, server)
	if err != nil {
		return "", err
	}
	intent = strings.ToLower(intent)

	score := func(t *mcp.Tool) int {
		if t == nil {
			return -1
		}
		name := strings.ToLower(t.Name)
		desc := strings.ToLower(t.Description)
		score := 0
		for _, kw := range strings.Fields(intent) {
			if strings.Contains(name, kw) {
				score += 3
			}
			if strings.Contains(desc, kw) {
				score += 2
			}
		}
		return score
	}

	bestScore := -1
	bestName := ""
	for _, t := range tools {
		s := score(t)
		if s > bestScore {
			bestScore = s
			bestName = t.Name
		}
	}
	if bestName == "" {
		return "", fmt.Errorf("no tools available on %s MCP server", server)
	}
	a.log.Info("resolved tool by intent", "server", server, "intent", intent, "tool", bestName)
	return bestName, nil
}

func (a *Adapter) callToolWithRetry(ctx context.Context, server ServerName, tool string, args map[string]any) (string, error) {
	cfg, ok := a.serverCfg(server)
	if !ok {
		return "", fmt.Errorf("unknown server config: %s", server)
	}
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		sess, err := a.ensureSession(ctx, cfg)
		if err != nil {
			lastErr = err
		} else {
			callCtx := ctx
			if cfg.Timeout > 0 {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
				defer cancel()
			}
			result, err := sess.CallTool(callCtx, &mcp.CallToolParams{
				Name:      tool,
				Arguments: args,
			})
			if err == nil {
				if result.IsError {
					lastErr = fmt.Errorf("mcp tool %s returned error result", tool)
				} else {
					return renderToolContent(result), nil
				}
			} else {
				lastErr = err
			}
		}

		if attempt < maxRetries {
			sleep := time.Duration(1<<attempt) * 500 * time.Millisecond
			a.log.Warn("retrying MCP tool call", "server", server, "tool", tool, "attempt", attempt+1, "sleep", sleep, "error", lastErr)
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
	}
	return "", fmt.Errorf("mcp call failed after retries: %w", lastErr)
}

func (a *Adapter) listTools(ctx context.Context, server ServerName) ([]*mcp.Tool, error) {
	a.mu.Lock()
	known, ok := a.tools[server]
	a.mu.Unlock()
	if ok && len(known) > 0 {
		return known, nil
	}

	cfg, ok := a.serverCfg(server)
	if !ok {
		return nil, fmt.Errorf("unknown server config: %s", server)
	}
	sess, err := a.ensureSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	out, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("list tools from %s: %w", server, err)
	}
	a.mu.Lock()
	a.tools[server] = slices.Clone(out.Tools)
	a.mu.Unlock()
	return out.Tools, nil
}

func (a *Adapter) ensureSession(ctx context.Context, cfg ServerConfig) (*mcp.ClientSession, error) {
	a.mu.Lock()
	existing := a.sessions[cfg.Name]
	a.mu.Unlock()
	if existing != nil {
		return existing, nil
	}

	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("endpoint missing for server %s", cfg.Name)
	}
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "gitops-execution-agent",
		Version: "v0.1.0",
	}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: cfg.Endpoint,
		HTTPClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		MaxRetries: 3,
	}

	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s mcp: %w", cfg.Name, err)
	}

	a.mu.Lock()
	a.sessions[cfg.Name] = sess
	a.mu.Unlock()
	a.log.Info("connected MCP session", "server", cfg.Name, "endpoint", cfg.Endpoint)
	return sess, nil
}

func (a *Adapter) serverCfg(name ServerName) (ServerConfig, bool) {
	for _, s := range a.cfg.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return ServerConfig{}, false
}

func renderToolContent(result *mcp.CallToolResult) string {
	var out []string
	for _, c := range result.Content {
		switch x := c.(type) {
		case *mcp.TextContent:
			out = append(out, x.Text)
		default:
			out = append(out, fmt.Sprintf("%v", c))
		}
	}
	return strings.Join(out, "\n")
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "tempor") ||
		strings.Contains(msg, "reset") ||
		errors.Is(err, context.DeadlineExceeded)
}
