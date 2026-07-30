package analyze

import (
	"encoding/json"
	"strings"
)

type eventKind int

const (
	kindOther eventKind = iota
	kindUserInput
	kindAssistantText
	kindThinking
	kindToolCall
	kindToolResult
)

type action struct {
	Kind       eventKind
	Category   string
	Label      string
	Key        string
	Confidence float64
}

func classifyRecord(record map[string]any, provider string) (action, bool) {
	if provider == "codex" {
		return classifyCodex(record)
	}
	return classifyMessage(record)
}

func classifyCodex(record map[string]any) (action, bool) {
	payload, ok := record["payload"].(map[string]any)
	if !ok {
		return action{}, false
	}
	switch text(payload["type"]) {
	case "function_call", "custom_tool_call", "local_shell_call":
		return toolAction(text(payload["name"]), toolInput(payload)), true
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
		return action{Kind: kindToolResult, Confidence: .8}, true
	case "reasoning":
		return action{Kind: kindThinking, Confidence: .9}, true
	case "message":
		switch text(payload["role"]) {
		case "user":
			return action{Kind: kindUserInput, Confidence: .85}, true
		case "assistant":
			return action{Kind: kindAssistantText, Confidence: .75}, true
		}
	}
	return action{}, false
}

func classifyMessage(record map[string]any) (action, bool) {
	message, _ := record["message"].(map[string]any)
	role := text(record["role"])
	if message != nil && text(message["role"]) != "" {
		role = text(message["role"])
	}
	if role == "" {
		role = text(record["type"])
	}

	var blocks []any
	if message != nil {
		blocks, _ = message["content"].([]any)
	}
	if blocks == nil {
		blocks, _ = record["content"].([]any)
	}

	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch text(block["type"]) {
		case "tool_use", "server_tool_use", "mcp_tool_use":
			return toolAction(text(block["name"]), mapOf(block["input"])), true
		case "tool_result", "mcp_tool_result", "web_search_tool_result":
			return action{Kind: kindToolResult, Confidence: .8}, true
		case "thinking", "redacted_thinking":
			return action{Kind: kindThinking, Confidence: .9}, true
		}
	}

	switch role {
	case "user", "human":
		return action{Kind: kindUserInput, Confidence: .85}, true
	case "assistant", "model":
		return action{Kind: kindAssistantText, Confidence: .75}, true
	}
	return action{}, false
}

func toolInput(payload map[string]any) map[string]any {
	for _, key := range []string{"arguments", "input", "parameters"} {
		switch value := payload[key].(type) {
		case map[string]any:
			return value
		case string:
			var decoded map[string]any
			if json.Unmarshal([]byte(value), &decoded) == nil {
				return decoded
			}
			return map[string]any{"command": value}
		}
	}
	return nil
}

