package mcpadapter

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

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
	args = normalizeArgsForServer(server, args)
	toolName, err := a.resolveTool(ctx, server, intent, args)
	if err != nil {
		return "", err
	}
	return a.callToolWithRetry(ctx, server, toolName, args)
}

func (a *Adapter) ResolveTool(ctx context.Context, server ServerName, intent string, args map[string]any) (string, error) {
	args = normalizeArgsForServer(server, args)
	return a.resolveTool(ctx, server, intent, args)
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

func (a *Adapter) resolveTool(ctx context.Context, server ServerName, intent string, args map[string]any) (string, error) {
	tools, err := a.listTools(ctx, server)
	if err != nil {
		return "", err
	}
	intent = strings.ToLower(intent)
	intentTokens := strings.Fields(intent)

	containsAny := func(tokens []string, values ...string) bool {
		if len(tokens) == 0 {
			return false
		}
		set := map[string]bool{}
		for _, t := range tokens {
			set[t] = true
		}
		for _, v := range values {
			if set[v] {
				return true
			}
		}
		return false
	}

	wantsWrite := containsAny(intentTokens, "create", "open", "new", "update", "edit", "modify", "delete", "remove", "commit", "push", "write", "set", "add")
	wantsCreate := containsAny(intentTokens, "create", "open", "new")
	wantsUpdate := containsAny(intentTokens, "update", "edit", "modify", "set")
	wantsCommit := containsAny(intentTokens, "commit")
	wantsPush := containsAny(intentTokens, "push")
	wantsBranch := containsAny(intentTokens, "branch")
	wantsPR := containsAny(intentTokens, "pull", "pullrequest", "pr", "merge_request", "mergerequest", "mr") || strings.Contains(intent, "pull request") || strings.Contains(intent, "merge request")

	requiredVerbMatch := []string{}
	switch {
	case wantsCreate:
		requiredVerbMatch = []string{"create", "open", "new"}
	case wantsUpdate:
		requiredVerbMatch = []string{"update", "edit", "modify", "set"}
	case wantsCommit:
		requiredVerbMatch = []string{"commit"}
	case wantsPush:
		requiredVerbMatch = []string{"push", "upload"}
	}

	requiredMissing := func(t *mcp.Tool) []string {
		req, _, _ := toolSchema(t)
		if len(req) == 0 {
			return nil
		}
		missing := make([]string, 0, len(req))
		for k := range req {
			if _, ok := args[k]; !ok {
				missing = append(missing, k)
			}
		}
		slices.Sort(missing)
		return missing
	}

	score := func(t *mcp.Tool) int {
		if t == nil {
			return -1
		}
		if wantsWrite && t.Annotations != nil && t.Annotations.ReadOnlyHint {
			return -1
		}
		if len(requiredMissing(t)) > 0 {
			return -1
		}
		name := strings.ToLower(t.Name)
		desc := strings.ToLower(t.Description)
		if len(requiredVerbMatch) > 0 {
			matched := false
			for _, v := range requiredVerbMatch {
				if strings.Contains(name, v) || strings.Contains(desc, v) {
					matched = true
					break
				}
			}
			if !matched {
				return -1
			}
		}
		if wantsPR {
			if !(strings.Contains(name, "pull") ||
				strings.Contains(name, "merge_request") ||
				strings.Contains(name, "merge request") ||
				strings.Contains(name, "pr") ||
				strings.Contains(desc, "pull request") ||
				strings.Contains(desc, "merge request") ||
				strings.Contains(desc, "pull")) {
				return -1
			}
		}
		if wantsBranch && !wantsPR {
			if !(strings.Contains(name, "branch") || strings.Contains(name, "ref") || strings.Contains(desc, "branch")) {
				return -1
			}
		}
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
	if bestName == "" || bestScore <= 0 {
		type cand struct {
			name    string
			missing []string
			score   int
		}
		var cands []cand
		for _, t := range tools {
			if t == nil {
				continue
			}
			name := t.Name
			missing := requiredMissing(t)
			nameLower := strings.ToLower(name)
			descLower := strings.ToLower(t.Description)
			s := 0
			for _, kw := range strings.Fields(intent) {
				if strings.Contains(nameLower, kw) {
					s += 3
				}
				if strings.Contains(descLower, kw) {
					s += 2
				}
			}
			cands = append(cands, cand{name: name, missing: missing, score: s})
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].score == cands[j].score {
				return cands[i].name < cands[j].name
			}
			return cands[i].score > cands[j].score
		})
		var lines []string
		for i, c := range cands {
			if i >= 5 {
				break
			}
			if len(c.missing) > 0 {
				lines = append(lines, fmt.Sprintf("%s (missing: %s)", c.name, strings.Join(c.missing, ",")))
			} else {
				lines = append(lines, c.name)
			}
		}
		if len(lines) == 0 {
			return "", fmt.Errorf("no MCP tools available on server=%s", server)
		}
		return "", fmt.Errorf("no suitable MCP tool match for intent=%q on server=%s; top candidates: %s", intent, server, strings.Join(lines, " | "))
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
			toolDef, _ := a.getTool(ctx, server, tool)
			callArgs := filterArgsForTool(toolDef, args)
			callArgs = coerceArgsForTool(toolDef, callArgs)

			callCtx := ctx
			cancel := func() {}
			if cfg.Timeout > 0 {
				callCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
			}
			result, err := sess.CallTool(callCtx, &mcp.CallToolParams{
				Name:      tool,
				Arguments: callArgs,
			})
			cancel()
			if err == nil {
				if result.IsError {
					msg := strings.TrimSpace(renderToolContent(result))
					if msg == "" {
						lastErr = fmt.Errorf("mcp tool %s returned error result", tool)
					} else {
						lastErr = fmt.Errorf("mcp tool %s returned error: %s", tool, msg)
					}
				} else {
					return renderToolContent(result), nil
				}
			} else {
				lastErr = err
			}
		}

		toolReturnedErrorResult := lastErr != nil && strings.Contains(strings.ToLower(lastErr.Error()), "returned error")
		retryable := IsRetryable(lastErr)
		if toolReturnedErrorResult && !retryable {
			return "", lastErr
		}

		if retryable {
			a.resetServerSession(server)
		}

		if attempt < maxRetries && retryable {
			sleep := time.Duration(1<<attempt) * 500 * time.Millisecond
			a.log.Warn("retrying MCP tool call", "server", server, "tool", tool, "attempt", attempt+1, "sleep", sleep, "error", lastErr)
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		} else if attempt < maxRetries && !retryable {
			break
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

func (a *Adapter) getTool(ctx context.Context, server ServerName, name string) (*mcp.Tool, bool) {
	tools, err := a.listTools(ctx, server)
	if err != nil {
		return nil, false
	}
	for _, t := range tools {
		if t != nil && t.Name == name {
			return t, true
		}
	}
	return nil, false
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
	httpClient := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   cfg.Endpoint,
		HTTPClient: httpClient,
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

func (a *Adapter) resetServerSession(server ServerName) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.sessions[server]; s != nil {
		_ = s.Close()
	}
	delete(a.sessions, server)
	delete(a.tools, server)
}

func toolSchema(t *mcp.Tool) (required map[string]bool, properties map[string]bool, additionalAllowed bool) {
	if t == nil || t.InputSchema == nil {
		return nil, nil, true
	}
	schema, ok := t.InputSchema.(map[string]any)
	if !ok || schema == nil {
		return nil, nil, true
	}

	additionalAllowed = true
	if v, ok := schema["additionalProperties"]; ok {
		if b, ok := v.(bool); ok {
			additionalAllowed = b
		}
	}

	if propsRaw, ok := schema["properties"]; ok {
		if props, ok := propsRaw.(map[string]any); ok && props != nil {
			properties = map[string]bool{}
			for k := range props {
				properties[k] = true
			}
		}
	}

	if reqRaw, ok := schema["required"]; ok {
		if reqList, ok := reqRaw.([]any); ok && len(reqList) > 0 {
			required = map[string]bool{}
			for _, v := range reqList {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					required[s] = true
				}
			}
		}
	}

	return required, properties, additionalAllowed
}

func filterArgsForTool(t *mcp.Tool, args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	_, props, additionalAllowed := toolSchema(t)
	if additionalAllowed || len(props) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if props[k] {
			out[k] = v
		}
	}
	return out
}

