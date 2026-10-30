# rooket-action Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish `jhoblitt/rooket-action`, a JavaScript action backed by a Go
helper. It leaves a GitHub-hosted Ubuntu runner ready to run `rooket up`, tears
down what rooket left behind after the job, and is versioned by
semantic-release.

**Architecture:** `action.yml` runs `js/main.mjs` and `js/post.mjs`. They find
Go, build `cmd/rooket-action` from the action's own source tree, and exec its
`setup` and `teardown` subcommands. Every decision is made in Go, and the
JavaScript only locates Go, builds the helper, and passes inputs through as
flags.

**Tech Stack:**
- Go 1.27, cobra, Ginkgo v2 and Gomega
- Node 24, standard library only, for the action
- semantic-release, from npm, for releases only
- GitHub Actions

**Spec:** `docs/superpowers/specs/2026-10-29-rooket-action-design.md`.

**Revised 2026-10-30.** rooket now publishes semantic-release binaries, so the
action downloads a pinned rooket release instead of running `go install` on a
`go.mod` pin. Tasks 3, 6, 7 and 9 changed. Task 3 needs rooket's first release
to exist, so the execution order is 0, 1, 2, 4, 5, 3, 6, 7, 8, 9. Tasks 0 and 1
are done, and the spike's results are in the spec.

## Global Constraints

**Ownership and process**
- Repository `jhoblitt/rooket-action`, default branch `main`, visibility
  public. Confirm visibility with the owner at creation; never default it.
- Execution runs in a session started in the rooket-action clone, so that
  `EnterWorktree` can make the branch worktrees there. This plan makes no
  change to jhoblitt/rooket. rooket's releases are a separate piece of work in
  that repository.
- Every outward action (repo creation, push, PR) waits for the owner's go-ahead
  for that action. PRs open as drafts, assigned to the author. No GitHub
  comment is posted without an explicit instruction.
- Commits are conventional and commitlint-clean. A rooket pin bump is committed
  as `fix(deps): …`.

**Go**
- Follow go-conventions: `go 1.27` (minor only, no `toolchain` line), cobra,
  Ginkgo and Gomega, `make check` green, and `log/slog` per
  `references/logging.md`.
- GitHub workflow commands (`::warning::`, `::group::`) are protocol output on
  stdout, not logs.
- The default rooket release and the sha256 of its linux/amd64 archive are
  compiled into the helper (Task 3). `go.mod` does not require rooket.
- Every `go` invocation the action makes, the helper build included, runs with
  `GOTOOLCHAIN=auto`, whatever the consumer set.

**JavaScript**
- `action.yml`: `runs.using: node24`, `main: js/main.mjs`, `post: js/post.mjs`,
  and no `post-if`, so post defaults to `always()`.
- The JavaScript has no npm dependencies and no build step.

**Runner prep values**
- Guard: `GITHUB_ACTIONS=true`, `RUNNER_ENVIRONMENT=github-hosted`,
  `RUNNER_OS=Linux` and `RUNNER_ARCH=X64`, checked before anything else.
- Binary directory: `$RUNNER_TEMP/rooket-action/bin`. It holds the helper,
  rooket, kind, kubectl and helm, and only it is added to `GITHUB_PATH`.
- rooket install, chosen by the inputs:
  - `rooket-path` set: copy that file in. Combining it with `rooket-version`
    or `rooket-sha256` is an error naming both inputs.
  - Neither `rooket-version` nor `rooket-sha256` set: download the pinned
    release's archive and check it against the pinned sha256.
  - Both set: download that release's archive and check it against the given
    sha256.
  - `rooket-version` alone: `go install github.com/jhoblitt/rooket@<v>`.
  - `rooket-sha256` alone: an error.
- Disk: `min-free-gb`, or else 30 GiB plus 10 GiB × `workers`, free on `/`.
  When short, `sudo rm -rf` exactly `/usr/share/dotnet`,
  `/usr/local/lib/android`, `/opt/ghc`, `/opt/hostedtoolcache/CodeQL`,
  `/usr/local/.ghcup`, `/usr/lib/jvm`, `/usr/share/swift` and
  `/usr/local/share/powershell`. Still short afterwards is a warning.
