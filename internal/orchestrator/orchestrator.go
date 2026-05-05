package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gitops-agent/internal/executor"
	"gitops-agent/internal/planner"
	"gitops-agent/internal/prurl"
	"gitops-agent/internal/skills"
)

type FieldType string

const (
	FieldTypeString FieldType = "string"
	FieldTypeSelect FieldType = "select"
	FieldTypeBool   FieldType = "boolean"
	FieldTypeNumber FieldType = "number"
)

type FormField struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required,omitempty"`
	Options     []string `json:"options,omitempty"`
	Description string   `json:"description,omitempty"`
}

type TaskForm struct {
	Task           string         `json:"task"`
	SelectedSkills []string       `json:"selected_skills"`
	Fields         []FormField    `json:"fields"`
	JSONSchema     map[string]any `json:"json_schema"`
	SelectionLog   []string       `json:"selection_log,omitempty"`
}

type ProgressEvent struct {
	At      time.Time `json:"at"`
	Stage   string    `json:"stage"`
	Status  string    `json:"status"`
	Message string    `json:"message"`
}

type Output struct {
	PRLink                 string          `json:"pr_link,omitempty"`
	EnterpriseSearchHint   string          `json:"enterprise_search_hint,omitempty"`
	DeploymentInstructions []string        `json:"deployment_instructions"`
	ArgoCDSyncSteps        []string        `json:"argocd_sync_steps"`
	Progress               []ProgressEvent `json:"progress"`
}

type RunResponse struct {
	Task   string          `json:"task"`
	Status string          `json:"status"`
	Output Output          `json:"output"`
	Result executor.Result `json:"execution_result"`
}

type executionAgent interface {
	Run(ctx context.Context, req executor.Request) (executor.Result, error)
}

type Orchestrator struct {
	executor  executionAgent
	planner   *planner.Planner
	skills    []skills.Skill
	knowledge executor.KnowledgeSearcher
}

func New(exec executionAgent, p *planner.Planner, loaded []skills.Skill, knowledge executor.KnowledgeSearcher) *Orchestrator {
	if p == nil {
		p = planner.New()
	}
	return &Orchestrator{
		executor:  exec,
		planner:   p,
		skills:    loaded,
		knowledge: knowledge,
	}
}

func (o *Orchestrator) GenerateForm(task string) (TaskForm, error) {
	task = strings.TrimSpace(task)
	if task == "" {
		return TaskForm{}, fmt.Errorf("task is required")
	}

	plan := o.planByMetadata(task)
	if len(plan.Selected) == 0 {
		plan = o.planner.Plan(task, o.skills)
	}
	if len(plan.Selected) == 0 {
		return TaskForm{}, fmt.Errorf("no matching skills found for task")
	}

	fields := collectFields(task, plan.Selected)
	return TaskForm{
		Task:           titleCaseTask(task),
		SelectedSkills: skillNames(plan.Selected),
		Fields:         fields,
		JSONSchema:     buildJSONSchema(fields),
		SelectionLog:   plan.SelectionLog,
	}, nil
}

