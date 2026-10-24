# Contributing

This project is in bootstrap development. The approved first release packages
an unchanged OpenWhispr/whisper.cpp backend with a Go HTTP API and a Helm chart
for NVIDIA GPU transcription in Resident mode. See the [specification](docs/specs/gpu-whisper-server/spec.md)
and [packet checklist](docs/specs/gpu-whisper-server/tasks/todo.md).

## Local setup and checks

Use Git and Go 1.27.3, matching [go.mod](go.mod) and
[tool versions](tools/versions.yaml). Work from a local repository checkout.
From its root, run:

```sh
GOTOOLCHAIN=local GOWORK=off go mod download
GOTOOLCHAIN=local GOWORK=off go mod verify
git status --short
```

These commands verify module setup, not application behavior. Run the checks
owned by the affected packet as well. Temporary scripts, probes and dependencies belong
under ignored `.local/`; they are not contributor or CI prerequisites. The uv
project environment uses the standard ignored `.venv/` directory.

Future GPU acceptance requires a Linux NVIDIA host, compatible NVIDIA driver
and container runtime; Kubernetes acceptance additionally requires NVIDIA GPU
scheduling. RTX 2060 is the target home-lab card. Hardware-free bootstrap checks
do not establish GPU compatibility. Image builds, Helm checks and actual Go
tests and their qualification status are documented in the owning packets.

## Python tooling

Use uv 0.13.5 and CPython 3.14.9 for all Python tooling.
Install the pinned uv release with the official versioned installer, then follow
its instructions to make `uv` available on PATH:

```sh
curl -LsSf https://astral.sh/uv/0.13.5/install.sh | sh
```

From the repository root, install the exact interpreter selected by
[.python-version](.python-version), then synchronize the locked dependencies:

```sh
uv python install
uv sync --locked
uv run --locked ruff format --check tools tests/tooling
uv run --locked ruff check tools tests/tooling
uv run --locked mypy
uv run --locked python -m unittest discover -s tests/tooling -v
```

Ruff owns formatting, imports, syntax, modernization and selected bug/simplification
checks; mypy checks production annotations and test bodies. Configuration lives
in [pyproject.toml](pyproject.toml). See the official
[Ruff configuration](https://docs.astral.sh/ruff/configuration/) and
[mypy configuration](https://mypy.readthedocs.io/en/stable/config_file.html).
Run `uv run --locked ruff format tools tests/tooling` to apply formatting locally.
The non-packaged uv project declares runtime tooling dependencies and a default
`dev` dependency group in `pyproject.toml`; [uv.lock](uv.lock) is the only dependency
lock. `uv sync --locked` installs both groups into `.venv/` and fails on a stale
lock. Update dependency declarations and run `uv lock` together when a dependency
change is intentional. Service images do not install Python tooling.

CI explicitly runs `uv sync --locked --no-build` and then `uv run --no-sync` for
checks, so execution reuses the prepared environment. Local commands use
`uv run --locked` to verify the project lock before execution. See the official
[uv project workflow](https://docs.astral.sh/uv/guides/projects/) and
[Python version management](https://docs.astral.sh/uv/concepts/python-versions/).

Follow [the Python architecture rules](AGENTS.md#python-tooling-architecture).
Prefer functions for simple transformations, dataclasses for related data and
classes for owned state. Keep command assembly separate from policy and I/O.
Use typed interfaces at real boundaries without adding speculative abstractions.
Preserve CLI compatibility, report schemas and safe error output. Add behavior
tests for changed boundaries and failure paths; avoid testing implementation shape.

The default unittest run reports environment-dependent real-tool skips. Those
skips do not qualify security or release behavior: enable the documented
`C02_SCANNER_TESTS`/`C02_PUBLISHER_TESTS` fixtures with their pinned tools and
artifact inputs for the full C02 check. Never substitute a mock for required
native, GPU or hosted CI evidence.

## GitHub Actions linting

Install the checksum-verified actionlint release selected by
[tools/versions.yaml](tools/versions.yaml), using the existing uv environment:

```sh
uv run --locked python tools/install_ci.py \
  --destination .local/ci-tools \
  --tools actionlint \
  --download-cache .local/ci-downloads
.local/ci-tools/actionlint -version
.local/ci-tools/actionlint
```

Run these commands from the repository root. With no file arguments,
[actionlint checks every workflow](https://github.com/rhysd/actionlint/blob/v1.7.12/docs/usage.md#actionlint-command)
under `.github/workflows/`, including newly added files. CI uses the same command
and pinned installer. Run it after changing workflows or local composite actions.
Do not suppress findings to make the check pass. Downloads and binaries remain
under ignored `.local/`; no global installation is required.

Common runner, Python, tool-cache and scanner setup belongs to the
[internal composite actions](.github/actions/README.md). Reuse them in both CI and
release workflows. Changes must preserve the caller's dependency/cache policy;
use actionlint and ShellCheck to lint their syntax.

Write tests for observable behavior of the service and its tools. Source syntax,
formatting and workflow syntax belong to the existing compiler and linters; do
not add repository-file validators or tests that mirror source/configuration
layout. See [the project testing rules](AGENTS.md#test-behavior-not-repository-files).

Actionlint also uses ShellCheck for embedded shell scripts when `shellcheck` is
available on PATH. GitHub's Ubuntu runners provide it; install ShellCheck locally
for that additional coverage. Use `actionlint -verbose` to inspect discovered
files and enabled external checkers. Pyflakes is optional for embedded Python;
the project's Python modules are checked separately with Ruff and mypy.

## Changes

Discuss material scope or architecture changes in an issue before implementation.
Keep changes focused on the approved requirements. Create a `feature/` branch,
use English identifiers, comments and documentation, and open a pull request.
Describe the behavior changed, related requirement/issue, actual checks run and
any checks unavailable. Required CI names will be configured after the workflows
exist; never bypass a failing required check.

Do not commit credentials, models, private recordings, transcripts, agent history
or generated build output. Use synthetic or explicitly public fixtures. Follow
the [code of conduct](CODE_OF_CONDUCT.md). Use [support](SUPPORT.md) for questions
and [private reporting](SECURITY.md) for suspected vulnerabilities.

## Commit messages and release records

Follow the pinned [commit, versioning and changelog conventions in AGENTS.md](AGENTS.md#commits-versioning-and-changelog).
Write commit messages in English. For example:

```text
docs(agents): document release conventions
fix(api): preserve request cancellation
feat(api)!: change transcription response fields

BREAKING CHANGE: Clients must use the documented replacement response fields.
```

The feature and fix messages above illustrate the format, not implemented changes.
Choose the type/scope from the actual change. Describe migration when a change
breaks a public contract. Review the combined release changes before choosing
its SemVer version; a commit message does not authorize a release.

Add notable changes to the appropriate CHANGELOG.md Unreleased section. Entries
describe verified behavior and its effect on users, rather than listing commits.
At release time, move the included entries into a dated version section and add
links to real release tags/comparisons. Keep unreleased work out of that section
and preserve the project's pre-release qualification requirements.

Contributions to this project's own code and chart are under
[Apache-2.0](LICENSE). Preserve third-party notices and licensing obligations
described in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
