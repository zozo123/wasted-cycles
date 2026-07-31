package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestGitHubCommandValidatesArgumentsBeforeNetwork(t *testing.T) {
	for name, args := range map[string][]string{
		"missing repository": {},
		"too many repos":     {"a/b", "c/d"},
		"invalid days":       {"--days", "0", "a/b"},
		"invalid max runs":   {"--max-runs", "0", "a/b"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runGitHub(args, &stdout, &stderr); code != 2 {
				t.Fatalf("code = %d, want 2; stderr=%s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("validation wrote stdout: %q", stdout.String())
			}
		})
	}
}

func TestGitHubCommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runGitHub([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	for _, want := range []string{"OWNER/REPO", "-days", "-json", "-max-runs", "private repositories"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, stderr.String())
		}
	}
}
