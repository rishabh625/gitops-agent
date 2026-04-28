package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SourceType string

const (
	SourceLocal  SourceType = "local"
	SourceGitHub SourceType = "github"
)

type LoaderConfig struct {
	Source        SourceType
	LocalRoot     string
	GitHubOwner   string
	GitHubRepo    string
	GitHubRef     string
	GitHubPath    string
	GitHubToken   string
	HTTPTimeoutMs int
}

type Loader struct {
	cfg    LoaderConfig
	client *http.Client
	log    *slog.Logger
}

func NewLoader(cfg LoaderConfig, logger *slog.Logger) *Loader {
	timeout := 10000
	if cfg.HTTPTimeoutMs > 0 {
		timeout = cfg.HTTPTimeoutMs
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Loader{
		cfg:    cfg,
		client: &http.Client{Timeout: time.Duration(timeout) * time.Millisecond},
		log:    logger,
	}
}

func (l *Loader) LoadAll(ctx context.Context) ([]Skill, error) {
	switch l.cfg.Source {
	case SourceLocal:
		return l.loadAllLocal()
	case SourceGitHub:
		return l.loadAllGitHub(ctx)
	default:
		return nil, fmt.Errorf("unknown skill source %q", l.cfg.Source)
	}
}

func (l *Loader) loadAllLocal() ([]Skill, error) {
	root := l.cfg.LocalRoot
	if root == "" {
		root = "gitops-skills"
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read local skills root: %w", err)
	}

	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirName := e.Name()
		p := filepath.Join(root, dirName, "SKILL.md")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s, err := ParseSkill(p, dirName, string(b))
		if err != nil {
			return nil, err
		}
		if err := ValidateSkill(s); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Frontmatter.Name < skills[j].Frontmatter.Name })
	return skills, nil
}

type githubContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	DownloadURL string `json:"download_url"`
}

func (l *Loader) loadAllGitHub(ctx context.Context) ([]Skill, error) {
	owner := l.cfg.GitHubOwner
	repo := l.cfg.GitHubRepo
	ref := l.cfg.GitHubRef
	path := l.cfg.GitHubPath
	if path == "" {
		path = "gitops-skills"
	}
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("github owner/repo must be set")
	}
	if ref == "" {
		ref = "main"
	}

	api := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(path),
		url.QueryEscape(ref),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return nil, err
	}
	if l.cfg.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+l.cfg.GitHubToken)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github list skills: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github list skills failed: status=%d body=%s", resp.StatusCode, string(data))
	}

	var entries []githubContent
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode github contents: %w", err)
	}

	var skills []Skill
	for _, entry := range entries {
		if entry.Type != "dir" {
			continue
		}
		skillURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s/SKILL.md",
			owner, repo, ref, strings.TrimPrefix(entry.Path, "/"))
		b, err := l.fetch(ctx, skillURL)
		if err != nil {
			l.log.Warn("skip invalid skill", "dir", entry.Name, "error", err)
			continue
		}
		s, err := ParseSkill(entry.Path+"/SKILL.md", entry.Name, string(b))
		if err != nil {
			return nil, err
		}
		if err := ValidateSkill(s); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Frontmatter.Name < skills[j].Frontmatter.Name })
	return skills, nil
}

func (l *Loader) fetch(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch %s failed status=%d body=%s", rawURL, resp.StatusCode, string(data))
	}
	return io.ReadAll(resp.Body)
}
