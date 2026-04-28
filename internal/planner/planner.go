package planner

import (
	"sort"
	"strings"

	"gitops-agent/internal/skills"
)

type Plan struct {
	Task         string
	Selected     []skills.Skill
	SelectionLog []string
}

type Planner struct{}

func New() *Planner { return &Planner{} }

func (p *Planner) Plan(task string, all []skills.Skill) Plan {
	taskLower := strings.ToLower(task)

	skillByName := map[string]skills.Skill{}
	for _, s := range all {
		skillByName[s.Frontmatter.Name] = s
	}

	selected := make(map[string]bool)
	var log []string

	add := func(name, reason string) {
		if _, ok := skillByName[name]; !ok {
			return
		}
		if selected[name] {
			return
		}
		selected[name] = true
		log = append(log, reason)
	}

	switch {
	case strings.Contains(taskLower, "update image tag"):
		add("update-image-tag", "task indicates image tag update")
		add("validate-gitops-repo", "image changes require repo validation")
		add("create-pull-request", "all repository changes must go via PR")
	case strings.Contains(taskLower, "promote image"):
		add("promote-image-env", "task indicates environment promotion")
		add("validate-gitops-repo", "promotion requires pre-flight validation")
		add("create-pull-request", "promotion must go via PR")
	case strings.Contains(taskLower, "rollout") || strings.Contains(taskLower, "deployment"):
		add("transform-to-argo-rollout", "task indicates rollout migration")
		add("validate-gitops-repo", "migration requires repo validation")
		add("create-pull-request", "migration changes must go via PR")
	default:
		for _, s := range all {
			desc := strings.ToLower(s.Frontmatter.Description)
			if strings.Contains(desc, taskLower) || strings.Contains(taskLower, s.Frontmatter.Name) {
				add(s.Frontmatter.Name, "description match")
			}
		}
	}

	if len(selected) == 0 {
		// Fallback execution path for generic GitOps tasks.
		add("validate-gitops-repo", "fallback validation skill")
		add("create-pull-request", "fallback PR creation skill")
	}

	names := make([]string, 0, len(selected))
	for n := range selected {
		names = append(names, n)
	}
	sort.Strings(names)

	ordered := make([]skills.Skill, 0, len(names))
	for _, n := range names {
		ordered = append(ordered, skillByName[n])
	}

	// Keep natural execution order.
	reorder := []string{
		"validate-gitops-repo",
		"transform-to-argo-rollout",
		"update-image-tag",
		"promote-image-env",
		"create-pull-request",
	}
	orderIdx := map[string]int{}
	for i, n := range reorder {
		orderIdx[n] = i
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		ii, okI := orderIdx[ordered[i].Frontmatter.Name]
		jj, okJ := orderIdx[ordered[j].Frontmatter.Name]
		if okI && okJ {
			return ii < jj
		}
		if okI {
			return true
		}
		if okJ {
			return false
		}
		return ordered[i].Frontmatter.Name < ordered[j].Frontmatter.Name
	})

	return Plan{
		Task:         task,
		Selected:     ordered,
		SelectionLog: log,
	}
}
