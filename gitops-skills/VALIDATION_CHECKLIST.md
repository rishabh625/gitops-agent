# AgentSkills Validation Checklist

Use this checklist for every skill under `gitops-skills/`.

## Frontmatter compliance

- [ ] File path is `gitops-skills/<skill-name>/SKILL.md`.
- [ ] `name` exists, matches `<skill-name>`, is 1-64 chars, lowercase letters/numbers/hyphens only.
- [ ] `name` does not start or end with `-`.
- [ ] `description` exists and is 1-1024 chars.
- [ ] `description` explains both what the skill does and when to use it.
- [ ] Optional fields (`license`, `compatibility`, `metadata`, `allowed-tools`) follow spec constraints.
- [ ] `metadata` values are string values.
- [ ] `allowed-tools` is a space-separated string.

## Body quality

- [ ] Body includes clear inputs.
- [ ] Body includes expected outputs.
- [ ] Body includes explicit tool bindings.
- [ ] Body includes ordered execution steps.
- [ ] Body includes guardrails and fallback behavior.
- [ ] `SKILL.md` is under 500 lines.

## Packaging structure

- [ ] Optional resources are stored under `scripts/`, `references/`, or `assets/` when used.
- [ ] References from `SKILL.md` point to files within the same skill directory.
- [ ] Skill can be understood without requiring external undocumented conventions.
