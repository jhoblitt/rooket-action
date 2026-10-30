# rooket-action: a GitHub action that prepares a runner for rooket

Date: 2026-10-29
Status: approved in conversation 2026-10-29; to be committed to
jhoblitt/rooket-action as its first design document

## Problem

Every CI job that runs rooket repeats the same runner preparation: free disk,
install kind, kubectl and helm, install and load the iSCSI and LVM tooling,
select the docker engine, and install rooket itself. Two copies exist and they
have drifted.

- jhoblitt/rooket's `integration.yml` installs kubectl through `stable.txt` and
  helm through helm's `get-helm-3` script from `main`, so neither is pinned, and
  its module list omits `iscsi_target_mod`.
- jhoblitt/rgw-go's local composite action `.github/actions/rooket-cluster`
  pins each tool to a sha256 taken from a second source. It pins rooket to a
  raw commit SHA bumped by hand, which was 52 commits behind rooket's `main`
  when this was written.

A composite action cannot have a post step, so each caller writes its own
`if: always()` teardown, and a cancelled job leaves its cluster behind.

The runner image is not a substitute. ubuntu-24.04 ships its own kind, kubectl
and helm, and its kubectl (1.37 when this was written) is well outside the
supported skew from the node image rooket pins (v1.33.1).

## Goals

- One action, `jhoblitt/rooket-action`, that leaves a GitHub-hosted Ubuntu
  runner ready to run `rooket up`, for consumers such as rgw-go, and later for
  rooket's own CI.
- Every tool it downloads is pinned and checked against a sha256 that did not
  come from the host serving the download.
- kubectl's version matches the node image of the rooket the action pins, and
  a mismatch fails the action's own CI.
- A consumer's pin on the action is bumped by Dependabot, not by hand.
- A cancelled or failed job still tears down what rooket created.

## Non-goals

- Starting the cluster. `rooket up`, the consumer's checks of what it brought
  up, its diagnostics, and its in-job teardown stay in the consumer. rgw-go's
  `make cluster-up` runs the same path locally and in CI, and wraps `rooket up`
  in checks only rgw-go can make.
- Any change to rooket. The owner declined a CI-only rooket subcommand; rooket
  gains nothing for this action.
