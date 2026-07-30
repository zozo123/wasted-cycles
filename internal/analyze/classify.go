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

// Needle lists are scanned in the order used by classifyCommand. Every needle is
// a lowercase substring matched against a normalized command, so each one must
// carry enough context to be unambiguous: bare tool names ("act", "aws", "kind",
// "travis") collide with English words, file paths, and YAML filenames that the
// agent merely reads or edits.
var (
	// Hosted pipelines the agent watches or polls. The machine doing the work is
	// a remote CI fleet; the local process only blocks on its verdict.
	ciNeedles = []string{
		"gh run watch", "gh run view", "gh run list", "gh run rerun", "gh pr checks",
		"gh workflow run", "gh workflow view", "gh cache",
		"circleci local", "circleci pipeline", "circleci config validate",
		"buildkite-agent", "glab ci", "gitlab-runner", "az pipelines", "jenkins-cli",
		"drone build", "drone exec", "teamcity", "travis ", "argo submit",
		"aws codebuild", "aws codepipeline",
		"act -j", "act pull_request", "act workflow_dispatch",
		"waiting for ci", "waiting for checks",
	}
	// Containers, VMs, clusters and cloud infrastructure being provisioned,
	// deployed to, or polled. Nothing here compiles source.
	containerNeedles = []string{
		"docker compose up", "docker-compose up", "docker compose start", "docker stack deploy",
		"docker swarm", "podman run", "podman compose", "podman machine",
		"colima start", "lima start", "orbstack start", "minikube start", "minikube delete",
		"kind create cluster", "kind delete cluster", "kind load", "k3d cluster",
		"devcontainer up", "vagrant up", "vagrant provision", "packer build",
		"kubectl apply", "kubectl wait", "kubectl rollout", "kubectl logs -f", "kubectl port-forward",
		"kubectl delete", "kubectl create", "kubectl exec", "kubectl top",
		"helm install", "helm upgrade", "helm uninstall", "helm rollback",
		"skaffold dev", "skaffold run", "skaffold debug", "tilt up", "argocd app sync",
		"terraform apply", "terraform plan", "terraform init", "terraform destroy", "terraform refresh",
		"tofu apply", "tofu plan", "pulumi up", "pulumi preview", "pulumi destroy",
		"cdk deploy", "cdk destroy", "ansible-playbook",
		"aws cloudformation", "aws ecs ", "aws eks ", "aws ec2 ", "aws lambda ", "aws s3 sync",
		"aws logs tail", "aws elasticbeanstalk", "aws ssm start-session",
		"gcloud run deploy", "gcloud container clusters", "gcloud compute instances", "gcloud sql",
		"az deployment", "az aks ", "az vm ", "az webapp", "az containerapp",
		"fly deploy", "flyctl deploy", "vercel deploy", "vercel --prod", "netlify deploy",
		"railway up", "heroku ps", "heroku logs", "git push heroku", "wrangler deploy", "nsc cluster",
	}
	// Compilation, linking, bundling, codegen and image builds of source. Remote
	// and distributed build farms belong here too: the work is still a compile,
	// it just happens on someone else's cores.
	buildNeedles = []string{
		// Go
		"go build", "go install", "go generate", "go run ",
		// Rust
		// "cross build --" rather than "cross build": the shorter form is a
		// substring of the English phrase "across build", which showed up in real
		// traces. Note that "cargo build"/"cargo run"/"cargo test" likewise contain
		// "go build"/"go run"/"go test"; that overlap is harmless because both
		// sides of it land in the same bucket.
		"cargo build", "cargo check", "cargo rustc", "cargo run", "cross build --",
		// C / C++ / native toolchains. Bare compiler names are unusable as
		// substrings -- "cl", "ld", "gcc" and "clang" match "incl", "should",
		// "gcc/clang builds add .cxx" and "apt-get install clang lld" far more
		// often than an actual compile -- so each one carries a flag.
		"gcc -c ", "gcc -o ", "gcc -std", "g++ ", "clang -c ", "clang -o ", "clang -std",
		"clang++ ", "cl.exe", "nvcc ", "cmake --build", "cmake -b ",
		"ninja -c ", "ninja -j", "ninja -f ", "meson compile", "meson setup", "waf build",
		// JVM
		"mvn compile", "mvn package", "mvn install", "gradle build", "gradlew build",
		"gradle assemble", "gradlew assemble", "gradle compile", "gradlew compile",
		"javac ", "sbt compile", "sbt package", "kotlinc ", "scalac ",
		// .NET. "dotnet restore" is deliberately absent: a NuGet restore is a
		// registry fetch and belongs in dependencyNeedles, not here.
		"dotnet build", "dotnet publish", "dotnet msbuild", "dotnet run",
		// Swift / Zig / Nim / Haskell / BEAM
		"swift build", "nim c ", "nim cpp ", "nimble build", "stack build", "cabal build",
		"mix compile", "mix deps.compile", "rebar3 compile",
		// Bazel / Buck
		"bazel build", "bazel run", "buck2 build", "buck build", "protoc ", "cdk synth",
		// Frontend bundlers that are genuinely compile-shaped. Each needle avoids
		// the tool's own config filename: bare "rollup" matches gh's
		// statusCheckRollup, and bare "webpack"/"vite"/"esbuild" match
		// webpack.config.js, vite.config.ts and node_modules/.bin/esbuild.
		"npm run build", "pnpm build", "pnpm run build", "yarn build", "yarn run build",
		"bun run build", "vite build", "next build", "turbo build", "turbo run build",
		"nx build", "parcel build", "rollup -c", "rollup --config", "npx rollup",
		"webpack --", "npx webpack", "esbuild --bundle", "npx esbuild",
		"tsc -b", "tsc --build", "tsc -p ", ".bin/tsc",
		// Make targets that are unambiguously a build. Bare "make" is handled by
		// buildPrefixes so that "make test" can still reach testNeedles.
		"make build", "make all", "make -j", "make compile",
		// Image builds of source.
		"docker build", "docker buildx", "buildah bud", "buildah build", "kaniko",
		"pack build", "ko build", "nix build", "nix-build",
		// Remote / distributed execution of a compile.
		"--remote_executor", "--config=remote", "--remote_cache",
		"depot build", "depot bake", "earthly ", "dagger call", "dagger run", "nsc build",
		"skaffold build",
		"gcloud builds submit", "az acr build",
		// Incredibuild. Bare "incredibuild" is unusable: across 79k real commands
		// it appears overwhelmingly as an org name (incredibuild-rnd/...), an
		// install path (/opt/incredibuild/bin) or a hostname, and almost never as
		// a command. The console binaries are the real invocation.
		// Compiler caches and distributors (ccache, sccache, distcc) are
		// deliberately absent: they only ever appear as a wrapper -- "sccache g++
		// -c x.cpp" -- so the wrapped compiler above already matches, while the
		// bare names collide with the code-search queries agents run about them.
		"ib_console ", "ibconsole ", "buildconsole ",
	}
	// Test, lint and typecheck execution.
	testNeedles = []string{
		// Go
		"go test", "go vet", "golangci-lint", "gotestsum", "staticcheck",
		// Rust
		"cargo test", "cargo nextest", "cargo clippy", "cargo miri",
		// C / C++
		"ctest", "bazel test", "clang-tidy", "cppcheck",
		// Python. "-m unittest" rather than bare "unittest": the bare form matches
		// "from unittest.mock import MagicMock" in every heredoc that stubs a test,
		// which is source the agent is writing, not a suite it is waiting on.
		"pytest", "tox ", "nox ", "-m unittest", "-m pytest",
		"mypy", "pyright", "ruff check", "flake8", "pylint",
		// JS / TS. Every needle here is kept clear of the tool's own config file:
		// "eslint " misses .eslintrc, "vitest run" misses vitest.config.ts,
		// "jest " misses jest.config.js.
		"npm test", "npm run test", "npm run lint", "npm run typecheck",
		"pnpm test", "pnpm run test", "pnpm lint", "pnpm typecheck",
		"yarn test", "bun test", "vitest run", "npx vitest", "exec vitest",
		"jest ", "npx jest", "mocha ", "playwright test", "cypress run",
		"eslint ", "npx eslint", "tsc --noemit", "typecheck",
		// Ruby / PHP / shell
		"rspec", "rake test", "phpunit", "bats ", "shellcheck",
		// JVM
		"mvn test", "mvn verify", "gradle test", "gradlew test", "gradle check", "gradlew check",
		// .NET / Swift / BEAM / Haskell / Zig
		"dotnet test", "swift test", "xcodebuild test", "mix test", "stack test",
		"cabal test", "zig build test",
		// Make targets and commit gates
		"make test", "make check", "make lint", "make verify", "pre-commit run", "prek run",
	}
	// Weak container signals: these run *something* inside a container, so the
	// payload decides the bucket. Scanned after build and test needles so that
	// "docker run ... pytest" stays a test wait.
	containerRunNeedles = []string{
		"docker run ", "docker start ", "docker exec ", "docker attach", "docker wait",
		"docker compose run", "docker compose exec", "nerdctl run",
	}
	dependencyNeedles = []string{
		"npm install", "npm ci", "pnpm install", "yarn install", "pip install", "uv pip", "uv sync",
		"poetry install", "cargo fetch", "cargo update", "go mod download", "go mod tidy",
		"go mod vendor", "bundle install", "gem install", "composer install", "bun install",
		// Package restores for compiled ecosystems. These are registry fetches, not
		// compiles: "dotnet restore" pulls NuGet packages while "dotnet build"
		// invokes the compiler, and the two must not share a bucket.
		"dotnet restore", "nuget restore", "mix deps.get", "rebar3 get-deps",
		"gradle --refresh-dependencies", "swift package resolve", "cabal update",
		"brew install", "apt-get", "apt install", "apk add", "yum install", "dnf install",
		"pacman -s ", "choco install", "winget install", "conda install", "rustup ",
		"docker pull", "docker push",
		"podman pull", "helm repo update", "git clone", "git fetch", "git pull", "git push",
		"curl ", "wget ",
	}
	editNeedles    = []string{"sed -i", "git commit", "git add", "git checkout", "git switch", "git rebase", "git merge", "mkdir ", "chmod ", "mv ", "rm -", "touch "}
	exploreNeedles = []string{"git status", "git diff", "git log", "git show", "gh api", "gh pr view", "gh issue", "kubectl get", "kubectl describe", "docker ps", "docker images", "terraform fmt", "terraform validate", "helm template", "kustomize build", "ls ", "ls -", "cat ", "head ", "tail ", "grep ", "rg ", "find ", "sed -n", "awk ", "wc ", "jq ", "tree ", "which ", "pwd", "du ", "stat "}
)