- Pins, copied from rgw-go's `.github/actions/rooket-cluster/action.yml`:

  | Tool | Version | URL | sha256 | Second source |
  |---|---|---|---|---|
  | kind | v0.32.0 | `https://kind.sigs.k8s.io/dl/v0.32.0/kind-linux-amd64` | `50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54` | kind's GitHub release |
  | kubectl | v1.33.1 | `https://dl.k8s.io/release/v1.33.1/bin/linux/amd64/kubectl` | `5de4e9f2266738fd112b721265a0c1cd7f4e5208b670f811861f699474a100a3` | Kubernetes changelog sha512 of the client tarball |
  | helm | v3.20.1 | `https://get.helm.sh/helm-v3.20.1-linux-amd64.tar.gz`, member `linux-amd64/helm` | `0165ee4a2db012cc657381001e593e981f42aa5707acdd50658326790c9d0dc3`, of the archive | helm's GitHub release |
  | rooket | rooket's newest release when Task 3 runs | rooket's release linux/amd64 archive, with the binary as its member. The name template comes from rooket's release config | of the archive, from the release's `checksums.txt` | that file's cosign keyless bundle, checked with `cosign verify-blob` against rooket's release workflow identity |

- iSCSI, each command run with `sudo`, in this order:
  1. `apt-get update`
  2. `apt-get install -y open-iscsi targetcli-fb lvm2`
  3. `modprobe -a configfs target_core_mod target_core_file iscsi_target_mod iscsi_tcp`
  4. `systemctl enable --now iscsid`
- Exports: the binary directory to `GITHUB_PATH`, and `ROOKET_ENGINE=docker`
  to `GITHUB_ENV`.
- Post: `<rooket> down --all --force --delete-disks` with
  `ROOKET_ENGINE=docker`. A failure prints `::warning::` and exits 0.

**Inputs and outputs**
- Inputs (all strings): `rooket-version` (empty means the pinned release),
  `rooket-sha256` (empty), `rooket-path`, `workers` (`1`), `min-free-gb`
  (empty), `iscsi` (`true`) and `teardown` (`true`).
- Outputs: `path` and `rooket-version`.

**Workflows**
- Every `uses:` is pinned to a SHA with a `# vX.Y.Z` comment, through pinact.
- Every workflow passes actionlint with shellcheck, sets top-level
  `permissions: contents: read`, has a concurrency block, and sets
  `timeout-minutes` on every job. Every checkout uses
  `persist-credentials: false`.

## Review Focus

Each of these is tested in the task named:

1. **A consumer whose toolchain policy blocks the 1.27 fetch** sets
   `GOTOOLCHAIN=local`, or runs `setup-go` with an older Go. Expected: the
   action still builds and installs, because its `go` invocations force
   `GOTOOLCHAIN=auto`. *(Task 6)*
2. **`rooket-path` is relative and lost its exec bit,** for example `./rooket`
   downloaded from an artifact, which rooket's own CI does. Expected: it
   resolves against the working directory and is copied in with mode 0755.
   *(Task 6)*
3. **A string input outside the expected form:** `iscsi: False` or `TRUE`,
   `teardown: no`, `workers: 0` or `three`, or `min-free-gb: -5`. Expected:
   booleans are `true`/`false` case-insensitively, and anything else is an
   error naming the input. `workers` is at least 1. `min-free-gb` is empty or
   at least 1. *(Task 6)*
4. **`rooket-version` written in another form:** a 40-hex SHA, a branch name, a
   pseudo-version, or the value padded with spaces. Expected:
   - Without `rooket-sha256`, it is trimmed and passed verbatim to
     `go install …@<v>`.
   - With `rooket-sha256`, that release's archive is downloaded and checked.
   - Empty means the pinned release.
   - A consumer hash that does not match is an error naming both hashes, and is
     not retried.
   *(Tasks 3 and 6)*
5. **The action used twice in one job.** Expected: the second `setup`
   overwrites the binaries in place and succeeds, and the second post-step
   teardown finds nothing and exits 0. *(Tasks 2 and 6)*

---

### Task 0: Create and scaffold the repository

**Files:** the github-new-repo and go-new-project scaffolds, plus
`docs/superpowers/specs/2026-10-29-rooket-action-design.md` and this plan.
Both documents were copied into the otherwise empty `~/github/rooket-action`
before the repository existed.

- [ ] **Step 1:** Ask the owner to confirm visibility `--public` and to approve
  creating the repository. Stop until both are given.
