package analyze

import (
	"strings"
	"testing"
)

// TestCommandInfraCatalog pins the infrastructure lens: containers, clusters,
// cloud provisioning, remote CI and remote/distributed compile.
func TestCommandInfraCatalog(t *testing.T) {
	cases := map[string]string{
		// hosted CI: the work runs on a remote fleet, we only poll the verdict.
		"gh run watch 123":                           "ci_wait",
		"gh run view 91 --log-failed":                "ci_wait",
		"gh pr checks --watch":                       "ci_wait",
		"gh workflow run release.yml":                "ci_wait",
		"buildkite-agent pipeline upload":            "ci_wait",
		"glab ci status":                             "ci_wait",
		"az pipelines runs show --id 4":              "ci_wait",
		"aws codebuild start-build --project-name p": "ci_wait",
		"act -j build":                               "ci_wait",

		// containers, clusters, VMs, cloud infra.
		"docker compose up -d":                       "container_wait",
		"docker-compose up --build":                  "container_wait",
		"minikube start --cpus 4":                    "container_wait",
		"kind create cluster --name dev":             "container_wait",
		"devcontainer up --workspace-folder .":       "container_wait",
		"colima start":                               "container_wait",
		"vagrant up":                                 "container_wait",
		"kubectl apply -f deploy.yaml":               "container_wait",
		"kubectl wait --for=condition=ready pod/api": "container_wait",
		"kubectl rollout status deploy/api":          "container_wait",
		"kubectl logs -f deploy/api":                 "container_wait",
		"helm upgrade --install api ./chart --wait":  "container_wait",
		"skaffold dev":                               "container_wait",
		"tilt up":                                    "container_wait",
		"terraform apply -auto-approve":              "container_wait",
		"pulumi up --yes":                            "container_wait",
		"cdk deploy ApiStack":                        "container_wait",
		"ansible-playbook -i hosts site.yml":         "container_wait",
		"packer build template.pkr.hcl":              "container_wait",
		"aws cloudformation wait stack-create-complete --stack-name s": "container_wait",
		"aws ecs update-service --service api":                         "container_wait",
		"gcloud run deploy api --source .":                             "container_wait",
		"az deployment group create -g rg":                             "container_wait",
		"fly deploy":                                                   "container_wait",
		"vercel deploy --prod":                                         "container_wait",
		"railway up":                                                   "container_wait",
		"docker run --rm alpine echo hi":                               "container_wait",
		"heroku logs --tail":                                           "container_wait",
		"watch -n 5 kubectl get pods":                                  "container_wait",
		"kubectl port-forward svc/api 8080:80":                         "container_wait",

		// remote / distributed compile and image builds of source.
		"docker build -t api .":                             "build_wait",
		"docker buildx bake":                                "build_wait",
		"buildah bud -t api .":                              "build_wait",
		"depot build -t api .":                              "build_wait",
		"earthly +build":                                    "build_wait",
		"dagger call build":                                 "build_wait",
		"gcloud builds submit --tag gcr.io/p/api":           "build_wait",
		"az acr build --registry r -t api .":                "build_wait",
		"bazel build //... --remote_executor=grpc://rbe:80": "build_wait",
		"nsc build .":                                       "build_wait",
		"ib_console /build /rebuild":                        "build_wait",
		"skaffold build --push":                             "build_wait",
		"ssh builder 'make -j32 all'":                       "build_wait",
		"make -j16 all":                                     "build_wait",
		"go build ./...":                                    "build_wait",
		"cmake --build build":                               "build_wait",

		// tests keep their own bucket even when wrapped in infra.
		"go test ./...":                        "test_wait",
		"bazel test //pkg:all":                 "test_wait",
		"docker run --rm ci-image pytest -q":   "test_wait",
		"docker compose run --rm api npm test": "test_wait",

		// readiness polls are infra waits, not dependency fetches.
		"until curl -sf http://localhost:8080/healthz; do sleep 2; done": "container_wait",
		"while ! nc -z localhost:5432; do sleep 1; done":                 "container_wait",
	}

	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

// TestCommandInfraFalsePositives locks in the substrings that must NOT fire.
// Every case here is an agent reading or editing infrastructure files, or an
// English word that happens to contain a tool name.
func TestCommandInfraFalsePositives(t *testing.T) {
	cases := map[string]string{
		// CI config files are read and edited constantly; never a CI wait.
		"cat .gitlab-ci.yml":                  "explore",
		"cat .circleci/config.yml":            "explore",
		"cat .buildkite/pipeline.yml":         "explore",
		"grep -rn artifact .github/workflows": "explore",
		"git add .travis.yml":                 "edit",

		// infra files and read-only infra queries.
		"cat docker-compose.yml":       "explore",
		"cat Dockerfile":               "explore",
		"kubectl get pods":             "explore",
		"kubectl describe pod api":     "explore",
		"docker ps -a":                 "explore",
		"terraform fmt -recursive":     "explore",
		"terraform validate":           "explore",
		"helm template ./chart":        "explore",
		"kustomize build overlays/dev": "explore",

		// words that contain tool names.
		"grep -rn 'await' src":                "explore",
		"rg 'contact' --files-with-matches":   "explore",
		"cat internal/awsclient/README.md":    "explore",
		"curl -s https://aws.amazon.com/ec2/": "dependency_wait",

		// a sleep the agent writes into a file is not a wait.
		"sed -i 's/sleep 1/sleep 2/' scripts/poll.sh": "edit",
	}

	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

// TestCommandCompiledLanguageCatalog pins the compiled/systems-language lens:
// a compiler or linker is running and the agent is blocked on it.
func TestCommandCompiledLanguageCatalog(t *testing.T) {
	cases := map[string]string{
		// C / C++
		"cmake --build build -j 8":                                     "build_wait",
		"cmake -S llvm -B build -G Ninja":                              "build_wait",
		"ninja -C build clang":                                         "build_wait",
		"ninja -j16":                                                   "build_wait",
		"gcc -c foo.c -o foo.o":                                        "build_wait",
		"gcc -o app main.c":                                            "build_wait",
		"g++ -std=c++20 -O2 -c widget.cpp":                             "build_wait",
		"clang -c parser.c -o parser.o":                                "build_wait",
		"clang++ -stdlib=libc++ -c engine.cpp":                         "build_wait",
		"nvcc -c kernel.cu":                                            "build_wait",
		"meson compile -C builddir":                                    "build_wait",
		"bazel build //packages/core:core":                             "build_wait",
		"buck2 build //app:bin":                                        "build_wait",
		"/opt/incredibuild/bin/ib_console --standalone -- gcc -c b1.c": "build_wait",
		"BuildConsole solution.sln /build /cfg=Release":                "build_wait",

		// Rust
		"cargo build --release --locked":                 "build_wait",
		"cargo check --all-targets":                      "build_wait",
		"cross build --target aarch64-unknown-linux-gnu": "build_wait",
		"cargo run --example demo --locked":              "build_wait",

		// Go
		"go build ./...":                    "build_wait",
		"go install ./cmd/tool":             "build_wait",
		"go generate ./...":                 "build_wait",
		"go run ./cmd/wasted-cycles --json": "build_wait",

		// JVM
		"mvn package -DskipTests":    "build_wait",
		"mvn compile":                "build_wait",
		"./gradlew assemble":         "build_wait",
		"javac -d out src/Main.java": "build_wait",
		"sbt compile":                "build_wait",

		// .NET / Swift / Zig / Nim / Haskell / BEAM
		"dotnet build -c Release":          "build_wait",
		"dotnet publish -r linux-x64":      "build_wait",
		"swift build -c release":           "build_wait",
		"zig build -Doptimize=Fast":        "build_wait",
		"nim c -d:release src/app.nim":     "build_wait",
		"stack build":                      "build_wait",
		"cabal build all":                  "build_wait",
		"mix compile --warnings-as-errors": "build_wait",

		// compile-shaped frontend bundlers
		"tsc -b tsconfig.build.json":        "build_wait",
		"vite build --mode production":      "build_wait",
		"npx webpack --mode production":     "build_wait",
		"npx esbuild --bundle src/index.ts": "build_wait",
		"rollup -c rollup.config.js":        "build_wait",
		"next build":                        "build_wait",
		"turbo run build --filter=web":      "build_wait",
		"pnpm run build":                    "build_wait",

		// bare build tools, only trusted at the start of a command
		"make -j16": "build_wait",
		"make":      "build_wait",
		"msbuild App.sln /p:Configuration=Release":      "build_wait",
		"xcodebuild -scheme App -configuration Release": "build_wait",

		// test execution for the same ecosystems
		"ctest --output-on-failure":          "test_wait",
		"cargo test --all-features --locked": "test_wait",
		"cargo nextest run":                  "test_wait",
		"go test ./internal/...":             "test_wait",
		"mvn verify":                         "test_wait",
		"./gradlew test --console=plain":     "test_wait",
		"dotnet test":                        "test_wait",
		"swift test":                         "test_wait",
		"mix test --trace":                   "test_wait",
		"stack test":                         "test_wait",
		"bazel test //pkg:all":               "test_wait",
		"zig build test":                     "test_wait",
		"xcodebuild test -scheme AppTests":   "test_wait",

		// typecheck / lint runs that burn real CPU
		"tsc --noEmit":                              "test_wait",
		"mypy --namespace-packages src":             "test_wait",
		"golangci-lint run ./internal/...":          "test_wait",
		"cargo clippy --all-targets -- -D warnings": "test_wait",
		"clang-tidy src/*.cpp":                      "test_wait",
		"ruff check src tests":                      "test_wait",
		"pnpm --filter web typecheck":               "test_wait",
		"npx eslint src --max-warnings 0":           "test_wait",

		// make targets must reach their own bucket despite the "make " prefix rule
		"make test":  "test_wait",
		"make check": "test_wait",
		"make lint":  "test_wait",

		// restore vs build: a NuGet/Hex restore is a registry fetch, not a compile
		"dotnet restore":        "dependency_wait",
		"nuget restore App.sln": "dependency_wait",
		"mix deps.get":          "dependency_wait",
		"swift package resolve": "dependency_wait",
		"cargo fetch":           "dependency_wait",
		"go mod download":       "dependency_wait",
	}

	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

// TestCommandCompilerFalsePositives locks in the substrings that must NOT be
// read as a compile. Every case is drawn from real agent traces: 79k shell
// commands from ~/.claude/projects, where each of these patterns fired.
func TestCommandCompilerFalsePositives(t *testing.T) {
	notBuild := []string{
		// "incredibuild" is an org, a path and a hostname far more often than a
		// command -- 2081 hits in the corpus, essentially none of them builds.
		"gh pr view 234 --repo incredibuild-rnd/ib-idp --json reviews",
		"ls -lt /etc/incredibuild/log/",
		// bare compiler names inside prose, paths and package installs.
		"echo 'gcc/clang builds add .cxx and emit .o objects'",
		"sudo apt-get install -y cmake ninja-build clang lld",
		"echo 'the old world build should have finished'",
		// tool names that are also config filenames the agent merely reads.
		"cat webpack.config.js",
		"ls node_modules/.bin/esbuild",
		"rm -rf node_modules/.pnpm/esbuild@0.28.0",
		"cat vitest.unit.config.ts",
		"cat jest.config.js",
		"cat .eslintrc.json",
		// "make" inside English and inside other subcommands.
		"echo 'make sure the scan state is reachable'",
		"grep -n install_prefix Makefile",
		"python manage.py makemigrations",
		// gh's statusCheckRollup is not the rollup bundler.
		"gh pr view 22637 --json statusCheckRollup",
		// "across build" contains "cross build".
		"echo 'the flag is threaded across build and test lanes'",
		// compiler caches are discussed far more often than the bare name is run.
		"gh api -X GET search/code -f q='ccache sccache distcc path:.github/workflows'",
		// unittest.mock in a heredoc is source being written, not a suite running.
		"python3 -c 'from unittest.mock import MagicMock'",
	}
	for _, command := range notBuild {
		got, _, _ := classifyCommand(command)
		if got == "build_wait" || got == "test_wait" {
			t.Errorf("classifyCommand(%q) = %q, want neither build_wait nor test_wait", command, got)
		}
	}
}

// TestCommandBuildPrefixIsAnchored proves the anchoring is what makes bare build
// tool names safe: the same token mid-command must not be read as a compile.
func TestCommandBuildPrefixIsAnchored(t *testing.T) {
	if got, _, _ := classifyCommand("make -j8"); got != "build_wait" {
		t.Errorf("anchored make should be a build, got %q", got)
	}
	// Bare "make " is the token that depends entirely on anchoring. Flag-carrying
	// forms like "make -j" stay free substrings so they survive an ssh wrapper.
	if got, _, _ := classifyCommand("echo 'please make it happen'"); got == "build_wait" {
		t.Error("bare make mid-sentence must not be a build")
	}
	if got, _, _ := classifyCommand("ssh builder 'make -j32 all'"); got != "build_wait" {
		t.Errorf("flag-anchored make survives a wrapper, got %q", got)
	}
	// normalizeCommand strips leading cd hops, so anchoring survives them.
	if got, _, _ := classifyCommand("cd /repo/build && ninja -C ."); got != "build_wait" {
		t.Errorf("cd-prefixed ninja should be a build, got %q", got)
	}
}

// TestCommandNoLegacyVerify guards the id rename: nothing may emit "verify".
func TestCommandNoLegacyVerify(t *testing.T) {
	lists := [][]string{
		ciNeedles, containerNeedles, buildNeedles, buildHeadNeedles,
		testNeedles, testHeadNeedles, containerRunNeedles,
		dependencyNeedles, dependencyHeadNeedles,
	}
	for _, needles := range lists {
		for _, needle := range needles {
			category, _, _ := classifyCommand(needle + " x")
			switch category {
			case "verify", "tool_other":
				t.Errorf("needle %q classified as %q", needle, category)
			}
		}
	}
}

// TestCommandPrecedence pins the ordering decisions in bucketFor. Each case is a
// command that more than one rule can claim; the comment is the reason the
// winner wins. These are the assertions that break if the ladder is reordered.
func TestCommandPrecedence(t *testing.T) {
	cases := []struct {
		command string
		want    string
		why     string
	}{
		{"gh run watch 12 && cargo build --release", "ci_wait", "a hosted pipeline poll outranks the local build it mentions"},
		{"docker build -t api .", "build_wait", "an image build of source is a compile, not a container start"},
		{"docker run --rm api pytest -q", "test_wait", "the payload decides: a containerized test run is a test wait"},
		{"kubectl exec -it api -- pytest tests/", "test_wait", "same payload rule for pods"},
		{"docker compose up -d", "container_wait", "nothing runs inside it; the infrastructure is the wait"},
		{"packer build image.pkr.hcl", "container_wait", "packer boots cloud VMs and compiles no source"},
		{"cargo test --all-features", "test_wait", "a cargo build needle must not swallow cargo test"},
		{"make test", "test_wait", "a conventional test target beats the bare make build prefix"},
		{"zig build test", "test_wait", "same, for zig's build-system test step"},
		{"xcodebuild test -scheme AppTests", "test_wait", "same, for xcodebuild"},
		{"go build ./... && go test ./...", "test_wait", "in a build+test chain the wall clock sits in the test"},
		{"bazel test //... --remote_executor=grpc://rbe", "test_wait", "remote execution of a test suite is still a test wait"},
		{"mvn dependency:go-offline", "dependency_wait", "a registry fetch beats the bare mvn build prefix"},
		{"terraform init -upgrade", "dependency_wait", "init downloads providers; it provisions nothing"},
		{"terraform apply -auto-approve", "container_wait", "apply is the provisioning wait"},
		{"./gradlew build --refresh-dependencies", "build_wait", "the compile still happens; the flag does not demote it"},
		{"curl -sf http://localhost:8080/healthz", "test_wait", "loopback is the agent checking its own server"},
		{"curl -sSL https://registry.example.com/pkg.tgz", "dependency_wait", "a real remote fetch stays a dependency wait"},
		{"until kubectl get pods | grep -q running; do sleep 2; done", "container_wait", "poll primitive plus infra target"},
		{"docker --version", "explore", "a sub-second capability probe is not a container wait"},
		{"npm run lint", "test_wait", "the script name decides, not the package manager"},
		{"npm run dev", "tool_other", "a dev server is a local process, not a wait on someone else's compute"},
	}

	for _, testCase := range cases {
		got, _, _ := classifyCommand(testCase.command)
		if got != testCase.want {
			t.Errorf("classifyCommand(%q) = %q, want %q (%s)", testCase.command, got, testCase.want, testCase.why)
		}
	}
}

// TestCommandDependencyCatalog covers the package-manager and network lens.
func TestCommandDependencyCatalog(t *testing.T) {
	cases := map[string]string{
		"npm install":                         "dependency_wait",
		"npm ci --prefer-offline":             "dependency_wait",
		"pnpm add -D vitest":                  "dependency_wait",
		"yarn install --frozen-lockfile":      "dependency_wait",
		"bun install":                         "dependency_wait",
		"pip install -r requirements.txt":     "dependency_wait",
		"uv sync --frozen":                    "dependency_wait",
		"uv lock":                             "dependency_wait",
		"poetry lock --no-update":             "dependency_wait",
		"conda env create -f environment.yml": "dependency_wait",
		"apk add --no-cache git":              "dependency_wait",
		"apt-get install -y build-essential":  "dependency_wait",
		"brew install ripgrep":                "dependency_wait",
		"gem install bundler":                 "dependency_wait",
		"composer update":                     "dependency_wait",
		"cargo fetch":                         "dependency_wait",
		"cargo vendor":                        "dependency_wait",
		"go mod tidy":                         "dependency_wait",
		"rustup component add clippy":         "dependency_wait",
		"mise install":                        "dependency_wait",
		"git clone https://github.com/x/y":    "dependency_wait",
		"git ls-remote --heads origin":        "dependency_wait",
		"git submodule update --init":         "dependency_wait",
		"scp build.tar host:/tmp":             "dependency_wait",
		"docker pull alpine:3":                "dependency_wait",
		"ollama pull llama3":                  "dependency_wait",
		"huggingface-cli download org/model":  "dependency_wait",
		"aws s3 sync ./dist s3://bucket":      "dependency_wait",
		"helm repo update":                    "dependency_wait",
		"go list -m -u all":                   "dependency_wait",

		// registry *reads* that return immediately are exploration, not a wait.
		"npm view react version": "explore",
		"pip list":               "explore",
		"cargo tree":             "explore",
		"command -v rg":          "explore",
		"uv --version":           "explore",
	}

	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

// TestCommandNegatives is the false-positive wall. Every entry is a real shape
// from agent traces where a needle appears inside text the agent is printing,
// searching, editing or naming -- never inside work the machine is doing. A
// wrong answer here silently mislabels the user's own data, which is worse than
// missing the command entirely.
func TestCommandNegatives(t *testing.T) {
	blocked := set("build_wait", "test_wait", "ci_wait", "container_wait", "dependency_wait")
	commands := []string{
		// the command as an argument to a printer or a searcher.
		"echo go build",
		"echo 'npm install && npm test'",
		"printf 'docker compose up\\n' >> docs/setup.txt",
		"rg 'pip install' README.md",
		"grep -rn 'uv sync' docs/",
		"grep -rn 'cargo build' .github/workflows/ci.yml",
		"sed -n '1,40p' Dockerfile",
		"sed -n '1,60p' docker-compose.yml",
		"cat .gitlab-ci.yml",
		"head -20 Makefile",
		"awk '/go test/ {print}' notes.md",
		// prose that happens to start with a build tool.
		"make sure the tests pass before pushing",
		"echo 'make sure the scan state is reachable'",
		// paths and identifiers that contain a tool name.
		"ls /opt/docker-compose-backup",
		"ls node_modules/.bin/esbuild",
		"cat vitest.config.ts",
		"cat jest.config.js",
		"cat .eslintrc.json",
		"cat webpack.config.js",
		"git log --author=travis --oneline -5",
		"gh pr view 22637 --json statusCheckRollup",
		// English words that contain a tool or verb name.
		"rg 'await connection' src/",
		"rg -n 'artifact|compact|transact' internal/",
		"echo 'the flag is threaded across build and test lanes'",
		"python3 -c 'from unittest.mock import MagicMock'",
		// probes and reads of the very tools above.
		"docker compose --version",
		"terraform validate",
		"helm template ./chart",
	}

	for _, command := range commands {
		got, _, _ := classifyCommand(command)
		if blocked[got] {
			t.Errorf("classifyCommand(%q) = %q, want a non-blocking category", command, got)
		}
	}
}

// TestCommandNormalization proves the rewrites that happen before any needle is
// scanned. Each pair must land in the same bucket as its bare form.
func TestCommandNormalization(t *testing.T) {
	cases := map[string]string{
		// git global flags hide the subcommand from every git needle.
		"git -C /srv/repo fetch --all":                  "dependency_wait",
		"git -C /srv/repo status --short":               "explore",
		"git -c user.name=ci commit -m wip":             "edit",
		"git --git-dir=/srv/repo/.git push origin main": "dependency_wait",
		// leading environment assignments.
		"ISLO_ENV=local uv run pytest -q":             "test_wait",
		"CGO_ENABLED=0 GOOS=linux go build ./cmd/app": "build_wait",
		// runner prefixes carry no information of their own.
		"uv run ruff check src":         "test_wait",
		"npx jest --runInBand":          "test_wait",
		"pnpm exec vitest run":          "test_wait",
		"poetry run pytest tests/unit":  "test_wait",
		"bundle exec rspec spec/models": "test_wait",
		// wrappers and cd hops.
		"cd /repo && sudo make -j8 all":            "build_wait",
		"bash -lc 'cd /repo/api && go test ./...'": "test_wait",
		"time cargo build --release":               "build_wait",
	}

	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}

// TestCommandRetryKeys pins the dedup key: it exists for exactly the buckets
// where doing the same work twice is waste the user can act on, and it survives
// the formatting differences that make the same command look different.
func TestCommandRetryKeys(t *testing.T) {
	keyed := map[string]string{
		"go test ./...":         "test_wait",
		"cargo build --release": "build_wait",
		"gh run watch 12":       "ci_wait",
	}
	for command, category := range keyed {
		got, _, key := classifyCommand(command)
		if got != category {
			t.Fatalf("classifyCommand(%q) = %q, want %q", command, got, category)
		}
		if key == "" {
			t.Errorf("classifyCommand(%q) produced no retry key for %q", command, category)
		}
	}

	unkeyed := []string{
		"docker compose up -d", // restarting a stack is normal, not waste
		"npm install",          // so is installing twice in two worktrees
		"kubectl apply -f k8s/",
		"cat README.md",
		"git commit -m wip",
	}
	for _, command := range unkeyed {
		category, _, key := classifyCommand(command)
		if key != "" {
			t.Errorf("classifyCommand(%q) = %q with key %q, want no key", command, category, key)
		}
	}

	// The key is built from the normalized command, so formatting cannot split
	// one repeated command into two distinct keys.
	base := keyOf(t, "go test ./internal/...")
	for _, variant := range []string{
		"GO TEST ./internal/...",
		"cd /repo && go   test ./internal/...",
		"GOFLAGS=-count=1 go test ./internal/...",
		"bash -lc 'go test ./internal/...'",
	} {
		if got := keyOf(t, variant); got != base {
			t.Errorf("key(%q) = %q, want %q", variant, got, base)
		}
	}

	// Two long commands under a shared path prefix must not collide once the
	// key is truncated: that collision reports unrelated work as a retry.
	prefix := "go test github.com/example/monorepo/services/platform/ingestion/pipeline/internal/"
	first := keyOf(t, prefix+"transform/stage_one -run TestPipelineTransformsRecordsEndToEnd -count=1")
	second := keyOf(t, prefix+"transform/stage_two -run TestPipelineTransformsRecordsEndToEnd -count=1")
	if first == second {
		t.Error("two different long test commands share a retry key")
	}
}

func keyOf(t *testing.T, command string) string {
	t.Helper()
	_, _, key := classifyCommand(command)
	if key == "" {
		t.Fatalf("classifyCommand(%q) produced no key", command)
	}
	return key
}

// TestShellActionJSWrapper covers Codex's `exec` tool, which carries a
// JavaScript program instead of a command string.
func TestShellActionJSWrapper(t *testing.T) {
	batch := `const [a, b, c] = await Promise.all([
  tools.exec_command({ cmd: "git status --short && git rev-parse HEAD" }),
  tools.exec_command({cmd:"gh pr view 22637"}),
  tools.exec_command({cmd: "gh pr checks 22637"}),
]);`
	got := toolAction("exec", map[string]any{"command": batch})
	if got.Category != "ci_wait" {
		t.Errorf("batch category = %q, want ci_wait (the batch blocks until its slowest leg settles)", got.Category)
	}
	if got.Key == "" || strings.Contains(got.Key, "tools.exec_command") {
		t.Errorf("batch key = %q, want the extracted commands and no JS boilerplate", got.Key)
	}
	if !strings.Contains(got.Key, "gh pr checks 22637") {
		t.Errorf("batch key = %q, want every leg represented", got.Key)
	}

	// The same command written with different spacing must produce one key.
	spaced := toolAction("exec", map[string]any{"command": `await tools.exec_command({ cmd: "cargo test --all" })`})
	tight := toolAction("exec", map[string]any{"command": `await tools.exec_command({cmd:"cargo test --all"})`})
	if spaced.Key != tight.Key || spaced.Key == "" {
		t.Errorf("keys differ across JS spacing: %q vs %q", spaced.Key, tight.Key)
	}
	if spaced.Category != "test_wait" {
		t.Errorf("category = %q, want test_wait", spaced.Category)
	}

	// A command past the 400-char needle window is still classified: extraction
	// happens before the window is applied.
	padded := "// " + strings.Repeat("x", 600) + "\nawait tools.exec_command({cmd: \"cargo build --release\"});"
	if got := toolAction("exec", map[string]any{"command": padded}); got.Category != "build_wait" {
		t.Errorf("padded batch category = %q, want build_wait", got.Category)
	}

	// Escapes survive the round trip.
	escaped := `await tools.exec_command({cmd: "pytest -k \"not slow\" tests/"})`
	if got := toolAction("exec", map[string]any{"command": escaped}); got.Category != "test_wait" {
		t.Errorf("escaped batch category = %q, want test_wait", got.Category)
	}

	// exec_command itself carries clean JSON arguments and must never be
	// re-parsed as a program.
	if got := toolAction("exec_command", map[string]any{"command": "cargo test --all"}); got.Category != "test_wait" {
		t.Errorf("exec_command category = %q, want test_wait", got.Category)
	}
	// A shell tool is never treated as a program, so a quoted cmd: in a shell
	// string cannot be lifted out of its quotes.
	if got := toolAction("bash", map[string]any{"command": `echo 'cmd: "cargo build"'`}); got.Category == "build_wait" {
		t.Error("a quoted cmd: inside a shell echo must not be extracted")
	}
}

// TestShellActionJSWrapperIsSafe covers adversarial payloads: extraction is a
// bounded regex over a bounded string, so nothing here may panic or hang.
func TestShellActionJSWrapperIsSafe(t *testing.T) {
	payloads := []string{
		`await tools.exec_command({cmd: "go test ./...`,               // unterminated literal
		"await tools.exec_command({cmd: `sed -n '1,${n}p' ${file}`})", // template literal
		`await tools.exec_command({cmd: someVariable})`,               // indirection
		`const cmd = {}; cmd: cmd: cmd: "x"`,                          // degenerate
		strings.Repeat(`await tools.exec_command({cmd:"go test ./..."});`, 5000),
		strings.Repeat(`{cmd:"`, 50000),
		"",
	}
	for _, payload := range payloads {
		got := toolAction("exec", map[string]any{"command": payload})
		if got.Kind != kindToolCall {
			t.Errorf("payload %.30q produced kind %v", payload, got.Kind)
		}
	}
	if commands := extractShellCommands(strings.Repeat(`{cmd:"go build"},`, 100)); len(commands) > maxJSCommands {
		t.Errorf("extracted %d commands, want at most %d", len(commands), maxJSCommands)
	}
}

// TestToolActionBlockedOnBackgroundProcess pins the wait / write_stdin
// decision. Both block on a process the agent started, but which compute is
// only knowable by correlating session_id with the exec_command that started
// it -- state this classifier does not have. They are therefore labelled as a
// wait and left out of the wasted-cycle buckets: under-count, never lie.
func TestToolActionBlockedOnBackgroundProcess(t *testing.T) {
	for _, tool := range []string{"wait", "write_stdin", "writeStdin"} {
		got := toolAction(tool, map[string]any{"session_id": 77173, "yield_time_ms": 1000})
		if got.Category != "tool_other" {
			t.Errorf("toolAction(%q) = %q, want tool_other", tool, got.Category)
		}
		if got.Label != "blocked on background process" {
			t.Errorf("toolAction(%q) label = %q, want it to read as a wait", tool, got.Label)
		}
		if got.Key != "" {
			t.Errorf("toolAction(%q) must not carry a retry key", tool)
		}
	}
}

// TestToolActionCategoryCoverage checks that every category this file is
// allowed to emit is actually reachable, and that it emits nothing else.
func TestToolActionCategoryCoverage(t *testing.T) {
	allowed := set("reasoning", "explore", "edit", "tool_other", "build_wait", "test_wait", "ci_wait", "container_wait", "dependency_wait", "agent_wait")
	seen := map[string]bool{}

	for tool, input := range map[string]map[string]any{
		"read":      nil,
		"edit_file": nil,
		"task":      nil,
		"todowrite": nil,
		"wait":      nil,
	} {
		seen[toolAction(tool, input).Category] = true
	}
	for _, command := range []string{
		"cargo build --release", "go test ./...", "gh run watch 1",
		"docker compose up -d", "npm install", "cat README.md",
		"git commit -m wip", "hostname",
	} {
		category, _, _ := classifyCommand(command)
		seen[category] = true
	}

	for category := range seen {
		if !allowed[category] {
			t.Errorf("emitted unknown category %q", category)
		}
	}
	for category := range allowed {
		if !seen[category] {
			t.Errorf("category %q is unreachable", category)
		}
	}
}

// TestCommandLocalChecks pins the two escape hatches for commands that name a
// heavy tool but return immediately.
func TestCommandLocalChecks(t *testing.T) {
	cases := map[string]string{
		"ansible-playbook site.yml --syntax-check": "explore",
		"helm upgrade api ./chart --dry-run":       "explore",
		"kubectl apply -f k8s/ --dry-run=client":   "explore",
		"terraform --version":                      "explore",
		"docker buildx --help":                     "explore",
		"command -v uv":                            "explore",
	}
	for command, want := range cases {
		got, _, _ := classifyCommand(command)
		if got != want {
			t.Errorf("classifyCommand(%q) = %q, want %q", command, got, want)
		}
	}
}
