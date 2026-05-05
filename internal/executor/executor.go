package executor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"gitops-agent/internal/mcpadapter"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/prurl"
	"gitops-agent/internal/skills"
)

type Request struct {
	Task   string
	Inputs map[string]any
}

// KnowledgeSearcher is optional: when a step fails, the executor can query an org
// knowledge base (e.g. Vertex AI Search / Gemini Enterprise app) for recovery guidance.
type KnowledgeSearcher interface {
	Search(ctx context.Context, query string) (string, error)
}

type StepResult struct {
	Skill                string `json:"skill"`
	Step                 string `json:"step"`
	Server               string `json:"server"`
	Output               string `json:"output"`
	Success              bool   `json:"success"`
	Error                string `json:"error,omitempty"`
	EnterpriseSearchHint string `json:"enterprise_search_hint,omitempty"`
}

type Result struct {
	Task         string       `json:"task"`
	Selected     []string     `json:"selected_skills"`
	SelectionLog []string     `json:"selection_log"`
	StartedAt    time.Time    `json:"started_at"`
	CompletedAt  time.Time    `json:"completed_at"`
	Steps        []StepResult `json:"steps"`
}

type Executor struct {
	log       *slog.Logger
	planner   *planner.Planner
	adapter   *mcpadapter.Adapter
	allSkill  []skills.Skill
	knowledge KnowledgeSearcher
}

func New(log *slog.Logger, p *planner.Planner, a *mcpadapter.Adapter, loaded []skills.Skill, knowledge KnowledgeSearcher) *Executor {
	if log == nil {
		log = slog.Default()
	}
	return &Executor{
		log:       log,
		planner:   p,
		adapter:   a,
		allSkill:  loaded,
		knowledge: knowledge,
	}
}

