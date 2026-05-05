package planner

import (
	"sort"
	"strings"
	"unicode"

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
	tokens := tokenize(task)
	if len(tokens) == 0 {
		return Plan{Task: task}
	}

	type scored struct {
		skill skills.Skill
		score int
		why   []string
	}

	var scoredSkills []scored
	for _, s := range all {
		score := 0
		var why []string

		nameTokens := tokenize(strings.ReplaceAll(s.Frontmatter.Name, "-", " "))
		if overlapCount(tokens, nameTokens) > 0 {
			score += 4
			why = append(why, "skill name overlap")
		}

		descTokens := tokenize(s.Frontmatter.Description)
		descOverlap := overlapCount(tokens, descTokens)
		if descOverlap > 0 {
			score += descOverlap * 2
			why = append(why, "description overlap")
		}

		for k, v := range s.Frontmatter.Metadata {
			metaTokens := tokenize(k + " " + v)
			metaOverlap := overlapCount(tokens, metaTokens)
			if metaOverlap > 0 {
				score += metaOverlap * 3
				why = append(why, "metadata overlap")
			}
		}

		bodyTokens := tokenize(s.Body)
		bodyOverlap := overlapCount(tokens, bodyTokens)
		if bodyOverlap > 0 {
			score += bodyOverlap
			why = append(why, "body overlap")
		}

		if score > 0 {
			scoredSkills = append(scoredSkills, scored{skill: s, score: score, why: why})
		}
	}

	sort.Slice(scoredSkills, func(i, j int) bool {
		if scoredSkills[i].score == scoredSkills[j].score {
			return scoredSkills[i].skill.Frontmatter.Name < scoredSkills[j].skill.Frontmatter.Name
		}
		return scoredSkills[i].score > scoredSkills[j].score
	})

	if len(scoredSkills) == 0 {
		return Plan{Task: task}
	}

	maxScore := scoredSkills[0].score
	var selected []skills.Skill
	var selectionLog []string
	for i, ss := range scoredSkills {
		if i >= 3 {
			break
		}
		if ss.score+2 < maxScore {
			continue
		}
		selected = append(selected, ss.skill)
		selectionLog = append(selectionLog, ss.skill.Frontmatter.Name)
	}

	return Plan{
		Task:         task,
		Selected:     selected,
		SelectionLog: selectionLog,
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
