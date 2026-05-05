package mcpadapter

import "testing"

func TestNormalizeArgsForServer_gitOwnerRepoNotDuplicated(t *testing.T) {
	out := normalizeArgsForServer(ServerGit, map[string]any{
		"repo": "rishabh625/helmsample",
	})
	if got := out["repo"]; got != "helmsample" {
		t.Fatalf("repo = %q, want short name helmsample (GitHub MCP uses owner+repo for API path)", got)
	}
	if got := out["owner"]; got != "rishabh625" {
		t.Fatalf("owner = %q", got)
	}
	if got := out["repository"]; got != "rishabh625/helmsample" {
		t.Fatalf("repository = %q, want full owner/name", got)
	}
}

func TestNormalizeArgsForServer_gitPlainRepoNameUnchanged(t *testing.T) {
	out := normalizeArgsForServer(ServerGit, map[string]any{
		"repo": "helmsample",
		"owner": "rishabh625",
	})
	if got := out["repo"]; got != "helmsample" {
		t.Fatalf("repo = %q", got)
	}
}