func (e *Executor) Run(ctx context.Context, req Request) (Result, error) {
	plan := e.planner.Plan(req.Task, e.allSkill)
	r := Result{
		Task:         req.Task,
		SelectionLog: plan.SelectionLog,
		StartedAt:    time.Now().UTC(),
	}
	for _, s := range plan.Selected {
		r.Selected = append(r.Selected, s.Frontmatter.Name)
	}

	for _, sk := range plan.Selected {
		e.log.Info("executing skill", "skill", sk.Frontmatter.Name, "steps", len(sk.Steps))
		createdBranch := ""
		for _, step := range sk.Steps {
			server, intent := mapStepToTool(step)
			stepRes := StepResult{
				Skill:  sk.Frontmatter.Name,
				Step:   step,
				Server: string(server),
			}

			args := cloneMap(req.Inputs)

			if server == mcpadapter.ServerGit && e.knowledge != nil && e.gitFallbackEnabled() && e.gitPreferEnterpriseCreate() && isGitCreateIntent(intent) {
				recovered, recoverOut := e.tryEnterpriseCreatePullRequest(ctx, req, sk.Frontmatter.Name, step, intent, args)
				if recovered {
					e.log.Info("git create handled via Gemini Enterprise before Git MCP", "skill", sk.Frontmatter.Name, "step", step, "intent", intent)
					stepRes.Success = true
					stepRes.Output = recoverOut
					r.Steps = append(r.Steps, stepRes)
					continue
				}
				if e.gitCreateOnly() && strings.Contains(strings.ToLower(intent), "create pull request") {
					stepRes.Success = false
					stepRes.Error = "Git create is configured to use Gemini Enterprise only, but no PR/MR URL was returned"
					r.Steps = append(r.Steps, stepRes)
					r.CompletedAt = time.Now().UTC()
					return r, fmt.Errorf("create pull request failed via enterprise-only mode")
				}
			}

			out, err := e.adapter.CallByIntent(ctx, server, intent, args)
			if err != nil {
				if server == mcpadapter.ServerGit && strings.Contains(strings.ToLower(intent), "create branch") {
					msg := strings.ToLower(err.Error())
					if strings.Contains(msg, "already exists") ||
						strings.Contains(msg, "already exist") ||
						strings.Contains(msg, "reference already exists") ||
						(strings.Contains(msg, "branch") && strings.Contains(msg, "exists")) {
						stepRes.Success = true
						stepRes.Output = err.Error()
						r.Steps = append(r.Steps, stepRes)
						if createdBranch == "" {
							createdBranch = firstNonEmptyString(args, "change_branch", "branch", "branch_name")
						}
						continue
					}
				}

				if server == mcpadapter.ServerGit &&
					strings.Contains(strings.ToLower(intent), "create pull request") &&
					(strings.Contains(strings.ToLower(err.Error()), "already exists") ||
						strings.Contains(strings.ToLower(err.Error()), "already exist") ||
						strings.Contains(strings.ToLower(err.Error()), "pull request already exists") ||
						strings.Contains(strings.ToLower(err.Error()), "pr already exists") ||
						strings.Contains(strings.ToLower(err.Error()), "422")) {
					findArgs := cloneMap(args)
					if createdBranch != "" {
						findArgs["change_branch"] = createdBranch
						findArgs["branch"] = createdBranch
						findArgs["branch_name"] = createdBranch
					}
					if text, findErr := e.adapter.CallByIntent(ctx, mcpadapter.ServerGit, "list pull requests list merge requests", findArgs); findErr == nil {
						if link := prurl.First(text); link != "" {
							stepRes.Success = true
							stepRes.Output = fmt.Sprintf("PR already existed; detected existing PR link.\n\nOriginal error: %s\n\nExisting PR:\n%s", err.Error(), link)
							r.Steps = append(r.Steps, stepRes)
							continue
						}
					}
				}

				var enterpriseHint string
				if server == mcpadapter.ServerGit && e.knowledge != nil {
					if e.gitFallbackEnabled() {
						recovered, recoverOut, hint := e.tryEnterpriseGitRecovery(ctx, req, sk.Frontmatter.Name, step, intent, args, err)
						if recovered {
							e.log.Info("git step recovered via Gemini Enterprise search after MCP failure", "skill", sk.Frontmatter.Name, "step", step)
							stepRes.Success = true
							stepRes.Output = recoverOut
							r.Steps = append(r.Steps, stepRes)
							if strings.Contains(strings.ToLower(intent), "create branch") && createdBranch == "" {
								createdBranch = firstNonEmptyString(args, "change_branch", "branch", "branch_name")
							}
							continue
						}
						enterpriseHint = hint
					} else {
						enterpriseHint = e.lookupEnterpriseHint(ctx, req.Task, sk.Frontmatter.Name, step, string(server), err)
					}
				} else if e.knowledge != nil {
					enterpriseHint = e.lookupEnterpriseHint(ctx, req.Task, sk.Frontmatter.Name, step, string(server), err)
				}

				stepRes.Success = false
				stepRes.Error = err.Error()
				if enterpriseHint != "" {
					stepRes.EnterpriseSearchHint = enterpriseHint
				}
				r.Steps = append(r.Steps, stepRes)
				r.CompletedAt = time.Now().UTC()

				if server == mcpadapter.ServerGit && strings.Contains(strings.ToLower(intent), "create pull request") {
					if createdBranch == "" {
						createdBranch = firstNonEmptyString(args, "change_branch", "branch", "branch_name")
					}
					if strings.TrimSpace(createdBranch) != "" {
						cleanupArgs := cloneMap(args)
						cleanupArgs["branch"] = createdBranch
						cleanupArgs["branch_name"] = createdBranch
						cleanupArgs["change_branch"] = createdBranch
						cleanupArgs["ref"] = "refs/heads/" + createdBranch
						_, cleanupErr := e.adapter.CallByIntent(ctx, mcpadapter.ServerGit, "delete branch delete ref", cleanupArgs)
						if cleanupErr != nil {
							e.log.Warn("failed to cleanup branch after PR failure", "branch", createdBranch, "error", cleanupErr)
						} else {
							e.log.Info("cleaned up branch after PR failure", "branch", createdBranch)
						}
					}
				}
				return r, fmt.Errorf("step failed skill=%s step=%q: %w", sk.Frontmatter.Name, step, err)
			}
			stepRes.Success = true
			stepRes.Output = out
			r.Steps = append(r.Steps, stepRes)
			if server == mcpadapter.ServerGit && strings.Contains(strings.ToLower(intent), "create branch") && createdBranch == "" {
				createdBranch = firstNonEmptyString(args, "change_branch", "branch", "branch_name")
			}
		}
	}
	r.CompletedAt = time.Now().UTC()
	return r, nil
}

