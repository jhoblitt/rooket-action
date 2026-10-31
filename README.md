# rooket-action

[![ci](https://github.com/jhoblitt/rooket-action/actions/workflows/ci.yml/badge.svg)](https://github.com/jhoblitt/rooket-action/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/jhoblitt/rooket-action/badge)](https://scorecard.dev/viewer/?uri=github.com/jhoblitt/rooket-action)

Prepares a GitHub-hosted Ubuntu runner to run rooket, and tears down what rooket
left behind. Every tool it downloads is pinned and checked against a sha256 that
did not come from the host serving the download, and kubectl's version matches
the node image of the rooket it pins. Its post step tears down what rooket
created even when the job fails or is cancelled.

## Install

Pin the action to a release's full commit SHA, with the tag as a comment:

```yaml
- uses: jhoblitt/rooket-action@<full-commit-sha> # v1.0.0
```

`<full-commit-sha>` is the 40-character commit that the tag `v1.0.0` points at.
The [releases](https://github.com/jhoblitt/rooket-action/releases) page shows it
abbreviated. Write `@v1.0.0` and run `pinact run`, which replaces it with the
full SHA and keeps the tag as the comment, or print the SHA with
`git ls-remote --tags https://github.com/jhoblitt/rooket-action refs/tags/v1.0.0`.
A `github-actions` entry in the consuming repository's `.github/dependabot.yml`
then bumps the SHA and its comment on Dependabot's schedule.

The action runs only on a GitHub-hosted Linux x64 runner (see
[Runners](#runners)). It builds a Go helper from its own source, so it needs Go
on `PATH`. GitHub's ubuntu-24.04 image carries one, and the action fetches the
toolchain the helper's `go.mod` names (`GOTOOLCHAIN=auto`). A job that needs Go
for its own steps runs [actions/setup-go](https://github.com/actions/setup-go)
before the action, and the action then builds its helper with that Go. With no
Go on `PATH`, the action fails and names `actions/setup-go`. The action keeps
its own module cache under `RUNNER_TEMP`, so in either order the job's module
cache, which `actions/setup-go` restores, holds nothing of the action's.

## Usage

The action does not start a cluster. It installs
[rooket](https://github.com/jhoblitt/rooket) and the pinned kind, kubectl and
helm into a directory it puts first on `PATH`, sets `ROOKET_ENGINE=docker`, and
frees disk when short. Unless `iscsi: false`, it installs and loads the iSCSI
and LVM tooling. The job runs `rooket up` and its own teardown. Outside a rook
clone, rooket refuses a command that names no cluster, so the job sets
`ROOKET_NAME`:

```yaml
jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 60
    env:
      ROOKET_NAME: e2e
    steps:
      - uses: jhoblitt/rooket-action@<full-commit-sha> # v1.0.0
      - run: rooket up --rook-version v1.21.1 --workers 1 --wait
      - run: rooket k -n rook-ceph exec deploy/rook-ceph-tools -- ceph -s
      - if: always()
        run: rooket down --delete-disks
```

The action's post step is a safety net under that teardown, not a replacement
for it. After the job's steps, it runs `rooket down --all --force --delete-disks`,
so a failed or cancelled job still tears down what rooket created. A failure
there is a warning and does not fail the job. An invalid `teardown` value fails
the main step instead, before rooket or any tool is installed. The post step
does nothing when `teardown: false`, or when the action's main step did not
succeed.

### Inputs

| Input | Default | Meaning |
|---|---|---|
| `rooket-version` | the pinned release | rooket release to download. Alone, anything `go install` accepts |
| `rooket-sha256` | the pinned hash | sha256 of that release's linux/amd64 archive. Needs `rooket-version` |
| `rooket-path` | empty | Use this binary instead of installing one. Excludes the two above |
| `workers` | `1` | Worker count, used only to size the disk check |
| `min-free-gb` | empty | GiB free that `/` needs, in place of 30 GiB plus 10 GiB per worker |
| `iscsi` | `true` | Install and load the iSCSI and LVM tooling |
| `teardown` | `true` | Run the post-step teardown |

### Outputs

| Output | Meaning |
|---|---|
| `path` | Absolute path of the rooket binary, also on `PATH` |
| `rooket-version` | What `rooket version` reports for that binary |

### Choosing rooket

- **None of the three set:** the action downloads the pinned release's
  linux/amd64 archive and checks it against the pinned sha256.
- **`rooket-version` and `rooket-sha256`:** the same, for that release and
  hash. The version must be a release, such as `v1.2.3`.
- **`rooket-version` alone:** `go install github.com/jhoblitt/rooket@<version>`,
  which accepts a version, branch or commit. The Go checksum database verifies
  the module. Building rooket makes this the slow path.
- **`rooket-sha256` alone:** an error, because a hash names no release.
- **`rooket-path`:** copies that binary in. Setting either of the others with
  it is an error.

To try a newer rooket release before the action pins it, take the archive's
hash from the release's `checksums.txt`, verified as in
[The rooket pin](#the-rooket-pin):

```yaml
- uses: jhoblitt/rooket-action@<full-commit-sha> # v1.0.0
  with:
    rooket-version: vX.Y.Z
    rooket-sha256: <sha256 of rooket_X.Y.Z_linux_amd64.tar.gz>
```

To test an unreleased rooket commit, set `rooket-version` alone:

```yaml
- uses: jhoblitt/rooket-action@<full-commit-sha> # v1.0.0
  with:
    rooket-version: <rooket-commit>
```

An override moves rooket alone. kubectl stays at the minor version of the
pinned release's node image, and kubectl's supported skew of one minor version
covers an override close to the pin.

### Runners

The action supports GitHub-hosted Linux x64 runners only. When `/` has less
free space than `min-free-gb`, or 30 GiB plus 10 GiB per worker, it deletes the
runner image's preinstalled .NET, Android, Haskell, CodeQL, JVM, Swift and
PowerShell toolchains with `sudo rm -rf`. Go and the rest of the tool cache
stay. A later step that needs a deleted toolchain must install it again. Still
being short afterwards is a warning, because the estimate is coarse.

Deleting files is safe only on a disposable machine. The action builds its
helper first; before deleting or installing anything, it checks for
`GITHUB_ACTIONS=true`, `RUNNER_ENVIRONMENT=github-hosted`, `RUNNER_OS=Linux` and
`RUNNER_ARCH=X64`, and refuses any machine where one does not hold. It never
tears down on a machine it refused.

### The rooket pin

The action pins one rooket release, with the sha256 of its linux/amd64 archive,
in [`internal/tools/rooket.go`](internal/tools/rooket.go). kind, kubectl and
helm are pinned in [`internal/tools/pins.go`](internal/tools/pins.go). The
hash's second source is the release's cosign keyless signature over its
`checksums.txt`, checked against Sigstore's transparency log rather than the
host serving the archive. To move the pin:

1. Pick the rooket release.
2. Download its `checksums.txt` and signature bundle, and verify them against
   rooket's release workflow:

   ```sh
   gh release download vX.Y.Z -R jhoblitt/rooket -p checksums.txt -p checksums.txt.sigstore.json
   cosign verify-blob --bundle checksums.txt.sigstore.json \
     --certificate-identity https://github.com/jhoblitt/rooket/.github/workflows/release.yml@refs/heads/main \
     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
     checksums.txt
   ```

3. Copy the sha256 of `rooket_X.Y.Z_linux_amd64.tar.gz` from `checksums.txt`
   into `internal/tools/rooket.go`, with the new version.
4. Run `make check`. One unit test holds the pin below v2, because rooket's
   module path has no `/v2` suffix. Another fails unless kubectl's minor
   version matches the node image the new release boots by default; follow it
   by bumping kubectl's version and hash in `internal/tools/pins.go`. That
   hash's second source is the sha512 that the
   [Kubernetes changelog](https://github.com/kubernetes/kubernetes/tree/master/CHANGELOG)
   lists for that kubectl version's client tarball. It covers the tarball, not
   the bare binary the action downloads, so the pin is the sha256 of the
   binary inside the checked tarball:

   ```sh
   k=v1.NN.P  # kubectl's new version
   curl -fLO "https://dl.k8s.io/$k/kubernetes-client-linux-amd64.tar.gz"
   sha512sum kubernetes-client-linux-amd64.tar.gz  # must match CHANGELOG-1.NN.md
   tar -xzf kubernetes-client-linux-amd64.tar.gz kubernetes/client/bin/kubectl
   sha256sum kubernetes/client/bin/kubectl         # the pin
   ```

5. Commit as `fix(deps): …`, which cuts a patch release of the action.
   Dependabot then carries it to consumers.

## Development

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/);
commitlint enforces this on every pull request. Every GitHub Action is pinned
to a commit SHA: run `pinact run` after editing a workflow and `actionlint`
before committing it. Where a `Makefile` is present, `make check` is the local gate,
`make tools` installs the pinned linter, and that pin lives in the `Makefile`.

## License

[Apache-2.0](LICENSE)
