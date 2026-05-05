// Package prurl extracts pull/merge request URLs from tool or model output.
package prurl

import (
	"regexp"
	"strings"
)

// Match GitHub/GitLab-style PR and MR links (HTTP/S).
var pullRequestURL = regexp.MustCompile(`https?://[^\s\)\]\"']+/(pull|pulls|merge_requests)/[^\s\)\]\"']+`)

// First returns the first PR/MR URL found in s, or "".
func First(s string) string {
	m := strings.TrimSpace(pullRequestURL.FindString(s))
	return trimTrailingPunctuation(m)
}

func trimTrailingPunctuation(s string) string {
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == '.' || last == ',' || last == ';' || last == ':' || last == '!' || last == '?' || last == ')' || last == ']' || last == '}' || last == '"' || last == '\'' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}
