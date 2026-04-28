package skills

// Frontmatter contains AgentSkills schema fields used by this executor.
type Frontmatter struct {
	Name          string            `yaml:"name"`
	Description   string            `yaml:"description"`
	License       string            `yaml:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty"`
	AllowedTools  string            `yaml:"allowed-tools,omitempty"`
}

// InputSpec declares a structured input for a skill.
type InputSpec struct {
	Name        string
	Description string
	Required    bool
}

// Skill is a parsed AgentSkills skill package.
type Skill struct {
	Directory   string
	Path        string
	Frontmatter Frontmatter
	Body        string
	Inputs      []InputSpec
	Steps       []string
}