const maxEnterpriseHintLen = 8000

func (e *Executor) gitPreferEnterpriseCreate() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_GIT_PREFER_CREATE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (e *Executor) gitCreateOnly() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_GIT_CREATE_ONLY")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func isGitCreateIntent(intent string) bool {
	s := strings.ToLower(strings.TrimSpace(intent))
	return strings.Contains(s, "create pull request") ||
		strings.Contains(s, "create pr") ||
		strings.Contains(s, "open pr") ||
		strings.Contains(s, "create merge request") ||
		strings.Contains(s, "create mr") ||
		strings.Contains(s, "open pull request") ||
		strings.Contains(s, "open merge request")
}

func (e *Executor) tryEnterpriseCreatePullRequest(ctx context.Context, req Request, skillName, step, intent string, args map[string]any) (recovered bool, output string) {
	if e == nil || e.knowledge == nil {
		return false, ""
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	q := buildGitPreferredCreateQuery(req.Task, skillName, step, intent, args)
	text, err := e.knowledge.Search(ctx, q)
	if err != nil {
		e.log.Warn("enterprise create PR search failed", "error", err)
		return false, ""
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, ""
	}
	link := prurl.First(text)
	if link == "" {
		return false, ""
	}
	out := fmt.Sprintf("Gemini Enterprise app created or identified the pull/merge request.\n\nEnterprise response:\n%s", text)
	if len(out) > maxEnterpriseHintLen {
		out = out[:maxEnterpriseHintLen] + "…"
	}
	return true, out
}

func buildGitPreferredCreateQuery(task, skillName, step, intent string, args map[string]any) string {
	argSummary := summarizeArgsForRecovery(args)
	return fmt.Sprintf(
		"Create or open the pull request for the prepared GitOps change. If the source branch already exists, reuse it and still open the PR. "+
			"Reply with the canonical HTTPS URL to the pull request or merge request.\n\n"+
			"Task: %s\nSkill: %s\nStep: %s\nIntent: %s\nRepository inputs: %s",
		strings.TrimSpace(task), skillName, step, intent, argSummary,
	)
}

func (e *Executor) lookupEnterpriseHint(ctx context.Context, task, skill, step, server string, err error) string {
	if e == nil || e.knowledge == nil || err == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	q := fmt.Sprintf("GitOps automation step failed. Task: %s. Skill: %s. Step: %s. MCP server: %s. Error: %s",
		strings.TrimSpace(task), skill, step, server, err.Error())
	hint, hErr := e.knowledge.Search(ctx, q)
	if hErr != nil {
		e.log.Warn("enterprise search hint failed", "error", hErr)
		return ""
	}
	hint = strings.TrimSpace(hint)
	if len(hint) > maxEnterpriseHintLen {
		hint = hint[:maxEnterpriseHintLen] + "…"
	}
	return hint
}

func (e *Executor) gitFallbackEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DISCOVERY_ENGINE_GIT_FALLBACK")))
	if v == "0" || v == "false" || v == "no" {
		return false
	}
	if v == "" {
		return true
	}
	return v == "1" || v == "true" || v == "yes"
}