func mapOf(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

var (
	exploreTools = set(
		"read", "read_file", "readfile", "view", "view_file", "view_image", "open", "cat",
		"glob", "grep", "search", "ripgrep", "file_search", "codebase_search", "semantic_search",
		"ls", "list_dir", "listdirectory", "find", "notebookread", "toolsearch",
		"websearch", "web_search", "webfetch", "web_fetch", "fetch", "browse",
	)
	editTools = set(
		"edit", "edit_file", "editfile", "write", "write_file", "writefile", "create_file",
		"multiedit", "notebookedit", "str_replace", "strreplace", "string_replace",
		"search_replace", "apply_patch", "applypatch", "patch", "delete_file", "rename_file",
	)
	shellTools = set(
		"bash", "shell", "exec", "exec_command", "execcommand", "run", "run_command",
		"run_terminal_cmd", "terminal", "local_shell", "awaitshell", "await_shell",
		"process", "python", "node", "js", "script",
	)
	agentTools = set(
		"task", "agent", "subagent", "spawn_agent", "wait_agent", "list_agents",
		"followup_task", "dispatch_agent", "send_message", "sendmessage", "workflow",
	)
	planTools = set(
		"todowrite", "update_plan", "updateplan", "taskcreate", "taskupdate", "tasklist",
		"taskget", "exitplanmode", "enterplanmode", "structuredoutput", "askuserquestion",
	)
)

func toolAction(name string, input map[string]any) action {
	tool := normalizeTool(name)
	switch {
	case tool == "":
		return action{Kind: kindToolCall, Category: "tool_other", Label: "tool call", Confidence: .5}
	case exploreTools[tool]:
		return action{Kind: kindToolCall, Category: "explore", Label: "read / search", Confidence: .9}
	case editTools[tool]:
		return action{Kind: kindToolCall, Category: "edit", Label: "code change", Confidence: .9}
	case agentTools[tool]:
		return action{Kind: kindToolCall, Category: "agent_wait", Label: "agent join", Confidence: .85}
	case planTools[tool]:
		return action{Kind: kindToolCall, Category: "reasoning", Label: "planning", Confidence: .8}
	case tool == "wait", tool == "write_stdin", tool == "writestdin":
		return action{Kind: kindToolCall, Category: "tool_other", Label: "waiting on command", Confidence: .7}
	case shellTools[tool]:
		category, label, key := classifyCommand(commandOf(input))
		return action{Kind: kindToolCall, Category: category, Label: label, Key: key, Confidence: confidenceForCommand(key)}
	default:
		return action{Kind: kindToolCall, Category: "tool_other", Label: "tool call", Confidence: .5}
	}
}

func normalizeTool(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if index := strings.LastIndex(name, "__"); index >= 0 {
		name = name[index+2:]
	}
	if index := strings.LastIndex(name, "."); index >= 0 {
		name = name[index+1:]
	}
	return name
}

func commandOf(input map[string]any) string {
	for _, key := range []string{"command", "cmd", "script", "code", "input", "argv"} {
		switch value := input[key].(type) {
		case string:
			return value
		case []any:
			parts := make([]string, 0, len(value))
			for _, item := range value {
				parts = append(parts, text(item))
			}
			return strings.Join(parts, " ")
		}
	}
	return ""
}

var (
	ciNeedles         = []string{"gh run watch", "gh run view", "gh pr checks", "gh workflow run", "circleci", "buildkite", "gitlab-ci", "az pipelines", "waiting for ci"}
	verifyNeedles     = []string{"go test", "go vet", "go build", "golangci-lint", "cargo test", "cargo build", "cargo clippy", "pytest", "tox ", "nox ", "unittest", "npm test", "npm run test", "npm run build", "pnpm test", "pnpm build", "yarn test", "vitest", "jest", "mocha", "rspec", "rake test", "mvn test", "mvn verify", "gradle test", "gradlew test", "ctest", "make test", "make check", "make build", "tsc ", "eslint", "ruff ", "mypy", "flake8", "shellcheck", "dotnet test", "phpunit", "bats "}
	dependencyNeedles = []string{"npm install", "npm ci", "pnpm install", "yarn install", "pip install", "uv pip", "uv sync", "poetry install", "cargo fetch", "go mod download", "go mod tidy", "bundle install", "gem install", "brew install", "apt-get", "apk add", "docker build", "docker pull", "docker compose", "git clone", "git fetch", "git pull", "git push", "curl ", "wget "}
	editNeedles       = []string{"sed -i", "git commit", "git add", "git checkout", "git switch", "git rebase", "git merge", "mkdir ", "chmod ", "mv ", "rm -", "touch "}
	exploreNeedles    = []string{"git status", "git diff", "git log", "git show", "gh api", "gh pr view", "gh issue", "ls ", "ls -", "cat ", "head ", "tail ", "grep ", "rg ", "find ", "sed -n", "awk ", "wc ", "jq ", "tree ", "which ", "pwd", "du ", "stat "}
)

func classifyCommand(raw string) (string, string, string) {
	command := normalizeCommand(raw)
	if command == "" {
		return "tool_other", "shell command", ""
	}
	window := command
	if len(window) > 400 {
		window = window[:400]
	}
	key := commandKey(command)
	switch {
	case containsAny(window, ciNeedles...):
		return "ci_wait", "CI feedback", key
	case containsAny(window, verifyNeedles...):
		return "verify", "test suite", key
	case containsAny(window, dependencyNeedles...):
		return "dependency_wait", "dependency / network", key
	case containsAny(window, editNeedles...):
		return "edit", "code change", ""
	case containsAny(window, exploreNeedles...):
		return "explore", "read / search", ""
	default:
		return "tool_other", "shell command", ""
	}
}

func normalizeCommand(raw string) string {
	command := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	for _, prefix := range []string{"bash -lc ", "bash -c ", "sh -c ", "zsh -c ", "/bin/bash -lc ", "/bin/sh -c "} {
		command = strings.TrimPrefix(command, prefix)
	}
	command = strings.Trim(command, "'\"")
	for strings.HasPrefix(command, "cd ") {
		index := strings.Index(command, "&&")
		if index < 0 {
			break
		}
		command = strings.TrimSpace(command[index+2:])
	}
	return command
}

func commandKey(command string) string {
	if len(command) > 180 {
		command = command[:180]
	}
	return command
}

func confidenceForCommand(key string) float64 {
	if key == "" {
		return .55
	}
	return .9
}

func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func text(value any) string {
	if s, ok := value.(string); ok {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return ""
}