func normalizeArgsForServer(server ServerName, args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	out := args
	cloned := false
	setIfMissing := func(k string, v any) {
		if v == nil {
			return
		}
		if _, ok := out[k]; ok {
			return
		}
		if !cloned {
			out = make(map[string]any, len(args)+6)
			for kk, vv := range args {
				out[kk] = vv
			}
			cloned = true
		}
		out[k] = v
	}
	getString := func(k string) (string, bool) {
		v, ok := out[k]
		if !ok {
			return "", false
		}
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		s = strings.TrimSpace(s)
		return s, s != ""
	}

	switch server {
	case ServerGit:
		if repo, ok := getString("repo"); ok {
			if normalized := repoFromURL(repo); normalized != "" && normalized != repo {
				if !cloned {
					out = make(map[string]any, len(args)+6)
					for kk, vv := range args {
						out[kk] = vv
					}
					cloned = true
				}
				out["repo"] = normalized
			}
			setIfMissing("repository", fmt.Sprintf("%v", out["repo"]))
		} else if repoURL, ok := getString("repo_url"); ok {
			if repo := repoFromURL(repoURL); repo != "" {
				setIfMissing("repo", repo)
				setIfMissing("repository", repo)
			}
		} else if repoURL, ok := getString("repository_url"); ok {
			if repo := repoFromURL(repoURL); repo != "" {
				setIfMissing("repo", repo)
				setIfMissing("repository", repo)
			}
		}
		if _, ok := getString("path"); !ok {
			if p, ok := getString("app_path"); ok {
				setIfMissing("path", p)
			}
		}
		if repo, ok := getString("repo"); ok {
			owner, name := splitOwnerRepo(repo)
			if owner != "" && name != "" {
				// GitHub MCP (and similar) build /repos/{owner}/{repo} using both fields. If we leave
				// repo as "owner/name" while also setting owner, the tool double-counts the owner
				// (e.g. .../repos/rishabh625/rishabh625/helmsample → 404).
				if !cloned {
					out = make(map[string]any, len(args)+6)
					for kk, vv := range args {
						out[kk] = vv
					}
					cloned = true
				}
				out["owner"] = owner
				out["repo"] = name
				out["repository"] = owner + "/" + name
				setIfMissing("repo_name", name)
				setIfMissing("repoName", name)
				setIfMissing("name", name)
			}
		}
		if _, ok := getString("change_branch"); !ok {
			if b, ok := getString("branch"); ok {
				setIfMissing("change_branch", b)
			} else if b, ok := getString("branch_name"); ok {
				setIfMissing("change_branch", b)
			} else if b := suggestChangeBranch(out); b != "" {
				setIfMissing("change_branch", b)
				setIfMissing("branch", b)
				setIfMissing("branch_name", b)
			}
		} else if b, ok := getString("change_branch"); ok {
			setIfMissing("branch", b)
			setIfMissing("branch_name", b)
		}

		base := ""
		if s, ok := getString("base_branch"); ok {
			base = s
		} else if s, ok := getString("base"); ok {
			base = s
		} else if s, ok := getString("target_branch"); ok {
			base = s
		} else if s, ok := getString("targetBranch"); ok {
			base = s
		}
		if base == "" {
			base = "main"
		}
		setIfMissing("base", base)
		setIfMissing("base_branch", base)
		setIfMissing("baseBranch", base)
		setIfMissing("target_branch", base)
		setIfMissing("targetBranch", base)

		head := ""
		if s, ok := getString("change_branch"); ok {
			head = s
		} else if s, ok := getString("head"); ok {
			head = s
		} else if s, ok := getString("head_branch"); ok {
			head = s
		} else if s, ok := getString("headBranch"); ok {
			head = s
		} else if s, ok := getString("source_branch"); ok {
			head = s
		}
		if head != "" {
			setIfMissing("head", head)
			setIfMissing("head_branch", head)
			setIfMissing("headBranch", head)
			setIfMissing("source_branch", head)
			setIfMissing("sourceBranch", head)
		}

		if _, ok := getString("title"); !ok {
			title := suggestPRTitle(out)
			if title != "" {
				setIfMissing("title", title)
				setIfMissing("pr_title", title)
				setIfMissing("pull_request_title", title)
			}
		}
		if _, ok := getString("body"); !ok {
			body := suggestPRBody(out)
			if body != "" {
				setIfMissing("body", body)
				setIfMissing("pr_body", body)
				setIfMissing("description", body)
			}
		}
	}
	return out
}