- Tagging rooket. rooket stays untagged (the owner's decision), so the action
  pins it by pseudo-version.
- Self-hosted runners, non-Ubuntu runners, and arm64. The steps assume a
  disposable GitHub-hosted `ubuntu-*` x64 runner with passwordless sudo.
- Podman. A hosted runner's podman is rootless, which rooket refuses, and
  making it rootful is setup of its own. The action always selects docker.
  *(My call.)*
- Pinning the apt packages (`open-iscsi`, `targetcli-fb`, `lvm2`). apt checks
  their signatures against Ubuntu's keyring, which is not the host serving the
  download. Their versions follow the runner image, as they do in both
  workflows today. *(My call.)*
- Publishing to the GitHub Marketplace, or publishing binaries.
- Consumer-specific setup, such as rgw-go's librados and jq install.
- Migrating rgw-go or rooket's CI to the action. Each is that repository's own
  change, made after the action's first release.

## Anti-goals

The action fails at its purpose if any of these holds, even if it works:

- It deletes files or tears down clusters on a machine that is not a
  disposable GitHub-hosted runner.
- A pin drifts silently: a tool's version or hash changes, or kubectl's version
  moves relative to the node image, with no change to rooket-action.
- Its logic lives in YAML, shell or JavaScript. The owner's standing rule is
  that scripts are only glue, and logic is Go with tests.
- It changes how rooket behaves. It may only prepare the machine, so the
  consumer's local and CI runs of `rooket up` stay the same path.

## Design

### Repository

`jhoblitt/rooket-action`, created through the github-conventions new-repo
procedure. That procedure covers the ruleset, delete-branch-on-merge,
populating through an `init` pull request, the Apache-2.0 license, the README
skeleton, Dependabot, workflow-lint, commitlint, dependency review, Scorecard,
and CodeQL for Go and JavaScript. Visibility is public, because rgw-go is public
and a public repository's workflows cannot use an action from a private one.
Visibility is confirmed with the owner when the repository is created, as the
canon requires.

`action.yml` sits at the root, so a consumer writes
`uses: jhoblitt/rooket-action@<sha> # vX.Y.Z`.

### Versions: semantic-release

semantic-release cuts the action's versions from conventional commits on
`main` *(the owner's decision)*. commitlint already enforces those commits.

- The first release is v1.0.0. *(semantic-release's default.)*
- `feat` cuts a minor release, `fix` and `perf` a patch, and a breaking change
  a major.
- A bump of the pinned rooket is committed as `fix(deps): …` so that it
  releases. Dependabot's own `build(deps)` and `ci` commits do not release.
  *(My call: a bump of a workflow pin or of semantic-release itself changes
  nothing a consumer runs.)*
- It releases only from a push to `main` whose CI passed, and publishes a git
  tag and a GitHub release with generated notes. Nothing is published to npm.
- The release job needs `contents: write` only. Features of the GitHub plugin
  that need more scopes, such as comments on released issues and PRs, are
  turned off. *(My call: least privilege.)*
- semantic-release and its plugins are pinned through a committed npm
  lockfile, which Dependabot's npm ecosystem bumps.

This replaces go-conventions' goreleaser release, which exists to publish
binaries. rooket-action publishes none.

### Shape: JavaScript glue plus a Go helper

The action is a JavaScript action (`runs.using: node24`) with `main` and
`post` scripts, because a post step is available only to JavaScript and Docker
container actions. A Docker container action runs in a container, not on the
host that must hold the kind cluster and the iSCSI targets.

The JavaScript is glue. It has no dependencies and no build step. It reads
inputs from `INPUT_*` and writes state, environment, path and outputs through
the runner's files directly. Every decision is made in a Go helper in the same
repository, which follows go-conventions (cobra, Ginkgo, Go 1.27).

### main

1. **Find Go.** It looks on `PATH`, where hosted runners have one (see
   "Verify before building", results). A Go older than the one the helper's or
   rooket's `go.mod` names is acceptable, because `GOTOOLCHAIN=auto` fetches
   the rest. If no Go is found, it fails and
   names `actions/setup-go` as the fix. A consumer that needs Go for its own
   steps may run `actions/setup-go` first, as rgw-go will, and the action then
   uses that Go.
2. **Build the helper** from the action's own source, which the runner
   downloaded at the commit the consumer pinned. Into the action's directory
   under `RUNNER_TEMP`.
3. **Install rooket** into the same directory, in one of two ways.
   - `rooket-path` set: copy that binary in. The caller's workspace never goes
     on `PATH`.
   - Otherwise: `go install github.com/jhoblitt/rooket@<v>` with `GOBIN` set to
     that directory. `<v>` is the `rooket-version` input, or else the version
     rooket-action's `go.mod` pins. The Go checksum database verifies the
     module, which is not the host serving it, so rgw-go's integrity rule
     holds. `rooket version` then reports that version.
4. **Run the helper's prepare step** (below).
5. **Save the binary's path for `post`**, only after prepare succeeds.

rgw-go accepted a source build per job for its PR-gating job. It costs a Go
build of the helper and a `go install` of rooket, expected to take under a
minute but not yet measured. *(My call:)* if the measured time, toolchain fetch
included, exceeds two minutes, building on the runner is reopened with the owner
before anything else is built on it.

### The rooket pin

rooket-action's `go.mod` carries `tool github.com/jhoblitt/rooket`, which keeps
the pin and its `go.sum` hashes from being removed by `go mod tidy`, since
nothing imports rooket. The pin is a pseudo-version, because rooket is
untagged.

Dependabot cannot bump a pseudo-version, so the pin moves by hand: run
`go get -tool github.com/jhoblitt/rooket@main` and commit the result as
`fix(deps): …`. That commit cuts a patch release, and Dependabot then bumps
every consumer's pin on the action. One pin now serves every consumer, where
each consumer used to keep its own.

The `rooket-version` input overrides the pin for one job, for example to try an
unreleased rooket fix in rgw-go's CI. It accepts anything `go install` accepts.
*(The owner's decision.)*

### prepare

The helper runs these steps in order. It fails fast on any step except the
disk check.

1. **Refuse anything but a GitHub-hosted Linux x64 runner.** It exits non-zero,
   naming the variable that failed, unless `GITHUB_ACTIONS=true`,
   `RUNNER_ENVIRONMENT=github-hosted`, `RUNNER_OS=Linux` and `RUNNER_ARCH=X64`.
   It checks before touching anything. This guard is what makes the deletions
   below safe; see the anti-goals.
2. **Disk.** It needs `min-free-gb` free on `/`, or else 30 GiB plus 10 GiB per
   worker. The formula fits both measurements so far: rgw-go's 40G for one
   worker, and rooket's 60G for three, which includes a rook build.
   - Only when short does it delete preinstalled toolchains, because that takes
     minutes.
   - It deletes exactly the list both workflows delete today:
     `/usr/share/dotnet`, `/usr/local/lib/android`, `/opt/ghc`,
     `/opt/hostedtoolcache/CodeQL`, `/usr/local/.ghcup`, `/usr/lib/jvm`,
     `/usr/share/swift` and `/usr/local/share/powershell`.
   - Nothing else in the tool cache is touched, Go in particular: a consumer's
     later steps build with the Go that `actions/setup-go` put there.
   - Still being short afterwards is a warning, not a failure, because the
     estimate is coarse.
3. **Tools.** It downloads kind, kubectl and helm into the action's directory
   and checks each against a sha256 compiled into the helper.
   - It extracts `helm` from the release archive, whose hash is the one
     checked.
   - Each download has a timeout per request and a bounded number of retries,
     so a network flake does not fail a long job. A hash mismatch is not
     retried.
   - The starting pins are rgw-go's current ones: kind v0.32.0, kubectl v1.33.1
     and helm v3.20.1.
   - Each hash's second source is recorded beside it, following rgw-go's
     practice: kind's and helm's GitHub releases, and the Kubernetes changelog's
     sha512 for kubectl.
4. **iSCSI and LVM**, unless `iscsi: false`. It runs `apt-get update` and
   installs `open-iscsi`, `targetcli-fb` and `lvm2`. It loads `configfs`,
   `target_core_mod`, `target_core_file`, `iscsi_target_mod` and `iscsi_tcp`
   explicitly, so a missing module fails by name, and it enables and starts
   `iscsid`. It creates no targets and no disk images; those stay with rooket.
5. **Exports.** It appends the action's directory to `GITHUB_PATH`, so later
   steps find the pinned tools ahead of the runner image's own copies, and
   appends `ROOKET_ENGINE=docker` to `GITHUB_ENV`. Rootful docker gives kind
   nodes the host's iSCSI block devices without the rootful-podman setup a
   runner otherwise needs.

Beyond the action's directory, the deletions, the apt packages, the modules
and `iscsid`, prepare changes nothing on the machine.

### kubectl and the node image

kubectl is pinned in rooket-action, like kind and helm. A unit test reads the
pinned rooket's module source from the module cache, parses its default node
image constant, and fails if kubectl's major and minor differ from that
image's tag. A rooket bump that moves the node image therefore fails
rooket-action's CI until kubectl follows it.

The test couples rooket-action to a rooket constant's name and file. If rooket
renames it, the test fails, never a consumer's job, which is the acceptable
place for that break.

A `rooket-version` override does not move kubectl. kubectl's supported skew of
one minor covers an override close to the pin, and an override is the
consumer's deliberate choice.

### post

`post` runs `rooket down --all --force --delete-disks` after every step of the
job, with `ROOKET_ENGINE=docker` set. It is a safety net under the consumer's
own teardown, not a replacement for it. rgw-go's `down.sh` must still run
in-job: it stops the rgw-go it started before removing the cluster, and removes
its own output directory. rgw-go confirmed that this order works for it:
diagnostics, then its own teardown, then this step, which finds nothing.

- It does nothing unless `main` saved a binary path, which happens only after
  prepare succeeds. So a runner the guard refused is never torn down. It also
  does nothing if `teardown: false`.
- When nothing is left, `rooket down --all` already reports "nothing to tear
  down" and exits 0.
- A failure is reported as a warning and does not fail the job. By the time it
  runs, the job's result is already decided, and the runner is discarded.

### Inputs and outputs

| Input | Default | Meaning |
|---|---|---|
| `rooket-version` | the pin in `go.mod` | rooket version or commit to `go install` |
| `rooket-path` | empty | Use this binary instead of installing one |
| `workers` | `1` | Worker count, used only to size the disk check |
| `min-free-gb` | empty | Overrides the disk formula |
| `iscsi` | `true` | Install and load the iSCSI and LVM tooling |
| `teardown` | `true` | Run the post-step teardown |

| Output | Meaning |
|---|---|
| `path` | Absolute path of the rooket binary, also on `PATH` |
| `rooket-version` | What `rooket version` reports for that binary |

### rooket-action's CI

- **Go:** `make check`, as go-conventions defines it.
- **Self-test, setup only:** runs `uses: ./` on `ubuntu-latest` with the
  defaults, then asserts the following:
  - `rooket` on `PATH` is the `path` output.
  - kind, kubectl and helm resolve to the action's directory and report their
    pinned versions.
  - `ROOKET_ENGINE` is `docker`.
  - `iscsid` is active and the five modules are loaded.
- **Self-test, cluster:** runs `uses: ./`, then
  `rooket up --rook-version <pinned release> --workers 1 --wait`, and leaves the
  cluster up so that `post` tears down a live cluster.
  - Its warning-only result means a broken teardown shows in the step log and
    annotations, not as a red job. That is an accepted gap. *(My call.)*
  - The Rook release it deploys is pinned in the workflow and bumped by hand.
- **Release:** semantic-release, after the jobs above pass on `main`.

## Verify before building

None of these could be checked without running a workflow, so the plan's first
task is a throwaway workflow that checks them:

1. A remote JavaScript action's directory holds the whole repository at the
   pinned ref, so `main` can build the helper relative to its own script.
2. `post` runs for a local (`uses: ./`) JavaScript action. The docs say only
   `pre` is unsupported for local actions.
3. ubuntu-latest has a Go on `PATH` or in its tool cache without
   `actions/setup-go`, and `GOTOOLCHAIN=auto` fetches Go 1.27.
4. `RUNNER_ENVIRONMENT`, `RUNNER_OS` and `RUNNER_ARCH` read `github-hosted`,
   `Linux` and `X64` on a hosted runner.
5. How long the helper build plus the rooket `go install` takes. More than two
   minutes reopens building on the runner (see "main").

If (1) or (3) fails, the fallback changes the design: require
`actions/setup-go` before the action, or ship the helper prebuilt. It therefore
comes back to the owner rather than being made silently.

### Results (2026-10-30)

A throwaway workflow on ubuntu-24.04, image 20261025.353.1, answered all five
questions. It ran one job with `uses: ./` and one with
`uses: jhoblitt/rooket-action@<sha>`, and neither job used `actions/setup-go`.

1. Holds. The remote action's directory,
   `/home/runner/work/_actions/jhoblitt/rooket-action/<sha>`, held the whole
   repository, `go.mod`, `cmd/` and `internal/` included.
2. Holds. `post` ran for the local action.
3. Holds.
   - `/usr/bin/go` is on `PATH`, at go1.24.13.
   - The tool cache holds 1.24.13, 1.25.14 and 1.26.10.
   - `GOTOOLCHAIN=auto` fetched go1.27.0 for the helper's module and go1.27.3
     for rooket.
   - Because Go is on `PATH`, `main` looks for Go on `PATH` only, with no
     tool-cache scan.
4. Holds. `GITHUB_ACTIONS=true`, `RUNNER_ENVIRONMENT=github-hosted`,
   `RUNNER_OS=Linux` and `RUNNER_ARCH=X64`.
5. Under the limit. The cache was cold in both jobs.

   | Step | Local job | Remote job |
   |---|---|---|
   | Toolchain fetch | 3.9 s | 4.7 s |
   | Helper build | 16.7 s | 17.8 s |
   | rooket `go install` | 80.3 s | 22.1 s |
   | Total | about 101 s | about 45 s |

   The slower total comes within 20 s of the two-minute limit, and the
   difference between the jobs is the module download.

## Follow-ups outside this repository

- **rgw-go:** replace its composite action's runner prep and rooket install with
  `uses: jhoblitt/rooket-action@<sha> # vX.Y.Z`. It keeps its librados and jq
  step, `make cluster-up` and `make populate`, and its Rook pin in
  `hack/rooket/lib.sh`, and it updates its s3tests path filter. That is
  rgw-go's change to make.
- **rooket:** optionally move `integration.yml`'s e2e job onto the action, with
  `rooket-path` set to the binary its `build` job produced. That would replace
  its unpinned kubectl and helm installs. The owner decides whether and when.

## Left to the builder

- The Go package layout, and how the command runner and the HTTP downloads are
  faked in tests.
- Retry counts and timeouts.
- Log formatting and `::group::` folding.
- The README's usage section.
