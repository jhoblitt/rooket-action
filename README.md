# rooket-action

[![ci](https://github.com/jhoblitt/rooket-action/actions/workflows/ci.yml/badge.svg)](https://github.com/jhoblitt/rooket-action/actions/workflows/ci.yml)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/jhoblitt/rooket-action/badge)](https://scorecard.dev/viewer/?uri=github.com/jhoblitt/rooket-action)

Prepares a GitHub-hosted Ubuntu runner to run rooket, and tears down what rooket
left behind. Every tool it downloads is pinned and checked against a sha256 that
did not come from the host serving the download, and kubectl's version matches
the node image of the rooket it pins. Its post step tears down what rooket
created even when the job fails or is cancelled.

## Install

Not released yet.

## Usage

Documented with the first release.

## Development

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/);
commitlint enforces this on every pull request. Every GitHub Action is pinned
to a commit SHA: run `pinact run` after editing a workflow and `actionlint`
before committing it. Where a `Makefile` is present, `make check` is the local gate,
`make tools` installs the pinned linter, and that pin lives in the `Makefile`.

## License

[Apache-2.0](LICENSE)