func (e *Executor) tryEnterpriseGitRecovery(ctx context.Context, req Request, skillName, step, intent string, args map[string]any, mcpErr error) (recovered bool, output string, hint string) {
	if e == nil || e.knowledge == nil || mcpErr == nil {
		return false, "", ""
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	q := buildGitRecoveryQuery(req.Task, skillName, step, intent, args, mcpErr)
	text, err := e.knowledge.Search(ctx, q)
	if err != nil {
		e.log.Warn("enterprise git recovery search failed", "error", err)
		return false, "", ""
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return false, "", ""
	}
	if prurl.First(text) == "" {
		h := text
		if len(h) > maxEnterpriseHintLen {
			h = h[:maxEnterpriseHintLen] + "…"
		}
		return false, "", h
	}
	out := fmt.Sprintf("Git MCP failed; Gemini Enterprise app completed the workflow (PR/MR link in response).\n\nMCP error: %s\n\nEnterprise response:\n%s",
		mcpErr.Error(), text)
	if len(out) > maxEnterpriseHintLen {
		out = out[:maxEnterpriseHintLen] + "…"
	}
	return true, out, ""
}

func buildGitRecoveryQuery(task, skillName, step, intent string, args map[string]any, mcpErr error) string {
	argSummary := summarizeArgsForRecovery(args)
	return fmt.Sprintf(
		"The Git MCP automation step failed. Perform the same Git operation using your integrated Git capabilities (create branch, commit, open pull/merge request, comment on PR, etc.). "+
			"When finished, reply with a clear summary and include the canonical HTTPS URL to the pull request or merge request.\n\n"+
			"Task: %s\nSkill: %s\nStep: %s\nMCP intent used: %s\nGit MCP error: %s\nRepository inputs: %s",
		strings.TrimSpace(task), skillName, step, intent, mcpErr.Error(), argSummary,
	)
}

func summarizeArgsForRecovery(m map[string]any) string {
	priority := []string{
		"repo_url", "repo", "repository", "git_url", "remote_url",
		"base_branch", "change_branch", "branch", "branch_name",
		"title", "pr_title", "body", "description",
		"application", "manifest_mode", "risk_level",
	}
	seen := map[string]bool{}
	var parts []string
	for _, k := range priority {
		v, ok := m[k]
		if !ok {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if strings.TrimSpace(s) == "" || s == "<nil>" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, truncateArgVal(s, 600)))
		seen[k] = true
	}
	for k, v := range m {
		if seen[k] {
			continue
		}
		s := fmt.Sprintf("%v", v)
		if strings.TrimSpace(s) == "" || s == "<nil>" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%s", k, truncateArgVal(s, 400)))
		if argsSummaryLen(parts) > 3800 {
			break
		}
	}
	out := strings.Join(parts, "; ")
	if len(out) > 4000 {
		out = out[:4000] + "…"
	}
	return out
}

func argsSummaryLen(parts []string) int {
	n := 0
	for _, p := range parts {
		n += len(p) + 2
	}
	return n
}

func truncateArgVal(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func firstNonEmptyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func mapStepToTool(step string) (mcpadapter.ServerName, string) {
	s := strings.ToLower(step)
	switch {
	case strings.Contains(s, "validate"), strings.Contains(s, "verify"), strings.Contains(s, "inspect"), strings.Contains(s, "renderable"):
		return mcpadapter.ServerGit, "inspect repository tree list files read file helm chart values kustomize kustomization validate"
	case strings.Contains(s, "update"), strings.Contains(s, "edit"), strings.Contains(s, "modify"), strings.Contains(s, "patch"):
		return mcpadapter.ServerGit, "update file edit modify patch write manifests values yaml kustomize helm"
	case strings.Contains(s, "create repository"), strings.Contains(s, "new repository"), strings.Contains(s, "init repository"):
		return mcpadapter.ServerGit, "create repository"
	case strings.Contains(s, "pull request"), strings.Contains(s, "open pr"), strings.Contains(s, "create pr"):
		return mcpadapter.ServerGit, "create pull request"
	case strings.Contains(s, "branch"):
		return mcpadapter.ServerGit, "create branch"
	case strings.Contains(s, "commit"):
		return mcpadapter.ServerGit, "create commit"
	case strings.Contains(s, "repo"), strings.Contains(s, "repository"):
		return mcpadapter.ServerGit, "inspect repository"
	case strings.Contains(s, "argo"), strings.Contains(s, "rollout"), strings.Contains(s, "sync"), strings.Contains(s, "application"):
		return mcpadapter.ServerArgo, "argo rollout health sync application"
	case strings.Contains(s, "kubernetes"), strings.Contains(s, "cluster"), strings.Contains(s, "namespace"), strings.Contains(s, "pod"):
		return mcpadapter.ServerK8s, "kubernetes workload pod namespace state"
	default:
		return mcpadapter.ServerGit, "inspect repository tree list files read file diff change"
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}