// Build tools whose bare name is only safe at the very start of a command.
// normalizeCommand has already stripped shell wrappers and leading "cd X &&"
// hops, so a prefix match here really is the command being invoked. Anchoring is
// what makes these shippable at all: as free substrings "make" also matches
// "makefile", "makemigrations" and "make sure", and across 79k real commands
// bare "make " fired 138 times but only 27 at the start of a command -- of which
// 24 were genuine invocations.
//
// This is checked *after* testNeedles so that "make test", "gradlew check" and
// "xcodebuild test" reach their own bucket instead of being swallowed as builds.
var (
	buildPrefixes = []string{
		"make ", "ninja", "cmake ", "msbuild ", "meson ", "scons", "mvn ",
		"gradlew ", "./gradlew ", "xcodebuild", "zig ", "waf ",
	}
	bareBuildCommands = set("make", "ninja", "scons", "msbuild", "xcodebuild")
)

func hasBuildPrefix(command string) bool {
	if bareBuildCommands[command] {
		return true
	}
	for _, prefix := range buildPrefixes {
		if strings.HasPrefix(command, prefix) {
			return true
		}
	}
	return false
}

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
	case containsAny(window, containerNeedles...):
		return "container_wait", "container / infra wait", key
	case containsAny(window, buildNeedles...):
		return "build_wait", "build", key
	case containsAny(window, testNeedles...):
		return "test_wait", "test suite", key
	case hasBuildPrefix(command):
		return "build_wait", "build", key
	case containsAny(window, containerRunNeedles...):
		return "container_wait", "container / infra wait", key
	case isInfraPoll(window):
		return "container_wait", "polling for readiness", key
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

// Sleep, watch and until loops only mean "blocked on infrastructure" when they
// are polling something. On their own they are far too generic: a heredoc that
// writes time.sleep(1) into a script is an edit, not a wait.
var (
	pollPrimitives   = []string{"sleep ", "watch -n", "until ", "while ! ", "timeout "}
	pollInfraTargets = []string{"kubectl ", "docker ", "helm ", "minikube", "nc -z", "pg_isready", "/healthz", "/readyz", "localhost:", "127.0.0.1:"}
)

func isInfraPoll(window string) bool {
	return containsAny(window, pollPrimitives...) && containsAny(window, pollInfraTargets...)
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
