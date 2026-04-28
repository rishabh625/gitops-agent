package skills

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var numberedStepRE = regexp.MustCompile(`^\s*\d+\.\s+`)
var inputBulletRE = regexp.MustCompile("^[-*]\\s+`([^`]+)`\\s*:\\s*(.+)$")

// ParseSkill parses a SKILL.md file with YAML frontmatter and markdown body.
func ParseSkill(path, dirName, content string) (Skill, error) {
	const sep = "---"
	if !strings.HasPrefix(content, sep) {
		return Skill{}, fmt.Errorf("skill %q missing opening frontmatter delimiter", path)
	}

	rest := strings.TrimPrefix(content, sep)
	rest = strings.TrimPrefix(rest, "\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return Skill{}, fmt.Errorf("skill %q missing closing frontmatter delimiter", path)
	}

	frontRaw := strings.TrimSpace(rest[:idx])
	body := strings.TrimSpace(strings.TrimPrefix(rest[idx+1:], "---"))

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(frontRaw), &fm); err != nil {
		return Skill{}, fmt.Errorf("unmarshal frontmatter for %q: %w", path, err)
	}

	s := Skill{
		Directory:   dirName,
		Path:        path,
		Frontmatter: fm,
		Body:        body,
		Inputs:      parseInputs(body),
		Steps:       parseSteps(body),
	}
	return s, nil
}

func parseInputs(body string) []InputSpec {
	lines := strings.Split(body, "\n")
	var out []InputSpec
	inInputs := false

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			if strings.EqualFold(trim, "## Inputs") {
				inInputs = true
				continue
			}
			if inInputs {
				break
			}
		}
		if !inInputs || trim == "" {
			continue
		}

		m := inputBulletRE.FindStringSubmatch(trim)
		if len(m) != 3 {
			continue
		}
		name := strings.TrimSpace(m[1])
		desc := strings.TrimSpace(m[2])
		if name == "" {
			continue
		}
		out = append(out, InputSpec{
			Name:        name,
			Description: desc,
			Required:    true,
		})
	}
	return out
}

func parseSteps(body string) []string {
	lines := strings.Split(body, "\n")
	var (
		steps    []string
		inSteps  bool
		currStep strings.Builder
		hasStep  bool
	)

	flush := func() {
		if hasStep {
			step := strings.TrimSpace(currStep.String())
			if step != "" {
				steps = append(steps, step)
			}
			currStep.Reset()
			hasStep = false
		}
	}

	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			if strings.EqualFold(trim, "## Steps") {
				inSteps = true
				continue
			}
			if inSteps {
				break
			}
		}
		if !inSteps {
			continue
		}

		switch {
		case numberedStepRE.MatchString(trim):
			flush()
			currStep.WriteString(numberedStepRE.ReplaceAllString(trim, ""))
			hasStep = true
		case strings.HasPrefix(trim, "- "):
			if hasStep {
				currStep.WriteString(" ")
				currStep.WriteString(strings.TrimSpace(strings.TrimPrefix(trim, "- ")))
			}
		case trim == "":
			flush()
		default:
			if hasStep {
				currStep.WriteString(" ")
				currStep.WriteString(trim)
			}
		}
	}
	flush()
	return steps
}
