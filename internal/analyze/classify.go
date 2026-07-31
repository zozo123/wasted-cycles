package analyze

import (
	"encoding/json"
	"hash/fnv"
	"regexp"
	"strconv"
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
		"glob", "grep", "rg", "search", "ripgrep", "file_search", "codebase_search", "semantic_search",
		"ls", "list_dir", "listdirectory", "find", "notebookread", "toolsearch", "readlints",
		"websearch", "web_search", "webfetch", "web_fetch", "fetch", "browse",
	)
	editTools = set(
		"edit", "edit_file", "editfile", "write", "write_file", "writefile", "create_file",
		"multiedit", "notebookedit", "str_replace", "strreplace", "string_replace",
		"search_replace", "apply_patch", "applypatch", "patch", "delete", "delete_file", "rename_file",
	)
	shellTools = set(
		"bash", "shell", "exec", "exec_command", "execcommand", "run", "run_command",
		"run_terminal_cmd", "terminal", "local_shell", "await", "awaitshell", "await_shell",
		"process", "python", "node", "js", "script",
	)
	agentTools = set(
		"task", "agent", "subagent", "spawn_agent", "wait_agent", "list_agents",
		"followup_task", "dispatch_agent", "send_message", "sendmessage", "workflow",
	)
	planTools = set(
		"todowrite", "update_plan", "updateplan", "createplan", "taskcreate", "taskupdate", "tasklist",
		"taskget", "exitplanmode", "enterplanmode", "structuredoutput", "askuserquestion", "askquestion",
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
		// These block on a process the agent already started, so they are a
		// compute wait -- but *which* compute is only knowable by correlating
		// write_stdin's session_id with the exec_command that printed
		// "Process running with session ID <n>". That correlation needs the
		// session walk in analyze.go, not a per-record classifier, so the
		// honest answer here is a labelled non-claim: visible as a wait,
		// never counted as a wasted cycle. Guessing a bucket would inflate
		// the headline number, and the metric's credibility depends on
		// under-counting rather than over-claiming.
		return action{Kind: kindToolCall, Category: "tool_other", Label: "blocked on background process", Confidence: .6}
	case shellTools[tool]:
		return shellAction(tool, input)
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

func shellAction(tool string, input map[string]any) action {
	raw := commandOf(input)
	if jsWrapperTools[tool] {
		if commands := extractShellCommands(raw); len(commands) > 0 {
			return batchAction(commands)
		}
	}
	category, label, key := classifyCommand(raw)
	return action{Kind: kindToolCall, Category: category, Label: label, Key: key, Confidence: confidenceFor(category)}
}

// Codex's `exec` tool does not carry a command string: it carries a JavaScript
// program whose body calls tools.exec_command({cmd: "..."}). toolInput cannot
// parse that as JSON, so without extraction the classifier ends up substring
// scanning JavaScript -- which mostly lands in the right bucket by accident
// (the command text is embedded in the source) but poisons the retry key with
// boilerplate and silently drops every command that starts past the scan
// window of a Promise.all batch.
//
// The pattern is deliberately not anchored inside "tools.exec_command(": real
// traces dispatch through data tables ([{name: "x", cmd: "..."}]) and tuples
// ([["exec_command", {cmd: "..."}]]) that an anchored form misses. jsProgram
// keeps that safe by requiring the payload to actually look like a program.
var (
	jsWrapperTools   = set("exec", "js", "node", "python", "script")
	jsCommandPattern = regexp.MustCompile(`(?:"cmd"|"command"|\bcmd|\bcommand)\s*:\s*"((?:[^"\\]|\\.)*)"`)
	jsProgramMarkers = []string{"tools.", "exec_command", "await ", "const ", "=>"}
)

const (
	jsScanLimit   = 20000
	maxJSCommands = 8
)

func extractShellCommands(source string) []string {
	if source == "" {
		return nil
	}
	source = clip(source, jsScanLimit)
	if !containsAny(strings.ToLower(source), jsProgramMarkers...) {
		return nil
	}
	matches := jsCommandPattern.FindAllStringSubmatch(source, maxJSCommands)
	commands := make([]string, 0, len(matches))
	for _, match := range matches {
		if command := unescapeJSString(match[1]); command != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

// JavaScript and JSON agree on every double-quoted escape observed in real
// traces, so the decoder is just json.Unmarshal on a re-quoted literal. The one
// payload in ~5k that it cannot decode falls back to the raw capture.
func unescapeJSString(value string) string {
	var decoded string
	if json.Unmarshal([]byte(`"`+value+`"`), &decoded) == nil {
		return decoded
	}
	return value
}

// A Promise.all batch settles only when its slowest leg does, so wall clock
// belongs to the most blocking thing in it, not to whichever command was
// written first.
var blockRank = map[string]int{
	"ci_wait":         9,
	"container_wait":  8,
	"build_wait":      7,
	"test_wait":       6,
	"dependency_wait": 5,
	"agent_wait":      4,
	"edit":            3,
	"explore":         2,
	"tool_other":      1,
}

func batchAction(commands []string) action {
	category, label := "tool_other", "shell command"
	keys := make([]string, 0, len(commands))
	for _, command := range commands {
		got, gotLabel, _ := classifyCommand(command)
		keys = append(keys, commandKey(normalizeCommand(command)))
		if blockRank[got] > blockRank[category] {
			category, label = got, gotLabel
		}
	}
	// The key covers every leg of the batch: two batches that share a first
	// command but differ later are different work and must not dedup together.
	key := repeatKey(category, strings.Join(keys, " ;; "))
	return action{Kind: kindToolCall, Category: category, Label: label, Key: key, Confidence: confidenceFor(category)}
}

// Needle lists are scanned in the order used by bucketFor. Every needle is a
// lowercase substring matched against a normalized command, so each one must
// carry enough context to be unambiguous: bare tool names ("act", "aws", "kind",
// "travis") collide with English words, file paths, and YAML filenames that the
// agent merely reads or edits.
//
// Lists whose name ends in HeadNeedles are matched only at the start of a shell
// segment (see commandSegments). That is reserved for needles whose measured
// false positives come from the agent *reading* a Dockerfile, README or CI yaml
// that contains the command -- `rg 'uv sync'` and `sed -n 1,40p Dockerfile`
// fire a free substring but never a segment head.
var (
	// Hosted pipelines the agent watches or polls. The machine doing the work is
	// a remote CI fleet; the local process only blocks on its verdict.
	ciNeedles = []string{
		"gh run watch", "gh run view", "gh run list", "gh run rerun", "gh pr checks",
		"gh workflow run", "gh workflow view", "gh cache",
		"circleci local", "circleci pipeline", "circleci config validate",
		"buildkite-agent", "glab ci", "gitlab-runner", "az pipelines", "jenkins-cli",
		"drone build", "drone exec", "teamcity", "argo submit",
		// "travis " is dropped in favour of its subcommands: the bare form matches
		// `git log --author=travis` and anyone named Travis.
		"travis logs", "travis status", "travis restart", "travis monitor",
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
		"kubectl delete", "kubectl create", "kubectl top",
		"helm install", "helm upgrade", "helm uninstall", "helm rollback",
		"skaffold dev", "skaffold run", "skaffold debug", "tilt up", "argocd app sync",
		"terraform apply", "terraform plan", "terraform destroy", "terraform refresh",
		"tofu apply", "tofu plan", "pulumi up", "pulumi preview", "pulumi destroy",
		"cdk deploy", "cdk destroy", "ansible-playbook",
		"aws cloudformation", "aws ecs ", "aws eks ", "aws ec2 ", "aws lambda ",
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
		"nx build", "parcel build", "rollup -c", "rollup --config",
		"webpack --", "esbuild --bundle",
		"tsc -b", "tsc --build", "tsc -p ", ".bin/tsc",
		// Make targets that are unambiguously a build. Bare "make" is handled by
		// buildPrefixes so that "make test" can still reach testNeedles.
		"make build", "make all", "make -j", "make compile",
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
	// Image builds of source. Head-anchored because `rg "docker build" docs/` and
	// `sed -n 1,40p Dockerfile` are how a quarter of the free-substring hits were
	// produced: the agent was reading the build, not running it.
	buildHeadNeedles = []string{
		"docker build", "docker buildx", "buildah bud", "buildah build", "kaniko",
		"pack build", "ko build", "nix build", "nix-build",
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
		"yarn test", "bun test", "vitest run", "playwright test", "cypress run",
		"jest ", "mocha ", "eslint ", "tsc --noemit", "typecheck",
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
	// Test runners whose bare name is a config filename (vitest.config.ts,
	// jest.config.js, .eslintrc) or an npm dependency line. Safe at a segment
	// head, never as a free substring. Runner prefixes are already stripped, so
	// "pnpm exec vitest" and "npx jest" reach this list as their bare names.
	testHeadNeedles = []string{
		"vitest", "jest", "eslint", "mocha", "rspec", "phpunit", "biome check", "oxlint",
	}
	// Weak container signals: these run *something* inside a container, so the
	// payload decides the bucket. Scanned after build and test needles so that
	// "docker run ... pytest" stays a test wait.
	containerRunNeedles = []string{
		"docker run ", "docker start ", "docker exec ", "docker attach", "docker wait",
		"docker compose run", "docker compose exec", "nerdctl run", "kubectl exec",
	}
	// Registry, proxy and remote-host fetches. Distinctive enough to match
	// anywhere in the command.
	dependencyNeedles = []string{
		"cargo fetch", "cargo update", "cargo vendor", "cargo add ",
		"go mod download", "go mod tidy", "go mod vendor", "go get ", "go list -m -u",
		"dotnet restore", "nuget restore", "mix deps.get", "rebar3 get-deps",
		"gradle --refresh-dependencies", "swift package resolve", "cabal update",
		"mvn dependency:", "pip-compile", "helm repo update",
		// terraform init provisions nothing: it downloads provider plugins and
		// modules from a registry. apply/plan/destroy are the container waits.
		"terraform init", "tofu init",
		// Bulk object-storage transfer, in both directions. The infra catalog
		// claimed "aws s3 sync" as container_wait; one owner is better than two
		// needles, and the wall clock here is registry bandwidth.
		"aws s3 sync", "aws s3 cp", "gsutil cp", "gsutil rsync",
		"ollama pull", "huggingface-cli download",
	}
	// Fetches whose measured false positives are all reader-headed: `rg 'uv sync'`,
	// `grep -rn "npm install" README.md`, `cat Dockerfile` full of `apk add`.
	dependencyHeadNeedles = []string{
		"npm install", "npm ci", "npm update", "npm audit fix", "npm publish",
		"pnpm install", "pnpm add", "pnpm update", "yarn install", "yarn add", "bun install",
		"pip install", "pip3 install", "pip download", "uv pip", "uv sync", "uv lock", "uv add",
		"poetry install", "poetry lock", "poetry add", "pipenv install",
		"conda install", "conda env create", "mamba install",
		"apt-get", "apt install", "apk add", "yum install", "dnf install", "zypper install",
		"brew install", "brew update", "brew upgrade", "choco install", "winget install",
		"gem install", "bundle install", "bundle update", "composer install", "composer update",
		"rustup toolchain", "rustup target add", "rustup component add", "rustup install",
		"asdf install", "mise install", "nvm install", "pyenv install",
		"nix-shell", "nix develop", "nix flake update",
		"docker pull", "docker push", "podman pull",
		"git clone", "git fetch", "git pull", "git push", "git ls-remote",
		"git submodule update", "git lfs pull", "git remote update",
		"curl ", "wget ", "aria2c", "scp ", "rsync ",
	}
	editNeedles    = []string{"sed -i", "git commit", "git add", "git checkout", "git switch", "git rebase", "git merge", "mkdir ", "chmod ", "mv ", "rm -", "touch "}
	exploreNeedles = []string{"git status", "git diff", "git log", "git show", "git branch", "git rev-parse", "gh api", "gh pr view", "gh issue", "kubectl get", "kubectl describe", "docker ps", "docker images", "terraform fmt", "terraform validate", "helm template", "kustomize build", "npm view", "npm ls", "pip list", "pip show", "cargo tree", "go list", "ls ", "ls -", "cat ", "head ", "tail ", "grep ", "rg ", "find ", "sed -n", "awk ", "wc ", "jq ", "tree ", "which ", "pwd", "du ", "stat "}
)

// Build tools whose bare name is only safe at the very start of a command.
// normalizeCommand has already stripped shell wrappers, env assignments, runner
// prefixes and leading "cd X &&" hops, so a prefix match here really is the
// command being invoked. Anchoring is what makes these shippable at all: as free
// substrings "make" also matches "makefile", "makemigrations" and "make sure",
// and across 79k real commands bare "make " fired 138 times but only 27 at the
// start of a command -- of which 24 were genuine invocations.
//
// This is the weakest build signal in the file, so it is checked last: after
// testNeedles so "make test", "gradlew check" and "xcodebuild test" reach their
// own bucket, and after dependencyNeedles so "mvn dependency:go-offline" is
// read as the registry fetch it is rather than as a Maven build.
var (
	buildPrefixes = []string{
		"make ", "ninja", "cmake ", "msbuild ", "meson ", "scons", "mvn ",
		"gradlew ", "./gradlew ", "xcodebuild", "zig ", "waf ",
	}
	bareBuildCommands = set("make", "ninja", "scons", "msbuild", "xcodebuild")
)

// makeProse guards the one English collision that survives anchoring: "make
// sure ..." is by far the most common way a bare "make " lands at the start of
// a string that is not a command.
var makeProse = set("sure", "it", "a", "an", "the", "this", "that", "them", "us", "me", "sense", "progress", "changes")

func hasBuildPrefix(segments []string) bool {
	for _, segment := range segments {
		if bareBuildCommands[segment] {
			return true
		}
		for _, prefix := range buildPrefixes {
			if !strings.HasPrefix(segment, prefix) {
				continue
			}
			if prefix == "make " && makeProse[firstField(segment[len(prefix):])] {
				continue
			}
			return true
		}
	}
	return false
}

const (
	scanWindow    = 400
	segmentWindow = 2000
	maxSegments   = 24
	keyWidth      = 180
)

func classifyCommand(raw string) (string, string, string) {
	command := normalizeCommand(raw)
	if command == "" {
		return "tool_other", "shell command", ""
	}
	category, label := bucketFor(command)
	return category, label, repeatKey(category, command)
}

// bucketFor is the precedence ladder, and the order is the whole design:
//
//  1. capability probes -- `--version` and `command -v` are sub-second reads and
//     must not be swallowed by the tool-name needle they contain.
//  2. hosted CI -- someone else's fleet is doing the work; a `gh run watch` that
//     also mentions a build is still a poll, so CI beats build.
//  3. strong container signals -- the payload *is* the infrastructure.
//  4. build + test in one chain -- the compile is a prerequisite the test run
//     would have done anyway, and the measured wall clock sits in the test.
//  5. build, then 6. test, then 7. an npm/pnpm/yarn script name.
//  8. weak container signals -- `docker run img pytest` is a test that happens
//     to be containerized, so this tier must lose to build and test.
//  9. readiness polls, then 10. loopback fetches -- the agent smoke-testing its
//     own server is not a registry fetch, so both beat dependency.
//  11. dependency fetches.
//  12. bare build tool names at the head of the command -- the weakest build
//     signal, so it loses to every explicit verb above.
//  13. edit, then 14. explore, then tool_other.
func bucketFor(command string) (string, string) {
	window := clip(command, scanWindow)
	segments := commandSegments(command)
	running := runningWindow(segments)
	build := containsAny(running, buildNeedles...) || headMatches(segments, buildHeadNeedles...)
	test := containsAny(running, testNeedles...) || headMatches(segments, testHeadNeedles...)
	script := scriptCategory(segments)

	switch {
	case isCapabilityProbe(window):
		return "explore", "capability probe"
	case containsAny(window, dryRunNeedles...):
		return "explore", "dry run"
	case containsAny(running, ciNeedles...):
		return "ci_wait", "CI feedback"
	case containsAny(running, containerNeedles...):
		return "container_wait", "container / infra wait"
	case build && test:
		return "test_wait", "build + test"
	case build:
		return "build_wait", "build"
	case test:
		return "test_wait", "test suite"
	case script != "":
		return script, "package script"
	case containsAny(running, containerRunNeedles...):
		return "container_wait", "container / infra wait"
	case isInfraPoll(window, running):
		return "container_wait", "polling for readiness"
	case isLoopbackFetch(running):
		return "test_wait", "local endpoint check"
	case containsAny(running, dependencyNeedles...), headMatches(segments, dependencyHeadNeedles...):
		return "dependency_wait", "dependency / network"
	case hasBuildPrefix(segments):
		return "build_wait", "build"
	case containsAny(window, editNeedles...):
		return "edit", "code change"
	case containsAny(window, exploreNeedles...):
		return "explore", "read / search"
	default:
		return "tool_other", "shell command"
	}
}

// Version and capability probes return in milliseconds. They are checked first
// because every one of them contains the name of a tool that some needle below
// would otherwise claim as a multi-minute wait.
var probeNeedles = []string{" --version", " --help", "command -v ", "type -p "}

// Flags that turn an otherwise long command into an instant local check:
// `ansible-playbook site.yml --syntax-check` contacts no host at all.
var dryRunNeedles = []string{"--dry-run", "--syntax-check", "--check-only"}

func isCapabilityProbe(window string) bool {
	return containsAny(window, probeNeedles...)
}

// Sleep, watch and until loops only mean "blocked on infrastructure" when they
// are polling something. On their own they are far too generic: a heredoc that
// writes time.sleep(1) into a script is an edit, not a wait.
var (
	pollPrimitives   = []string{"sleep ", "watch -n", "until ", "while ! ", "timeout "}
	pollInfraTargets = []string{"kubectl ", "docker ", "helm ", "minikube", "nc -z", "pg_isready", "/healthz", "/readyz", "localhost:", "127.0.0.1:"}
)

// The primitive may sit anywhere in the command, but the thing being polled has
// to be in a segment that actually runs: `sed -i 's/sleep 1/sleep 2/' poll.sh`
// mentions both halves and is an edit.
func isInfraPoll(window, running string) bool {
	return containsAny(window, pollPrimitives...) && containsAny(running, pollInfraTargets...)
}

// Commands that only print or search text. A needle inside one of them is
// content, not work: `echo go build`, `rg "pip install" README.md` and
// `sed -n '1,40p' Dockerfile` all carry a build or install command they are not
// running. Reader-headed segments are therefore excluded from every
// blocked-on-compute scan, while edit and explore still see the whole command.
var readerHeads = []string{
	"echo ", "printf ", "cat ", "bat ", "rg ", "grep ", "egrep ", "ag ", "ack ",
	"sed ", "awk ", "head ", "tail ", "less ", "more ", "diff ", "comm ", "jq ", "wc ",
}

func runningWindow(segments []string) string {
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if hasAnyPrefix(segment, readerHeads...) {
			continue
		}
		kept = append(kept, segment)
	}
	return clip(strings.Join(kept, " ; "), scanWindow)
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// Nearly a third of all curl invocations in real traces target the agent's own
// server on loopback. That is the agent checking its work, not a registry
// download, and counting it as dependency_wait was the single largest mislabel
// in that bucket.
var (
	fetchTools    = []string{"curl ", "wget "}
	loopbackHosts = []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]"}
)

func isLoopbackFetch(window string) bool {
	return containsAny(window, fetchTools...) && containsAny(window, loopbackHosts...)
}

// `npm run <script>` covers far more than the handful of full strings worth
// listing, so the script name decides the bucket. Script names outside these
// two sets (dev, start, serve) are long-lived local processes rather than a
// wait on someone else's compute, and are deliberately left to tool_other.
var (
	scriptRunners = []string{"npm run ", "npm run-script ", "pnpm run ", "yarn run ", "bun run "}
	buildScripts  = set("build", "bundle", "compile", "dist", "prepack", "prepare", "codegen", "generate")
	testScripts   = set("test", "tests", "lint", "typecheck", "type-check", "check", "e2e", "coverage", "spec", "vitest", "jest", "verify", "ci")
)

func scriptCategory(segments []string) string {
	for _, segment := range segments {
		for _, runner := range scriptRunners {
			if !strings.HasPrefix(segment, runner) {
				continue
			}
			name := firstField(segment[len(runner):])
			// "build:prod" and "test:unit" are the same work as their base task.
			if colon := strings.Index(name, ":"); colon > 0 {
				name = name[:colon]
			}
			switch {
			case buildScripts[name]:
				return "build_wait"
			case testScripts[name]:
				return "test_wait"
			}
		}
	}
	return ""
}

var (
	shellWrappers   = []string{"bash -lc ", "bash -c ", "sh -c ", "zsh -c ", "/bin/bash -lc ", "/bin/sh -c "}
	commandWrappers = []string{"sudo ", "time ", "nohup ", "exec ", "stdbuf -o0 "}
	// Runner prefixes carry no information of their own: "uv run pytest" is a
	// test wait and "uv run ruff format src" is an edit, and calling either of
	// them "uv" is meaningless. Strip them and classify what is left.
	runnerPrefixes = []string{
		"uv run ", "uvx ", "npx --yes ", "npx -y ", "npx ", "pnpm exec ", "pnpm dlx ",
		"yarn dlx ", "bunx ", "poetry run ", "pipenv run ", "bundle exec ",
		"mise exec ", "mise x ", "pdm run ", "hatch run ", "rye run ", "nix develop -c ",
	}
	// Shell keywords that can precede the real command inside a segment.
	segmentKeywords = []string{"until ", "while ", "if ", "then ", "do ", "else ", "elif ", "! "}
)

func normalizeCommand(raw string) string {
	command := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	for _, prefix := range shellWrappers {
		command = strings.TrimPrefix(command, prefix)
	}
	command = strings.Trim(command, "'\"")
	return stripGitGlobalFlags(stripLeadingNoise(command))
}

// stripLeadingNoise removes everything in front of the command that says
// nothing about what the machine is doing: "cd X &&" hops, leading VAR=value
// assignments (~90 commands in the local corpus start with one), sudo/time
// wrappers and package-runner prefixes. It loops because these stack:
// "cd api && ISLO_ENV=local uv run pytest".
func stripLeadingNoise(command string) string {
	for {
		trimmed := command
		if strings.HasPrefix(trimmed, "cd ") {
			if index := strings.Index(trimmed, "&&"); index >= 0 {
				trimmed = strings.TrimSpace(trimmed[index+2:])
			}
		}
		if isEnvAssignment(firstField(trimmed)) {
			trimmed = strings.TrimSpace(dropFirstField(trimmed))
		}
		// rustup run takes a toolchain argument before the real command.
		if rest, ok := strings.CutPrefix(trimmed, "rustup run "); ok {
			trimmed = strings.TrimSpace(dropFirstField(rest))
		}
		for _, prefix := range commandWrappers {
			trimmed = strings.TrimPrefix(trimmed, prefix)
		}
		for _, prefix := range runnerPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				trimmed = strings.TrimPrefix(trimmed, prefix)
				break
			}
		}
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == command {
			return command
		}
		command = trimmed
	}
}

// `git -C <dir> fetch` is the single largest blind spot in the git needles:
// every one of them expects the subcommand to be adjacent to "git", and 2596
// real commands pass a global flag first. Collapsing the flags makes the
// subcommand adjacent again. Case folding has already turned -C into -c, and
// both forms take exactly one argument, so they can be dropped together.
var gitValueFlags = set("-c", "--git-dir", "--work-tree", "--exec-path", "--namespace")

var gitBareFlags = set("--no-pager", "-p", "--paginate", "--no-optional-locks", "--literal-pathspecs", "--bare")

func stripGitGlobalFlags(command string) string {
	if !strings.Contains(command, "git ") {
		return command
	}
	fields := strings.Fields(command)
	out := make([]string, 0, len(fields))
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		out = append(out, field)
		if field != "git" && !strings.HasSuffix(field, "/git") {
			continue
		}
		for index+1 < len(fields) && isGitGlobalFlag(fields[index+1]) {
			if gitValueFlags[fields[index+1]] {
				if index+2 >= len(fields) {
					break
				}
				index += 2
				continue
			}
			index++
		}
	}
	return strings.Join(out, " ")
}

func isGitGlobalFlag(field string) bool {
	if gitValueFlags[field] || gitBareFlags[field] {
		return true
	}
	return strings.HasPrefix(field, "--git-dir=") ||
		strings.HasPrefix(field, "--work-tree=") ||
		strings.HasPrefix(field, "--exec-path=")
}

// commandSegments splits a command into the individual invocations a shell
// would run, so a needle can require the command to *start* a segment. Substring
// scanning alone cannot tell `pip install foo` from `rg 'pip install' README`.
//
// The split is quote-aware because the difference between running a command and
// printing one is often exactly a pair of quotes: `echo 'npm install && npm
// test'` is one segment headed by echo, not two package-manager invocations.
func commandSegments(command string) []string {
	parts := splitUnquoted(clip(command, segmentWindow))
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := normalizeSegment(part); segment != "" {
			segments = append(segments, segment)
			if len(segments) == maxSegments {
				break
			}
		}
	}
	return segments
}