func (o *Orchestrator) SubmitAndRun(ctx context.Context, task string, inputs map[string]any) (RunResponse, error) {
	if o.executor == nil {
		return RunResponse{}, fmt.Errorf("execution agent is not configured")
	}
	form, err := o.GenerateForm(task)
	if err != nil {
		return RunResponse{}, err
	}
	progress := []ProgressEvent{
		progressEvent("skill_mapping", "completed", fmt.Sprintf("Selected %d skill(s)", len(form.SelectedSkills))),
	}
	if err := validateInputs(form.Fields, inputs); err != nil {
		progress = append(progress, progressEvent("input_validation", "failed", err.Error()))
		return RunResponse{
			Task:   form.Task,
			Status: "failed",
			Output: Output{
				DeploymentInstructions: nil,
				ArgoCDSyncSteps:        nil,
				Progress:               progress,
			},
		}, err
	}
	progress = append(progress, progressEvent("input_validation", "completed", "Inputs validated"))
	progress = append(progress, progressEvent("execution_delegate", "in_progress", "Delegating task to execution agent"))

	res, err := o.executor.Run(ctx, executor.Request{
		Task:   task,
		Inputs: inputs,
	})
	if err != nil {
		progress = append(progress, progressEvent("execution_delegate", "failed", err.Error()))
		hint := o.orgHintOnFailure(ctx, task, "execution failed: "+err.Error())
		if hint != "" {
			progress = append(progress, progressEvent("enterprise_search", "completed", truncateProgressMessage(hint, 600)))
		}
		return RunResponse{
			Task:   form.Task,
			Status: "failed",
			Output: Output{
				PRLink:                 extractPRLink(res, inputs),
				EnterpriseSearchHint:   hint,
				DeploymentInstructions: deploymentInstructions(task, inputs, false),
				ArgoCDSyncSteps:        argocdSyncSteps(inputs),
				Progress:               progress,
			},
			Result: res,
		}, err
	}
	progress = append(progress, progressEvent("execution_delegate", "completed", "Execution agent finished"))
	prLink := extractPRLink(res, inputs)
	if expectsPullRequest(res) && strings.TrimSpace(prLink) == "" {
		progress = append(progress, progressEvent("pr_creation", "failed", "Execution finished but no pull request link was detected"))
		err := fmt.Errorf("execution completed but no pull request link detected; PR was not created")
		hint := o.orgHintOnFailure(ctx, task, "pull request not detected after successful steps; ensure Git MCP created a PR and returned a URL in tool output")
		if hint != "" {
			progress = append(progress, progressEvent("enterprise_search", "completed", truncateProgressMessage(hint, 600)))
		}
		return RunResponse{
			Task:   form.Task,
			Status: "failed",
			Output: Output{
				PRLink:                 "",
				EnterpriseSearchHint:   hint,
				DeploymentInstructions: deploymentInstructions(task, inputs, false),
				ArgoCDSyncSteps:        argocdSyncSteps(inputs),
				Progress:               progress,
			},
			Result: res,
		}, err
	}

	return RunResponse{
		Task:   form.Task,
		Status: "success",
		Output: Output{
			PRLink:                 prLink,
			DeploymentInstructions: deploymentInstructions(task, inputs, true),
			ArgoCDSyncSteps:        argocdSyncSteps(inputs),
			Progress:               progress,
		},
		Result: res,
	}, nil
}

func (o *Orchestrator) planByMetadata(task string) planner.Plan {
	tokens := tokenize(task)
	if len(tokens) == 0 {
		return planner.Plan{Task: task}
	}

	type scored struct {
		skill skills.Skill
		score int
		log   []string
	}
	var scoredSkills []scored
	for _, s := range o.skills {
		score := 0
		var why []string

		nameTokens := tokenize(strings.ReplaceAll(s.Frontmatter.Name, "-", " "))
		if overlapCount(tokens, nameTokens) > 0 {
			score += 4
			why = append(why, "skill name overlap")
		}

		descTokens := tokenize(s.Frontmatter.Description)
		overlapDesc := overlapCount(tokens, descTokens)
		if overlapDesc > 0 {
			score += overlapDesc
			why = append(why, "description overlap")
		}

		for k, v := range s.Frontmatter.Metadata {
			metaTokens := tokenize(k + " " + v)
			overlapMeta := overlapCount(tokens, metaTokens)
			if overlapMeta > 0 {
				score += overlapMeta * 3
				why = append(why, fmt.Sprintf("metadata match: %s", k))
			}
		}

		if score > 0 {
			scoredSkills = append(scoredSkills, scored{
				skill: s,
				score: score,
				log:   why,
			})
		}
	}

	sort.Slice(scoredSkills, func(i, j int) bool {
		if scoredSkills[i].score == scoredSkills[j].score {
			return scoredSkills[i].skill.Frontmatter.Name < scoredSkills[j].skill.Frontmatter.Name
		}
		return scoredSkills[i].score > scoredSkills[j].score
	})

	maxScore := 0
	if len(scoredSkills) > 0 {
		maxScore = scoredSkills[0].score
	}

	var selected []skills.Skill
	var selectionLog []string
	for i, ss := range scoredSkills {
		if i >= 3 {
			break
		}
		// Keep strongly relevant matches only.
		if ss.score+2 < maxScore {
			continue
		}
		selected = append(selected, ss.skill)
		selectionLog = append(selectionLog, fmt.Sprintf("%s (score=%d): %s", ss.skill.Frontmatter.Name, ss.score, strings.Join(ss.log, ", ")))
	}
	return planner.Plan{
		Task:         task,
		Selected:     selected,
		SelectionLog: selectionLog,
	}
}

