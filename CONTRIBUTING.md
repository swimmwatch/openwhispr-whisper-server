# Contributing

This project is in bootstrap development. The approved first release packages
an unchanged OpenWhispr/whisper.cpp backend with a Go HTTP API and a Helm chart
for NVIDIA GPU transcription in Resident mode. No runnable service, images,
chart or release is available yet. See the [specification](docs/specs/gpu-whisper-server/spec.md)
and [packet checklist](docs/specs/gpu-whisper-server/tasks/todo.md).

## Local setup and checks

Use Git and Go 1.26.5, matching [go.mod](go.mod) and
[tool versions](tools/versions.yaml). Clone this repository once its initial
source has been pushed; until then, work from the existing local checkout.
From its root, run:

```sh
GOTOOLCHAIN=local GOWORK=off go mod download
GOTOOLCHAIN=local GOWORK=off go mod verify
git status --short
```

The module currently has no external dependencies or Go packages. These commands
verify module setup, not application behavior. Do not add placeholder tests or
claim runtime coverage. Temporary scripts, probes and their dependencies belong
under ignored `.local/`; they are not contributor or CI prerequisites.

Future GPU acceptance requires a Linux NVIDIA host, compatible NVIDIA driver
and container runtime; Kubernetes acceptance additionally requires NVIDIA GPU
scheduling. RTX 2060 is the target home-lab card. Hardware-free bootstrap checks
do not establish GPU compatibility. Image builds, Helm checks and actual Go
tests will be documented when those components exist.

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
