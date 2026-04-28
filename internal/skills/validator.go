package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var skillNameRE = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateSkill enforces the AgentSkills schema and core repository rules.
func ValidateSkill(s Skill) error {
	fm := s.Frontmatter
	name := fm.Name
	desc := fm.Description

	if name == "" {
		return fmt.Errorf("%s: frontmatter.name is required", s.Path)
	}
	if len(name) > 64 {
		return fmt.Errorf("%s: frontmatter.name exceeds 64 chars", s.Path)
	}
	if !skillNameRE.MatchString(name) {
		return fmt.Errorf("%s: frontmatter.name must match %s", s.Path, skillNameRE.String())
	}
	if name != s.Directory {
		return fmt.Errorf("%s: frontmatter.name (%s) must match directory name (%s)", s.Path, name, s.Directory)
	}

	if desc == "" {
		return fmt.Errorf("%s: frontmatter.description is required", s.Path)
	}
	if len(desc) > 1024 {
		return fmt.Errorf("%s: frontmatter.description exceeds 1024 chars", s.Path)
	}
	if fm.Compatibility != "" && len(fm.Compatibility) > 500 {
		return fmt.Errorf("%s: frontmatter.compatibility exceeds 500 chars", s.Path)
	}
	for k, v := range fm.Metadata {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("%s: frontmatter.metadata contains empty key", s.Path)
		}
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s: frontmatter.metadata[%s] has empty value", s.Path, k)
		}
	}
	if strings.TrimSpace(s.Body) == "" {
		return fmt.Errorf("%s: body is empty", s.Path)
	}
	if len(strings.Split(s.Body, "\n")) > 500 {
		return fmt.Errorf("%s: SKILL.md body exceeds 500 lines", s.Path)
	}
	if len(s.Steps) == 0 {
		return fmt.Errorf("%s: could not parse steps from ## Steps section", s.Path)
	}
	return nil
}