func splitUnquoted(command string) []string {
	parts := make([]string, 0, 4)
	var quote byte
	start := 0
	for index := 0; index < len(command); index++ {
		char := command[index]
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
		case ';', '\n', '|', '&':
			parts = append(parts, command[start:index])
			if index+1 < len(command) && (command[index+1] == '|' || command[index+1] == '&') {
				index++
			}
			start = index + 1
		}
	}
	return append(parts, command[start:])
}

func normalizeSegment(part string) string {
	for {
		trimmed := strings.TrimSpace(strings.TrimLeft(part, "({ \t"))
		for _, keyword := range segmentKeywords {
			trimmed = strings.TrimPrefix(trimmed, keyword)
		}
		trimmed = stripLeadingNoise(strings.TrimSpace(trimmed))
		if trimmed == part {
			return part
		}
		part = trimmed
	}
}

// headMatches reports whether any segment *starts* with one of the needles. A
// needle that ends inside a token additionally requires the token to end there,
// so "npm ci" does not match "npm cinnamon" and "vitest" does not match
// "vitest.config.ts".
func headMatches(segments []string, needles ...string) bool {
	for _, segment := range segments {
		for _, needle := range needles {
			if !strings.HasPrefix(segment, needle) {
				continue
			}
			if len(segment) == len(needle) || !isTokenByte(needle[len(needle)-1]) || !isTokenByte(segment[len(needle)]) {
				return true
			}
		}
	}
	return false
}

func isTokenByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		return true
	}
	return b == '_' || b == '-' || b == '.' || b == '/'
}

// repeatKey is the dedup key that turns a second identical command into a
// "retry" segment. It is non-empty for exactly the buckets where running the
// same thing twice is waste the user can act on: a repeated build, test or CI
// poll. Repeating a container start or a dependency install is normal.
var repeatableCategories = set("build_wait", "test_wait", "ci_wait")

func repeatKey(category, command string) string {
	if !repeatableCategories[category] {
		return ""
	}
	return commandKey(command)
}

// commandKey is built from the normalized command, so it survives whitespace,
// casing, `cd` hops, runner prefixes and git global flags. Long commands get a
// hash suffix rather than a bare truncation: two different commands under a
// long shared path prefix would otherwise collide and be reported as retries of
// each other.
func commandKey(command string) string {
	if len(command) <= keyWidth {
		return command
	}
	digest := fnv.New32a()
	digest.Write([]byte(command))
	return command[:keyWidth] + "#" + strconv.FormatUint(uint64(digest.Sum32()), 16)
}

func confidenceFor(category string) float64 {
	if category == "tool_other" {
		return .55
	}
	return .9
}

func clip(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func firstField(value string) string {
	if index := strings.IndexByte(value, ' '); index >= 0 {
		return value[:index]
	}
	return value
}

// isEnvAssignment recognises the "ISLO_ENV=local" / "PATH=..." tokens that
// prefix roughly one command in a hundred and hide the real invocation from
// every head-anchored rule.
func isEnvAssignment(field string) bool {
	equals := strings.IndexByte(field, '=')
	if equals <= 0 {
		return false
	}
	for index := 0; index < equals; index++ {
		b := field[index]
		if !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_') {
			return false
		}
	}
	return true
}

func dropFirstField(value string) string {
	if index := strings.IndexByte(value, ' '); index >= 0 {
		return value[index+1:]
	}
	return ""
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