func suggestPRTitle(args map[string]any) string {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	app := get("application", "app", "app_name")
	image := get("image_name", "image", "container_image")
	tag := get("new_tag", "image_tag", "tag", "version")
	if app == "" {
		app = "gitops"
	}
	if image != "" && tag != "" {
		return fmt.Sprintf("chore(%s): bump %s to %s", app, image, tag)
	}
	if tag != "" {
		return fmt.Sprintf("chore(%s): upgrade to %s", app, tag)
	}
	return fmt.Sprintf("chore(%s): update manifests", app)
}

func suggestPRBody(args map[string]any) string {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	task := get("task")
	app := get("application", "app", "app_name")
	repo := get("repo", "repository", "repo_url")
	path := get("path", "app_path")
	image := get("image_name", "image", "container_image")
	tag := get("new_tag", "image_tag", "tag", "version")
	env := get("environment", "env")
	var lines []string
	if task != "" {
		lines = append(lines, "Task: "+task)
	}
	if app != "" {
		lines = append(lines, "Application: "+app)
	}
	if env != "" {
		lines = append(lines, "Environment: "+env)
	}
	if repo != "" {
		lines = append(lines, "Repo: "+repo)
	}
	if path != "" {
		lines = append(lines, "Path: "+path)
	}
	if image != "" && tag != "" {
		lines = append(lines, fmt.Sprintf("Change: %s -> %s", image, tag))
	} else if tag != "" {
		lines = append(lines, "Change: upgrade to "+tag)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func suggestChangeBranch(args map[string]any) string {
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	slug := get("application", "app", "app_name", "app_path", "path")
	if slug == "" {
		slug = "change"
	}
	slug = slugifyBranch(slug)
	if slug == "" {
		slug = "change"
	}

	relevant := map[string]string{
		"repo":        get("repo", "repository", "repo_url"),
		"path":        get("path", "app_path"),
		"application": get("application", "app", "app_name"),
		"strategy":    get("strategy"),
		"version":     get("version", "new_tag", "image_tag", "tag"),
	}
	var keys []string
	for k, v := range relevant {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	h := fnv.New32a()
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(relevant[k]))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum32()
	return fmt.Sprintf("adk/%s-%08x", slug, sum)
}

func slugifyBranch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	lastDash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '/' {
			if b.Len() == 0 || lastDash {
				continue
			}
			b.WriteRune('-')
			lastDash = true
			continue
		}
		if b.Len() == 0 || lastDash {
			continue
		}
		b.WriteRune('-')
		lastDash = true
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 30 {
		out = out[:30]
		out = strings.Trim(out, "-")
	}
	return out
}

func repoFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.TrimRight(raw, ".:") // common copy/paste trailing punctuation
	// git@github.com:owner/repo.git
	if strings.HasPrefix(raw, "git@") {
		if i := strings.Index(raw, ":"); i >= 0 && i+1 < len(raw) {
			raw = raw[i+1:]
		}
	}
	// https://github.com/owner/repo(.git)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil && u != nil {
			raw = u.Path
		}
	}
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 {
		return ""
	}
	// Prefer owner/repo from the start of the path. Many UIs include extra segments like:
	// /owner/repo/tree/main/apps/foo or /owner/repo/blob/main/README.md
	owner := parts[0]
	repo := parts[1]
	if owner == "" || repo == "" {
		return ""
	}
	return owner + "/" + repo
}

