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
	for _, want := range []string{"OWNER/REPO", "--days", "--json", "--max-runs", "Private repositories"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestGitHubOptionsMayFollowRepository(t *testing.T) {
	got, err := normalizeGitHubArgs([]string{"acme/widgets", "--days", "30", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--days", "30", "--json", "acme/widgets"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("normalizeGitHubArgs = %q, want %q", got, want)
	}
}

func TestTopLevelHelpAndUnexpectedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help code = %d", code)
	}
	if !strings.Contains(stdout.String(), "wasted-cycles github") {
		t.Fatalf("top-level help is incomplete:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"unexpected"}, &stdout, &stderr); code != 2 {
		t.Fatalf("unexpected argument code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("missing validation error:\n%s", stderr.String())
	}
}
