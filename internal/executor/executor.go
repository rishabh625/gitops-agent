package executor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gitops-agent/internal/mcpadapter"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/skills"
)

type Request struct {
	Task   string
	Inputs map[string]any
}

type StepResult struct {
	Skill   string `json:"skill"`
	Step    string `json:"step"`
	Server  string `json:"server"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
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
	log      *slog.Logger
	planner  *planner.Planner
	adapter  *mcpadapter.Adapter
	allSkill []skills.Skill
}

func New(log *slog.Logger, p *planner.Planner, a *mcpadapter.Adapter, loaded []skills.Skill) *Executor {
	if log == nil {
		log = slog.Default()
	}
	return &Executor{
		log:      log,
		planner:  p,
		adapter:  a,
		allSkill: loaded,
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
		for _, step := range sk.Steps {
			server, intent := mapStepToTool(step)
			stepRes := StepResult{
				Skill:  sk.Frontmatter.Name,
				Step:   step,
				Server: string(server),
			}

			args := cloneMap(req.Inputs)
			args["task"] = req.Task
			args["skill"] = sk.Frontmatter.Name
			args["step"] = step
			args["intent"] = intent

			out, err := e.adapter.CallByIntent(ctx, server, intent, args)
			if err != nil {
				stepRes.Success = false
				stepRes.Error = err.Error()
				r.Steps = append(r.Steps, stepRes)
				r.CompletedAt = time.Now().UTC()
				return r, fmt.Errorf("step failed skill=%s step=%q: %w", sk.Frontmatter.Name, step, err)
			}
			stepRes.Success = true
			stepRes.Output = out
			r.Steps = append(r.Steps, stepRes)
		}
	}
	r.CompletedAt = time.Now().UTC()
	return r, nil
}

func mapStepToTool(step string) (mcpadapter.ServerName, string) {
	s := strings.ToLower(step)
	switch {
	case strings.Contains(s, "pull request"), strings.Contains(s, "branch"), strings.Contains(s, "commit"), strings.Contains(s, "repo"):
		return mcpadapter.ServerGit, "create pull request branch commit repository"
	case strings.Contains(s, "argo"), strings.Contains(s, "rollout"), strings.Contains(s, "sync"), strings.Contains(s, "application"):
		return mcpadapter.ServerArgo, "argo rollout health sync application"
	case strings.Contains(s, "kubernetes"), strings.Contains(s, "cluster"), strings.Contains(s, "namespace"), strings.Contains(s, "pod"):
		return mcpadapter.ServerK8s, "kubernetes workload pod namespace state"
	default:
		return mcpadapter.ServerGit, "repository inspect change"
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}
