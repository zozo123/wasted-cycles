package analyze

import "testing"

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
	for _, needles := range [][]string{ciNeedles, containerNeedles, buildNeedles, testNeedles, containerRunNeedles} {
		for _, needle := range needles {
			category, _, _ := classifyCommand(needle + " x")
			switch category {
			case "verify", "tool_other":
				t.Errorf("needle %q classified as %q", needle, category)
			}
		}
	}
}