- [ ] **Step 2:** Run `github-conventions:github-new-repo` for
  `jhoblitt/rooket-action`: the empty repository, the ruleset,
  `--delete-branch-on-merge`, the empty root commit on `main`, and the canon
  files on an `init` branch.
- [ ] **Step 3:** On the same `init` branch, run
  `go-conventions:go-new-project` with module `github.com/jhoblitt/rooket-action`
  and binary `rooket-action`.
  - Delete the goreleaser config and the `v*`-tag release workflow it writes,
    because semantic-release replaces them (Task 8, the owner's decision).
  - Keep `ci.yml`, the Makefile and `.golangci.yml`.
- [ ] **Step 4:** Commit the spec and this plan, already in
  `docs/superpowers/`, on `init` with the scaffold. The SDD workspace under
  `.superpowers/` must stay out of every commit.
- [ ] **Step 5:** Run `make check`. Expected: green.
- [ ] **Step 6:** Commit, push `init`, and open the draft init PR. Watch CI to
  green, and merge once the owner approves.

### Task 1: Spike — verify the spec's five unknowns

A throwaway branch `spike/runner-facts`, never merged. It needs pushes, so get
the owner's go-ahead first.

**Files:** `action.yml` and `js/main.mjs`, `js/post.mjs` and
`.github/workflows/spike.yml`, on the spike branch only.

- [ ] **Step 1:** Write a JavaScript action that has no dependencies. Its
  `main` prints:
  - the directory listing two levels above its own script;
  - `process.env.PATH`, and `go version` from `PATH` if one is there;
  - the entries under `$RUNNER_TOOL_CACHE/go`;
  - `RUNNER_ENVIRONMENT`, `RUNNER_OS` and `RUNNER_ARCH`;
  - the wall time of `GOTOOLCHAIN=auto go install github.com/jhoblitt/rooket@main`
    and of a `go build` of a stub `cmd/rooket-action`, both from a cold cache.

  Its `post` prints `post ran`.
- [ ] **Step 2:** Write `spike.yml`, triggered on push to `spike/**`, with two
  jobs. One runs `uses: ./`. The other runs
  `uses: jhoblitt/rooket-action@<spike commit sha>`.
- [ ] **Step 3:** Push and read both jobs' logs. Record each answer in the
  spec's "Verify before building" section:
  1. The remote job's listing holds the whole repository, `go.mod` and `cmd/`
     included.
  2. Local `post` ran.
  3. Go is reachable, and which way (`PATH` or tool cache).
  4. The three variables read `github-hosted`, `Linux` and `X64`.
  5. The timings.
- [ ] **Step 4: Gate.** If (1) or (3) failed, or (5) took more than two
  minutes, stop and take the result to the owner. Otherwise, if Go was on
  `PATH`, note that Task 7's `findGo` is `PATH`-only, with no tool-cache scan.
- [ ] **Step 5:** Delete the spike branch. Commit the spec update to `main`
  through a small `docs:` PR.

### Task 2: Pinned tool downloads

**Files:**
- Create: `internal/tools/pins.go`, `internal/tools/install.go`
- Test: `internal/tools/install_test.go`, plus the suite file

**Interfaces — Produces:**
- `type Pin struct { Name, Version, URL, SHA256, Member, SecondSource string }`.
  `Member` is empty for a bare binary, and names the archive path for helm.
- `func Pins() []Pin`, returning kind, kubectl and helm with the Global
  Constraints values.
- `type Retry struct { Attempts int; PerRequest time.Duration; Backoff time.Duration }`
  and `var DefaultRetry Retry`. The values are the builder's call.
- `func Install(ctx context.Context, c *http.Client, p Pin, binDir string, r Retry) error`
  writes `binDir/<Name>` with mode 0755. The write goes through a temp file and
  a rename, so a running copy is replaced rather than truncated.

- [ ] **Step 1: Write the failing specs.** They use an `httptest.Server` and a
  `Pin` whose URL points at it:
  - The right bytes install at `binDir/<Name>` with mode 0755.
  - A wrong sha256 returns an error naming the pin and both hashes, after
    exactly one request, because a mismatch is not retried.
  - A 503 twice and then 200, with `Attempts: 3`, installs after 3 requests.
  - A 503 three times with `Attempts: 3` returns an error naming the URL.
  - A handler that sleeps past `PerRequest` counts as a failed attempt and is
    retried.
  - A helm pin extracts only `linux-amd64/helm` from a `tar.gz` fixture. A
    fixture missing that member is an error naming it.
  - Installing twice over an existing file succeeds, and the file holds the
    second content.
  - `Pins()` returns names kind, kubectl and helm, each with a 64-hex `SHA256`
    and a non-empty `SecondSource`.
- [ ] **Step 2:** Run `go test ./internal/tools/...`. Expected: FAIL, undefined
  symbols.
- [ ] **Step 3:** Implement `Pins` and `Install`.
- [ ] **Step 4:** Run `go test ./internal/tools/...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(tools): download pinned kind, kubectl and helm`.

### Task 3: The rooket release pin, and kubectl following its node image

This task blocks on rooket's first release. Run it after Task 5.

**Files:**
- Create: `internal/tools/rooket.go`, `internal/tools/nodeimage.go`
- Test: `internal/tools/rooket_test.go`, `internal/tools/nodeimage_test.go`

**Interfaces:**
- Consumes: `Pin`, `Pins()` and `Install` from Task 2.
- Produces:
  - `func Rooket(version, sha256 string) Pin`. It builds the pin for one rooket
    release: `Name` `rooket`, the release archive's URL and member from
    rooket's release name template, and the given hash.
  - `func DefaultRooket() Pin`, which is `Rooket` applied to the compiled-in
    default version and hash.
  - `Pins()` is unchanged: it still returns kind, kubectl and helm only. rooket
    is installed by Task 6, according to the inputs.
  - `func NodeImageTag(rooketModuleDir string) (string, error)`. It parses
    `<dir>/cmd/nodeimage.go` with `go/parser` and returns the tag of the
    string constant `defaultNodeImage`, for example `v1.33.1` from
    `kindest/node:v1.33.1@sha256:…`.

- [ ] **Step 1: Pin the release.** Take rooket's newest release, and fetch its
  `checksums.txt` and that file's cosign bundle. Run `cosign verify-blob`
  against the certificate identity of rooket's release workflow and the issuer
  `https://token.actions.githubusercontent.com`. Copy the linux/amd64
  archive's sha256 from the verified file. The report records each command and
  its output.
- [ ] **Step 2: Write the failing specs.**
  - `Rooket("vX.Y.Z", h)` yields the URL and member that rooket's name template
    gives for that version, and carries `h`.
  - `DefaultRooket()` has a `vMAJOR.MINOR.PATCH` version with major 1, a
    64-hex `SHA256`, and a non-empty `SecondSource`.
  - On a fixture directory whose `cmd/nodeimage.go` declares the constant,
    `NodeImageTag` returns `v1.33.1`.
  - A fixture without the constant returns an error naming both the file and
    `defaultNodeImage`. That message is what someone sees when rooket renames
    it.
  - **Pairing spec:** it resolves the pinned release's source directory from
    the `Dir` field of
    `go mod download -json github.com/jhoblitt/rooket@<DefaultRooket().Version>`.
    That command fetches the source even on a cold cache, and `go.mod` does
    not require rooket. The spec asserts that the kubectl pin's `vMAJOR.MINOR`
    equals that of `NodeImageTag`. Its failure message names both versions and
    says to bump kubectl's pin, with its hash, in `pins.go`.
- [ ] **Step 3:** Run `go test ./internal/tools/...`. Expected: FAIL.
- [ ] **Step 4:** Implement `Rooket`, `DefaultRooket` and `NodeImageTag`.
- [ ] **Step 5:** Run `make check`. Expected: PASS. `go.mod` gains no rooket
  requirement.
- [ ] **Step 6:** Commit
  `feat(tools): pin rooket's release and check kubectl against its node image`.

### Task 4: Runner guard and disk

**Files:**
- Create: `internal/prepare/guard.go`, `internal/prepare/disk.go`,
  `internal/sys/runner.go`
- Test: `internal/prepare/guard_test.go`, `internal/prepare/disk_test.go`,
  plus the suite file

**Interfaces — Produces:**
- `sys.Runner` interface:
  - `Run(ctx context.Context, env []string, name string, args ...string) error`
    streams to the process's stdout and stderr.
  - `Output(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)`
    captures stdout, runs in `dir`, and passes stderr through.
  - In both, `env` holds extra `KEY=VALUE` entries over the parent's
    environment.
- `sys.Exec`, the real Runner.
- `sys.Fake`, which records `Calls []sys.Call{Dir, Env, Name, Args}`, returns
  scripted output per call index, and fails on a scripted call index.
- `func Guard(getenv func(string) string) error`.
- `func Threshold(workers, minFreeGiB int) uint64`, in bytes. `minFreeGiB` 0
  means the formula.
- `func EnsureDisk(ctx context.Context, r sys.Runner, free func(path string) (uint64, error), need uint64, warn io.Writer) error`.

- [ ] **Step 1: Write the failing specs.**
  - `Guard` with all four variables at their required values returns nil.
  - A table with each variable missing or wrong in turn, for example
    `RUNNER_ENVIRONMENT=self-hosted` or `RUNNER_ARCH=ARM64`, returns an error
    naming that variable and its value.
  - `Threshold(1, 0)` is 40 GiB, `Threshold(3, 0)` is 60 GiB, and
    `Threshold(3, 25)` is 25 GiB.
  - `EnsureDisk` with enough free space makes no runner calls.
  - When short, it makes exactly one call: `sudo rm -rf` with the eight paths in
    the Global Constraints order.
  - When still short after cleanup, it writes `::warning::` to `warn` with the
    free and needed GiB, and returns nil.
  - When `free` errors, `EnsureDisk` returns that error.
- [ ] **Step 2:** Run `go test ./internal/...`. Expected: FAIL.
- [ ] **Step 3:** Implement. `free` is `statfs` on `/` (`Bavail × Bsize`) in
  production.
- [ ] **Step 4:** Run `go test ./internal/...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(prepare): refuse non-hosted runners and free disk when short`.

### Task 5: iSCSI, exports, and the prepare sequence

**Files:**
- Create: `internal/prepare/iscsi.go`, `internal/prepare/prepare.go`,
  `internal/ghfiles/ghfiles.go`
- Test: `internal/prepare/iscsi_test.go`, `internal/prepare/prepare_test.go`,
  `internal/ghfiles/ghfiles_test.go`

**Interfaces:**
- Consumes: Task 2's `Pins` and `Install`, and Task 4's `Guard`, `Threshold`,
  `EnsureDisk` and `sys.Runner`.
- Produces:
  - `func ghfiles.Set(path, key, value string) error` appends `key=value` and
    rejects a key or value containing a newline.
  - `func ghfiles.AddPath(path, dir string) error`.
  - `type prepare.Config struct { Workers, MinFreeGiB int; ISCSI bool; BinDir string }`
  - `type prepare.Deps struct { Runner sys.Runner; Getenv func(string) string; Free func(string) (uint64, error); HTTP *http.Client; Retry tools.Retry; Out io.Writer }`
  - `func prepare.Run(ctx context.Context, cfg Config, d Deps) error` runs, in
    order: guard, disk, each pin's `Install`, iSCSI if enabled, then exports.
    It reads `GITHUB_PATH` and `GITHUB_ENV` through `Getenv`.

- [ ] **Step 1: Write the failing specs.**
  - With `ISCSI: true`, the Fake records exactly the four commands of the Global
    Constraints, in order, each run as `sudo`.
  - With `ISCSI: false`, it records no iSCSI calls.
  - A Fake failing `modprobe` makes `Run` return an error naming the command,
    and `systemctl` is never called.
  - A refused guard means zero runner calls, zero HTTP requests, and the
    `GITHUB_PATH` and `GITHUB_ENV` files untouched.
  - After success, the `GITHUB_PATH` file's last line is `BinDir`, and the
    `GITHUB_ENV` file contains `ROOKET_ENGINE=docker`.
  - A failed download means neither file is written.
  - With `GITHUB_PATH` unset while the guard passes, `Run` returns an error
    naming `GITHUB_PATH` before any download.
  - `ghfiles.Set` with a value containing `\n` returns an error and writes
    nothing.
- [ ] **Step 2:** Run `go test ./internal/...`. Expected: FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** Run `go test ./internal/...`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(prepare): set up iSCSI and export the tool path and engine`.

### Task 6: rooket install and the `setup` and `teardown` subcommands

**Files:**
- Create: `internal/rooket/install.go`, `cmd/rooket-action/setup.go`,
  `cmd/rooket-action/teardown.go`, `cmd/rooket-action/inputs.go`
- Modify: `cmd/rooket-action/main.go` and its root command, from the scaffold
- Test: `internal/rooket/install_test.go`, `cmd/rooket-action/*_test.go`

**Interfaces:**
- Consumes:
  - Task 5's `prepare.Run`, `Config`, `Deps` and `ghfiles`;
  - Task 4's `sys.Runner`;
  - Task 3's `Rooket` and `DefaultRooket`;
  - Task 2's `Install` and `Retry`.
- Produces the CLI contract that Task 7's JavaScript calls:
  - `rooket-action setup --go <path> --bin-dir <dir> --rooket-version <s> --rooket-sha256 <s> --rooket-path <s> --workers <s> --min-free-gb <s> --iscsi <s>`
  - `rooket-action teardown --rooket <path> --teardown <s>`
  - Input flags are strings, parsed by `inputs.go`. `ParseBool` accepts
    `true`/`false` case-insensitively. Otherwise each error names the input in
    the form `input iscsi: "no" is not true or false`.
  - `func rooket.Install(...) (path string, err error)` takes:
    - the runner;
    - the HTTP client and `tools.Retry` it hands to `tools.Install`;
    - `rooket.Options{ Go, BinDir, Version, SHA256, Path string }`.
    The exact parameter list is the builder's call.
  - `func rooket.Version(ctx context.Context, bin string) (string, error)`
    returns the second word of the first line of `rooket version`.

`setup`:
1. Install rooket.
2. Run `prepare.Run`.
3. Append `path` and `rooket-version` to `GITHUB_OUTPUT`.
4. Last, append `helper=<os.Executable()>` and `rooket=<path>` to
   `GITHUB_STATE`.

`teardown` exits 0 in every case except a flag error.

- [ ] **Step 1: Write the failing specs** (Review Focus 1–5).
  - **Default release.** `Install` with empty `Version`, `SHA256` and `Path`
    downloads `DefaultRooket()`'s archive, from an `httptest` server standing
    in for GitHub, and checks it against the pinned hash. `BinDir/rooket`
    then exists with mode 0755, and no `go` call is made. How the test
    redirects the download is the builder's call.
  - **Consumer release.** `Version` and `SHA256` both set download
    `Rooket(Version, SHA256)`'s archive. A hash that does not match is an
    error naming both hashes, after one request.
  - **go install fallback.** `Version` alone runs
    `go install github.com/jhoblitt/rooket@<v>` with `GOBIN=<BinDir>` and
    `GOTOOLCHAIN=auto` in the call's `env`, and makes no HTTP request.
  - **Toolchain override.** A parent environment holding `GOTOOLCHAIN=local`
    still yields `GOTOOLCHAIN=auto` in that call's `env`.
  - **Version forms.** `Version: "  0123abcd…(40 hex)  "` alone installs
    `@0123abcd…`, trimmed. The same holds for `main` and for a pseudo-version.
  - **Input conflicts.** `SHA256` alone is an error naming `rooket-sha256` and
    `rooket-version`. `Path` with `Version` or `SHA256` is an error naming both
    inputs. A `SHA256` that is not 64 hex digits is an error naming
    `rooket-sha256`.
  - **Copied binary.** With `Path: "./rooket"`, cwd set to a temp directory,
    and a source file of mode 0644: no `go` calls, and `BinDir/rooket` exists
    with mode 0755 and the same bytes.
  - **Input validation.** `ParseBool` accepts `True`, `FALSE` and `false`.
    `no` is an error naming the input. `workers` `0`, `three` and `-1` are
    errors. `min-free-gb` empty means 0, and `-5` is an error.
  - **State order.** A failing `prepare.Run` leaves `GITHUB_STATE` unwritten.
    A successful `setup` writes both state lines and both outputs.
  - **Teardown command.** `teardown` with `--teardown true` runs exactly
    `<rooket> down --all --force --delete-disks`, with `ROOKET_ENGINE=docker`
    in `env`.
  - **Teardown failure.** A Fake failure on that call prints a line starting
    `::warning::` and exits 0.
  - **Teardown skipped.** With `--teardown false`, it makes no call and exits 0.
  - **Second teardown.** A second `teardown` against a Fake that succeeds also
    exits 0. With the real rooket, `down --all` with nothing left already exits
    0.
  - **Second setup.** A second `setup` into the same `BinDir` succeeds.
- [ ] **Step 2:** Run `go test ./...`. Expected: FAIL.
- [ ] **Step 3:** Implement, with the real dependencies wired in `main.go`.
- [ ] **Step 4:** Run `make check`. Expected: PASS.
- [ ] **Step 5:** Commit `feat: add the setup and teardown subcommands`.

### Task 7: The action, its self-test, and CodeQL

**Files:**
- Create: `action.yml`, `js/main.mjs`, `js/post.mjs`, `js/common.mjs`,
  `.github/workflows/self-test.yml`, `test/selftest/selftest_test.go` (build
  tag `selftest`), `.github/workflows/codeql.yml`

**Interfaces:**
- Consumes: Task 6's CLI contract.
- Produces: `uses: ./` and `uses: jhoblitt/rooket-action@…` with the spec's
  inputs and outputs.

The JavaScript's behaviour:

- **`common.mjs`:**
  - `input(name)` reads `process.env['INPUT_' + name.toUpperCase()]`, or `''`.
  - `findGo()` searches `PATH`. Add the tool-cache scan only if Task 1 found Go
    absent from `PATH`; that scan takes the newest `$RUNNER_TOOL_CACHE/go/*/x64/bin/go`.
  - `run(cmd, args, opts)` uses `spawnSync` with `stdio: 'inherit'` and returns
    the exit status.
- **`main.mjs`:**
  1. Resolve `actionDir` from `import.meta.url`.
  2. Set `binDir` to `$RUNNER_TEMP/rooket-action/bin`.
  3. Run `go build -o <binDir>/rooket-action ./cmd/rooket-action` in
     `actionDir`, with `GOTOOLCHAIN=auto`.
  4. Exec `setup`, passing every input as a flag.
  5. Exit with the exit status.

  When no Go is found, it prints
  `::error::rooket-action needs Go; add actions/setup-go before this step` and
  exits 1.
- **`post.mjs`:** with `STATE_helper` empty, exit 0. Otherwise exec
  `<STATE_helper> teardown --rooket <STATE_rooket> --teardown <input('teardown')>`.

- [ ] **Step 1: Write the self-test spec** (`test/selftest`, Ginkgo, run only in
  CI). It asserts:
  - `rooket version` reports `tools.DefaultRooket().Version`.
  - `exec.LookPath("rooket")` equals `$ROOKET_ACTION_PATH`, which the workflow
    sets from the `path` output.
  - For each `tools.Pins()` entry, `LookPath` resolves into `dir(path)`, and the
    tool's version output contains `pin.Version`.
  - `ROOKET_ENGINE` is `docker`.
  - `systemctl is-active iscsid` prints `active`.
  - `lsmod` lists `target_core_mod`, `target_core_file`, `iscsi_target_mod` and
    `iscsi_tcp`, and configfs is mounted at `/sys/kernel/config`.
- [ ] **Step 2:** Write `action.yml`, with the seven inputs, their defaults and
  the two outputs, and the three JavaScript files. Run `node --check` on each.
  Expected: no output.
- [ ] **Step 3:** Write `self-test.yml`, triggered on `pull_request` and
  `workflow_call` only (Task 8's concurrency rules explain why there is no push
  trigger). It has two jobs, which between them cover both ways of finding Go.
  - **`setup`** (timeout 20): checkout, set up Go from `go.mod`, then
    `uses: ./` with `id: rooket`. This is the consumer-ran-setup-go path. Then
    `ROOKET_ACTION_PATH=${{ steps.rooket.outputs.path }} go test -tags selftest ./test/selftest/...`.
  - **`cluster`** (timeout 60, env `ROOKET_NAME: smoke`): checkout, then
    `uses: ./` with no setup-go, which is the runner's-own-Go path. Then
    `rooket up --rook-version v1.20.7 --workers 1 --wait` and
    `rooket k -n rook-ceph exec deploy/rook-ceph-tools -- ceph -s`. It has no
    teardown step, so the post step tears down a live cluster. v1.20.7 is the
    release rooket's own e2e proves, bumped by hand.
- [ ] **Step 4:** Add `codeql.yml` from github-conventions' `security.md`, with
  one job per language: `go` and `javascript-typescript`.
- [ ] **Step 5:** Run `actionlint` and
  `GITHUB_TOKEN=$(gh auth token) pinact run`, then
  `pinact run --check --verify`. Expected: clean.
- [ ] **Step 6:** Commit `feat: add the action and its self-test`. Push and
  open the draft PR for Tasks 2–7. Watch CI.
  - Expected: `setup` is green.
  - Expected: `cluster` is green, and its post-step log shows
    `rooket down --all` removing `smoke`.
  - Expected: the post step of `setup` reports nothing to tear down.

### Task 8: semantic-release

**Files:**
- Create: `package.json` (private, `devDependencies`: `semantic-release`,
  `@semantic-release/commit-analyzer`,
  `@semantic-release/release-notes-generator`, `@semantic-release/github`),
  `package-lock.json`, `.releaserc.json`, `.github/workflows/release.yml`
- Modify: `.github/dependabot.yml` (add the `npm` ecosystem), and
  `.github/workflows/ci.yml` (add the `workflow_call` trigger)

**Interfaces:**
- Consumes: `ci.yml` and `self-test.yml` as reusable workflows.
- Produces: tags `vX.Y.Z` and GitHub releases. The first is `v1.0.0`.

`.releaserc.json`:
- `branches: ["main"]`.
- commit-analyzer on the default preset, so `feat` releases a minor, `fix` and
  `perf` a patch, a breaking change a major, and `build`, `ci`, `chore`, `docs`
  and `test` nothing.
- release-notes-generator.
- github, with `successComment: false`, `failComment: false` and
  `releasedLabels: false`.
- No npm plugin.

`release.yml`:
- Triggered on `push: [main]`, and fork-guarded with
  `if: github.repository == 'jhoblitt/rooket-action'`.
- Jobs `ci` and `self-test` call the reusable workflows. Job `release` needs
  both and holds `permissions: contents: write`. It checks out with
  `fetch-depth: 0`, sets up Node from `package.json` `engines` (24), runs
  `npm ci`, then `npx semantic-release` with
  `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`.
- Concurrency: `release.yml` uses the constant group `release` with
  `cancel-in-progress: false`.
- `ci.yml` and `self-test.yml` trigger on `pull_request` and `workflow_call`
  only. Their `push: main` triggers are dropped, because `release.yml` runs
  both on every push to `main`.
- Their concurrency groups use literal prefixes,
  `ci-${{ github.event.pull_request.number || github.sha }}` and
  `self-test-${{ github.event.pull_request.number || github.sha }}`, not
  `${{ github.workflow }}`. A called workflow evaluates `github.workflow` as the
  caller's name, so both calls from `release.yml` would share one group and
  cancel each other. Literal prefixes keep them apart, and keep them apart from
  the caller's `release` group.

- [ ] **Step 1:** Write the files. Run `npm install` to produce the lockfile,
  then
  `npx semantic-release --dry-run --no-ci --branches $(git branch --show-current)`.
  Expected: it reports the next version it would publish.
- [ ] **Step 2:** Run actionlint and pinact as in Task 7. Expected: clean.
- [ ] **Step 3:** Commit `ci: release with semantic-release`, then push and
  watch CI.
- [ ] **Step 4:** On merge, which waits for the owner, watch `release.yml`.
  Expected: tag `v1.0.0` and a GitHub release exist.

### Task 9: README

**Files:** modify `README.md` (the skeleton from Task 0).

- [ ] **Step 1:** Fill in the Install and Usage sections. Cover:
  - the `uses:` line pinned by SHA with `# vX.Y.Z`;
  - the inputs and outputs table from the spec;
  - running `actions/setup-go` first when the job needs Go anyway;
  - `rooket-version` with `rooket-sha256` for trying a newer rooket release, and
    `rooket-version` alone for testing an unreleased rooket;
  - the consumer still running `rooket up` and its own teardown, with the post
    step as a safety net;
  - the GitHub-hosted Linux x64 limit and why;
  - how the rooket pin moves:
    - verify the release's cosign bundle over `checksums.txt`;
    - update the version and hash in `internal/tools/rooket.go`;
    - commit as `fix(deps): …`.
- [ ] **Step 2:** Commit `docs: document using the action`, and land it on the
  Tasks 2–7 PR before merge, so `v1.0.0` ships documented.

### Hand-off after v1.0.0

- Send the rgw-go session the release's `uses:` line and the spec's rgw-go
  follow-up. Its migration is its own PR.
- Tell the owner that the rooket follow-up, moving `integration.yml`'s e2e job
  onto the action, is open and is theirs to schedule.