func splitOwnerRepo(repo string) (owner string, name string) {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimPrefix(repo, "/")
	parts := strings.Split(repo, "/")
	if len(parts) < 2 {
		return "", ""
	}
	return parts[0], parts[1]
}

func coerceArgsForTool(t *mcp.Tool, args map[string]any) map[string]any {
	if t == nil || t.InputSchema == nil || args == nil {
		return args
	}
	schema, ok := t.InputSchema.(map[string]any)
	if !ok || schema == nil {
		return args
	}
	propsRaw, ok := schema["properties"]
	if !ok {
		return args
	}
	props, ok := propsRaw.(map[string]any)
	if !ok || props == nil {
		return args
	}

	out := args
	cloned := false
	for k, v := range args {
		propRaw, ok := props[k]
		if !ok {
			continue
		}
		prop, ok := propRaw.(map[string]any)
		if !ok || prop == nil {
			continue
		}

		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			continue
		}
		if !schemaExpectsObject(prop) {
			continue
		}
		wrapped := wrapStringAsObject(prop, s)
		if wrapped == nil {
			continue
		}
		if !cloned {
			out = make(map[string]any, len(args))
			for kk, vv := range args {
				out[kk] = vv
			}
			cloned = true
		}
		out[k] = wrapped
	}
	return out
}

func schemaExpectsObject(s map[string]any) bool {
	if s == nil {
		return false
	}
	if t, ok := s["type"]; ok {
		switch x := t.(type) {
		case string:
			if x == "object" {
				return true
			}
		case []any:
			for _, v := range x {
				if sv, ok := v.(string); ok && sv == "object" {
					return true
				}
			}
		}
	}
	if _, ok := s["properties"]; ok {
		return true
	}
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		raw, ok := s[k]
		if !ok {
			continue
		}
		list, ok := raw.([]any)
		if !ok {
			continue
		}
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok || m == nil {
				continue
			}
			if schemaExpectsObject(m) {
				return true
			}
		}
	}
	return false
}

func wrapStringAsObject(schema map[string]any, value string) map[string]any {
	propsRaw, ok := schema["properties"]
	if !ok {
		return map[string]any{"name": value}
	}
	props, ok := propsRaw.(map[string]any)
	if !ok || props == nil {
		return map[string]any{"name": value}
	}

	// Prefer common identifiers. This is intentionally minimal: it only wraps a string
	// into an object the MCP server can validate.
	preferredKeys := []string{"name", "id", "app", "application", "repo", "owner", "project", "slug"}
	for _, k := range preferredKeys {
		if _, ok := props[k]; ok {
			return map[string]any{k: value}
		}
	}
	return map[string]any{"name": value}
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
		strings.Contains(msg, "eof") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection closed") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "stream closed") ||
		strings.Contains(msg, "reset") ||
		errors.Is(err, context.DeadlineExceeded)
}