func collectFields(task string, selected []skills.Skill) []FormField {
	seen := map[string]bool{}
	var fields []FormField
	for _, s := range selected {
		for _, in := range s.Inputs {
			name := strings.TrimSpace(in.Name)
			if name == "" || seen[name] {
				continue
			}
			field := inferField(task, in)
			fields = append(fields, field)
			seen[name] = true
		}
	}
	return fields
}

func inferField(task string, in skills.InputSpec) FormField {
	descLower := strings.ToLower(in.Description)
	nameLower := strings.ToLower(in.Name)
	required := in.Required && !strings.Contains(descLower, "optional")

	field := FormField{
		Name:        in.Name,
		Type:        string(FieldTypeString),
		Required:    required,
		Description: in.Description,
	}

	switch {
	case strings.Contains(nameLower, "strategy"):
		field.Type = string(FieldTypeSelect)
		field.Options = strategyOptions(task, in.Description)
	case strings.Contains(nameLower, "enable"), strings.Contains(nameLower, "flag"), strings.Contains(descLower, "true"), strings.Contains(descLower, "false"):
		field.Type = string(FieldTypeBool)
	case strings.Contains(nameLower, "count"), strings.Contains(nameLower, "replica"), strings.Contains(nameLower, "timeout"), strings.Contains(nameLower, "percentage"):
		field.Type = string(FieldTypeNumber)
	default:
		options := extractBacktickOptions(in.Description)
		if len(options) > 1 {
			field.Type = string(FieldTypeSelect)
			field.Options = options
		}
	}

	return field
}

func strategyOptions(task, description string) []string {
	taskLower := strings.ToLower(task)
	if strings.Contains(taskLower, "blue-green") || strings.Contains(taskLower, "blue green") {
		return []string{"blueGreen"}
	}
	if options := extractBacktickOptions(description); len(options) > 0 {
		return options
	}
	return []string{"blueGreen", "canary", "rolling"}
}

func buildJSONSchema(fields []FormField) map[string]any {
	properties := map[string]any{}
	var required []string

	for _, f := range fields {
		prop := map[string]any{
			"title": f.Name,
		}
		switch f.Type {
		case string(FieldTypeSelect):
			prop["type"] = "string"
			prop["enum"] = f.Options
		case string(FieldTypeBool):
			prop["type"] = "boolean"
		case string(FieldTypeNumber):
			prop["type"] = "number"
		default:
			prop["type"] = "string"
		}
		if f.Description != "" {
			prop["description"] = f.Description
		}
		properties[f.Name] = prop
		if f.Required {
			required = append(required, f.Name)
		}
	}

	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func validateInputs(fields []FormField, inputs map[string]any) error {
	for _, field := range fields {
		v, exists := inputs[field.Name]
		if field.Required && (!exists || isBlank(v)) {
			return fmt.Errorf("missing required input: %s", field.Name)
		}
		if !exists {
			continue
		}
		switch field.Type {
		case string(FieldTypeBool):
			switch v.(type) {
			case bool:
			default:
				return fmt.Errorf("input %s must be boolean", field.Name)
			}
		case string(FieldTypeNumber):
			switch v.(type) {
			case int, int32, int64, float32, float64:
			default:
				return fmt.Errorf("input %s must be numeric", field.Name)
			}
		case string(FieldTypeSelect):
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("input %s must be string", field.Name)
			}
			if len(field.Options) > 0 && !contains(field.Options, s) {
				return fmt.Errorf("input %s must be one of %v", field.Name, field.Options)
			}
		default:
			if _, ok := v.(string); !ok {
				return fmt.Errorf("input %s must be string", field.Name)
			}
		}
	}
	return nil
}

