package prurl

import "testing"

func TestFirst(t *testing.T) {
	cases := map[string]string{
		"see https://github.com/o/r/pull/12 ok":                    "https://github.com/o/r/pull/12",
		"MR: https://gitlab.com/foo/bar/-/merge_requests/99":      "https://gitlab.com/foo/bar/-/merge_requests/99",
		"no link here": "",
	}
	for in, want := range cases {
		if got := First(in); got != want {
			t.Errorf("First(%q) = %q, want %q", in, got, want)
		}
	}
}