func extractPRLink(res executor.Result, inputs map[string]any) string {
	if v, ok := inputs["pr_link"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	if v, ok := inputs["pr_url"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	for _, step := range res.Steps {
		if m := prurl.First(step.Output); m != "" {
			return m
		}
	}
	return ""
}

func expectsPullRequest(res executor.Result) bool {
	for _, n := range res.Selected {
		if strings.Contains(strings.ToLower(n), "pull-request") || strings.Contains(strings.ToLower(n), "pull request") {
			return true
		}
	}
	for _, step := range res.Steps {
		s := strings.ToLower(step.Step)
		if strings.Contains(s, "pull request") || strings.Contains(s, "open pr") || strings.Contains(s, "create pr") {
			return true
		}
	}
	return false
}

func deploymentInstructions(task string, inputs map[string]any, success bool) []string {
	app := fmt.Sprintf("%v", inputs["application"])
	if app == "" || app == "<nil>" {
		app = fmt.Sprintf("%v", inputs["app_path"])
	}

	ins := []string{
		"Review generated pull request changes and get required approvals.",
		"Merge the pull request only after CI and policy checks pass.",
	}
	if strings.Contains(strings.ToLower(task), "blue-green") {
		ins = append(ins, "Verify Service selectors route traffic to the blue-green active color.")
	}
	if app != "" && app != "<nil>" {
		ins = append(ins, fmt.Sprintf("Validate rollout health for application scope: %s.", app))
	}
	if success {
		ins = append(ins, "Record the deployment result in change management notes.")
	} else {
		ins = append(ins, "Do not proceed to sync until blockers are resolved.")
	}
	return ins
}

func argocdSyncSteps(inputs map[string]any) []string {
	app := fmt.Sprintf("%v", inputs["application"])
	if strings.TrimSpace(app) == "" || app == "<nil>" {
		app = fmt.Sprintf("%v", inputs["app_path"])
	}
	if strings.TrimSpace(app) == "" || app == "<nil>" {
		app = "<application-name>"
	}
	return []string{
		fmt.Sprintf("argocd app get %s", app),
		fmt.Sprintf("argocd app sync %s", app),
		fmt.Sprintf("argocd app wait %s --health --operation --timeout 600", app),
	}
}

const maxOrchestratorHintLen = 8000

func truncateProgressMessage(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func (o *Orchestrator) orgHintOnFailure(ctx context.Context, task string, detail string) string {
	if o == nil || o.knowledge == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	q := fmt.Sprintf("GitOps orchestration issue. Task: %s. Context: %s", strings.TrimSpace(task), detail)
	s, err := o.knowledge.Search(ctx, q)
	if err != nil || strings.TrimSpace(s) == "" {
		return ""
	}
	s = strings.TrimSpace(s)
	if len(s) > maxOrchestratorHintLen {
		s = s[:maxOrchestratorHintLen] + "…"
	}
	return s
}

func progressEvent(stage, status, message string) ProgressEvent {
	return ProgressEvent{
		At:      time.Now().UTC(),
		Stage:   stage,
		Status:  status,
		Message: message,
	}
}

func overlapCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := map[string]bool{}
	for _, token := range b {
		set[token] = true
	}
	count := 0
	for _, token := range a {
		if set[token] {
			count++
		}
	}
	return count
}

func tokenize(s string) []string {
	s = strings.ToLower(s)
	out := make([]string, 0, 16)
	var b strings.Builder
	flush := func() {
		if b.Len() >= 3 {
			out = append(out, b.String())
		}
		b.Reset()
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func extractBacktickOptions(desc string) []string {
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(desc, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if len(m) != 2 {
			continue
		}
		v := strings.TrimSpace(m[1])
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func contains(values []string, target string) bool {
	for _, v := range values {
		if strings.EqualFold(v, target) {
			return true
		}
	}
	return false
}

func isBlank(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

func skillNames(items []skills.Skill) []string {
	names := make([]string, 0, len(items))
	for _, s := range items {
		names = append(names, s.Frontmatter.Name)
	}
	return names
}

func titleCaseTask(task string) string {
	words := strings.Fields(strings.TrimSpace(task))
	for i, w := range words {
		parts := strings.Split(w, "-")
		for j, p := range parts {
			if p == "" {
				continue
			}
			parts[j] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
		words[i] = strings.Join(parts, "-")
	}
	return strings.Join(words, " ")
}
