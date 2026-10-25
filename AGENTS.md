# Project instructions

## Scope and sources

This project delivers Resident V1: a Go/Echo HTTP API, an unchanged
OpenWhispr/whisper.cpp server supervised by a separate Go backend, two images,
and a Helm chart with separate ordinal-paired API/backend Pods. RAM-cache,
on-demand loading and a native C++ fork are deferred. Do not broaden this scope.

The approved behavior lives in [spec.md](docs/specs/gpu-whisper-server/spec.md),
[core contracts](docs/specs/gpu-whisper-server/core/contracts.yaml) and
[OpenAPI](docs/specs/gpu-whisper-server/contracts/openapi.yaml). Delivery order,
ownership and required checks live in
[plan.md](docs/specs/gpu-whisper-server/tasks/plan.md),
[todo.md](docs/specs/gpu-whisper-server/tasks/todo.md) and
[handoff.md](docs/specs/gpu-whisper-server/tasks/handoff.md).
Read the invoked packet and its exact contracts before changes. One explicit
incremental invocation authorizes one packet; it does not authorize another
packet, review, commit, push, deployment or publication.

Use the engineer-agent-workflow MCP for manifest.yaml, decisions.yaml, design
catalog/experience state and review-input archives. Never read, diff, parse or
rewrite those workflow-state files through shell. Missing required MCP/manual
blocks the affected workflow; a local parser is not a replacement. Recheck the
packet's pinned specification freshness before work and completion.

## Communication and lookup

- Use Russian for conversation; use English for code, diagnostics and repository
  documentation. Report actual checks and material limitations concisely.
- Use the available synchronous request_user_input for material user decisions
  that cannot be resolved from existing answers, evidence or conventional
  defaults. Do not repeat answered questions or request permission for work
  already authorized. Follow the host's normal approval controls.
- For agent shell commands use rtk, with rtk proxy for raw output. RTK is optional
  contributor tooling, not a service/build dependency. If unavailable, report it;
  use direct commands only where applicable host instructions allow the fallback.
- Before code lookup check the target repository for .codegraph. If present,
  use CodeGraph explore/node first, then focused rg/file reads. Do not create an
  index without a request. Verify external version-specific facts with appropriate
  primary documentation; keep private data out of public queries.
- Use context-scouting only when explicitly invoked; scouts inspect only and
  the parent validates their evidence. Do not replace missing native scouts with
  nested CLI sessions or new user-visible chats.

## Test behavior, not repository files

- Write code and tests for observable project behavior: requests and responses,
  returned values, state changes, errors, process/resource ownership and produced
  artifacts. A test must demonstrate a concrete behavior or failure of our code.
- Do not write custom checkers or tests for repository syntax, formatting, file
  existence, documentation markers, exact source strings, field counts, private
  library internals, YAML layout, workflow step names/order or whole configuration
  dictionaries. Do not build a second copy of an implementation to compare it
  with the first. Renaming or reorganizing equivalent code must not break tests.
- Use the existing compiler, formatter and standard linters for source syntax and
  formatting: Go tooling, Ruff, mypy, actionlint, ShellCheck and Helm tooling.
  Do not wrap their work in another project-specific validation framework.
- Do not add speculative repository-path, symlink or configuration guards as a
  substitute for requested behavior tests, including during cleanup. Tests of a
  checker whose only purpose is inspecting repository files are also prohibited.
- Do not add tests that read GitHub workflow/action YAML to inspect its syntax,
  structure, configuration or embedded commands. Use actionlint and ShellCheck;
  test the actual application/tool functions independently of workflow files.
- Validate external inputs and artifacts where actual runtime logic consumes
  them: audio limits, request data, downloaded bytes, archives, scanner reports
  and release evidence. Test the resulting behavior and failures; do not merely
  test that a third-party parser, dataclass or filesystem API works.
- During cleanup, delete redundant checkers and their tests rather than replacing
  them with new checkers. Preserve the user's explicit tool/version choices and
  tests that execute meaningful service, build, security or release behavior.

## Commits, versioning and changelog

Use these pinned conventions; changing a convention version requires an explicit
documentation update, not following an unversioned latest page:

| Concern | Convention and pinned version |
| --- | --- |
| Semantic commit messages | [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/) |
| Release versions | [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html) |
| Semantic changelog | [Keep a Changelog 2.0.0](https://keepachangelog.com/en/2.0.0/), using the project's SemVer release versions |

- Write English commit subjects as `<type>[optional scope][!]: <description>`.
  Use `feat` for features, `fix` for bug fixes and appropriate types such as
  `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `perf` or `revert` otherwise.
  Use lowercase types/scopes consistently. Mark breaking changes with `!` or a
  `BREAKING CHANGE:` footer; explain the incompatibility and migration.
- Assess release bumps against the documented public contracts, not commit type
  alone. For stable versions, incompatible changes require MAJOR, compatible
  features MINOR and compatible fixes PATCH. During `0.y.z`, the public API is
  unstable; still identify breaking changes. Keep the existing release inventory
  and pre-release flow authoritative for Git tags, images and Helm versions.
  The Git tag's `v` prefix is not part of the SemVer number. Published artifact
  versions are immutable; changes require a new version.
- Maintain CHANGELOG.md as a curated record of notable changes. Keep Unreleased
  first, then newest releases first with version, `YYYY-MM-DD` release date and
  release/comparison links once real tags exist. Use non-empty Added, Changed,
  Deprecated, Removed, Fixed and Security sections as applicable. Clearly mark
  breaking changes and their migration. Commit messages may supply draft input;
  review the entries for accuracy and never substitute a raw Git log. Do not
  invent release dates, tags or completed behavior.

Contributor examples are in [CONTRIBUTING.md](CONTRIBUTING.md#commit-messages-and-release-records).

## HTTP status constants

- Use named standard-library or framework constants for HTTP status codes in
  production code, tests, fixtures and tooling: `net/http.Status...` in Go and
  `http.HTTPStatus...` in Python. Do not use bare numeric codes or duplicate the
  standard constants in project-local aliases.
- Derive textual status expectations from those constants when a tool returns
  numeric HTTP codes as strings. Keep required wire-format literals in protocol
  schemas and documentation. For languages without standard HTTP constants, use
  one clearly named constant at the narrowest owning boundary.
- Keep protocol-range bounds named when no standard constant exists. Do not
  confuse HTTP statuses with process exit codes, timeouts, sizes or port numbers.
- Use normal behavior tests and existing linters to verify changes; do not add
  source-scanning tests or custom checkers for this rule.

## Go architecture

The project boundaries and selected packages are authoritative in
[F01](docs/specs/gpu-whisper-server/tasks/foundation_01_go_service.md).
These are project choices; do not scaffold unused layers or copy a template tree.

- **G01.** cmd/api and cmd/backend assemble dependencies and own startup/shutdown.
  Domain owns plain types/invariants; application owns use cases and consumed
  ports; adapters own HTTP/files/decoding/subprocesses; platform owns setup.
  Domain/application must not import Echo, concrete adapters or configuration
  loaders. Create packages when actual behavior requires them.
- **G02.** Define narrow ports at the consuming application boundary. Inject
  implementations through constructors; map validated configuration into typed
  options in command assembly. Reuse Echo's agreed rate-limit store interface.
  Do not add generic repositories, service locators, DI frameworks or mock-only
  interfaces.
- **G03.** Domain Failure/FailureKind, Cause/Unwrap and errors.Is/As are protocol
  neutral. HTTP status, headers, response envelope and safe public messages belong
  to the HTTP error mapper. Never classify by message text, serialize Cause,
  expose raw Error() output or use panic for ordinary failures. Preserve the
  contract's unknown-error fallback and response ownership.
- **G04.** Pass context.Context first for request-scoped/blocking operations.
  Never retain pooled Echo Context in queued work. Every goroutine/subprocess
  needs ownership, cancellation and bounded stop/wait. Closing a transport does
  not prove GPU work stopped. Preserve FIFO, upload/decoder limits, validated
  request IDs, private logging and trusted client-IP/rate-limit rules.
- **G05.** Use the agreed standard slog, Echo, Koanf and validator packages and
  approved versions. Strictly validate configuration sources before startup,
  preserve precedence and reject unknown/deferred fields. Do not duplicate
  defaults, replace packages or add hot reload/storage/protocol behavior during
  a refactor. Material dependency/behavior changes belong to their owning plan.
- **G06.** Use gofmt, go vet and behavior-focused tests; race checks cover relevant
  concurrency in the supported environment. Review package boundaries directly;
  later packets own adapter/process wiring. Test failure and resource ownership,
  not implementation shape. Go tool success alone does not prove GPU correctness.

## Python tooling architecture

- **PY01.** Python is contributor/CI tooling, not a service runtime dependency.
  Keep executable CLI assembly separate from artifact, installation and security
  policy modules. Preserve existing command arguments, exit behavior and report
  schemas during refactoring. Importing a module must not run its CLI.
- **PY02.** Use CPython 3.14.9 for all Python tooling, pinned in `.python-version`
  and `pyproject.toml`. Use uv 0.13.5 to install Python and synchronize the project;
  `uv.lock` is the sole dependency lock. Annotate functions and methods, including
  test helpers. Keep `Any` at genuinely variable JSON/YAML boundaries; validate
  external data before trusting it. Do not suppress an entire module's type errors.
- **PY03.** Prefer small cohesive functions. Use classes for owned state or a
  policy with several related operations, and frozen dataclasses for validated
  records/options. Use composition and dependency injection at actual I/O
  boundaries. Do not add inheritance trees, generic frameworks or mock-only
  interfaces. Apply patterns only to demonstrated needs.
- **PY04.** Share code at its narrowest common ownership point; do not couple
  security policy to CLI parsing or publication to process startup. Preserve
  subprocess cancellation/error handling, checksum verification, immutable
  artifact identities and private scanner output. No shell interpolation of
  untrusted inputs or expanded credential logging.
- **PY05.** Use four-space indentation, one statement per line, English names,
  explicit imports and Ruff formatting (100-character target). Ruff lint and
  mypy settings live in `pyproject.toml`. Fix causes before adding a narrowly
  justified suppression; do not disable rules to make a check pass.
- **PY06.** Use the existing unittest suite. Test observable behavior, invalid
  inputs and resource/error ownership; mock only external boundaries and retain
  real-tool fixtures where required. New dependencies must serve a current need,
  be declared in the appropriate project dependency group and locked in `uv.lock`.
  Run the [Python checks](CONTRIBUTING.md#python-tooling) before completion.

## Docker and Compose architecture

Packaging and its verification belong to
[P05](docs/specs/gpu-whisper-server/tasks/05_images_standalone.md), with actual
GPU/lifecycle evidence in P07. Image identities/defaults stay in core contracts.

- **D01.** Keep separate API/backend multi-stage images. API owns decoding; the
  backend supervisor owns the unchanged native child. Runtime stages contain
  required binaries, compatible CUDA/FFmpeg libraries and license material.
  Do not force scratch/Alpine or copy an entire development tree.
- **D02.** Preserve Linux amd64, pinned sources/toolchains and the contract's CUDA
  architecture coverage with native detection disabled. Build without GPU
  injection. Do not adopt narrower coverage/newer CUDA from an upstream example.
  Native health alone does not prove GPU use; CPU fallback cannot satisfy readiness.
- **D03.** Verify base digests/source checksums. Order stable module inputs before
  source for caching. Keep .dockerignore aligned with required inputs while
  excluding secrets, private state, audio/models and outputs. No secrets through
  ARG/ENV/COPY; legitimate sensitive build inputs use BuildKit secret mounts.
  Cached builds do not replace release checks.
- **D04.** Run as explicit non-root with read-only root filesystems and bounded
  contract-approved writable mounts. Check UID/GID permissions instead of adding
  privileged/root workarounds. Prepared models are read-only; model cache persists;
  disposable audio is separate. Cleanup cannot remove operator-owned cache.
- **D05.** Use exec-form entry points. Stop and reap the native process group before
  replacement. Grace periods follow shutdown/cleanup budgets. Keep bounded,
  distinct liveness/readiness checks; no inference warmup healthchecks or claims
  that container startup proves model readiness.
- **D06.** Maintain one canonical Compose stack and non-secret example inputs.
  Publish only the API, on loopback in local recipes. Supervisor stays private;
  native HTTP listens on backend loopback. Only backend gets one GPU, using gpu
  capabilities and count or device_ids. Preserve download egress, distinguish
  interpolation from configuration injection, and recreate the pair for changes.

## Helm and Kubernetes architecture

Chart structure/render/schema checks belong to
[P06](docs/specs/gpu-whisper-server/tasks/06_helm_chart.md). P08 owns actual
GPU/network/rollout/storage evidence; rendering is not runtime proof.

- **H01.** Keep a minimal apiVersion v2 application chart in charts/whisper-server,
  SemVer chart version and quoted appVersion. Split templates by resource; use
  two-space indentation, chart-prefixed helpers, documented typed camelCase values
  and recommended labels. Do not add a library chart for a few helpers.
- **H02.** Preserve separate API/backend StatefulSets and matching ordinals,
  per-backend Services and per-pair NetworkPolicy. Selectors remain stable across
  image/chart updates: no version/checksum selector labels. Only API Service and
  optional Ingress expose clients. Do not introduce a shared backend pool or
  change pairing to a single Pod.
- **H03.** Only backend requests the agreed GPU resource with matching contract
  requests/limits. Use ordered no-surge replacement and explicit single-GPU
  downtime; no old/new native GPU overlap or CPU fallback. Host drivers, GPU
  operator/plugin, CNI, storage and Ingress controllers are operator prerequisites,
  not infrastructure provisioned by this chart.
- **H04.** values.schema.json rejects unsupported keys/types/bounds; templates
  enforce remaining cross-field constraints. Serialize the canonical service
  configuration and verify Docker/Helm parity; do not create another defaults/API
  table. Preserve both image references and release inventory identities.
- **H05.** Keep distinct startup/readiness/liveness probes and provisioning budgets.
  Long healthy inference must not trigger liveness restarts. Config changes update
  both Pod-template checksums; protocol/fingerprint/generation checks guard mixed
  revisions. Termination grace follows configured shutdown plus cleanup.
- **H06.** Preserve non-root/read-only execution, bounded transient storage,
  read-only prepared files and Retain/Retain cache ownership. Existing claims
  must never be deleted by the chart. Avoid needless API tokens, RBAC, privileges
  and broadened network rules. NetworkPolicy requires an enforcing CNI. Actual
  networking, rollout/rollback and storage behavior require P08 evidence.

## Private Home Lab and release boundaries

Read .local/homelab/README.md for this operator's confirmed target, login and
freshness procedure, then .local/homelab/cluster.yaml for private inventory.
These files may be absent in contributor checkouts. Never dump credentials or
private cluster identities to logs, tracked files, images, charts or CI artifacts.
Preserve private directory mode 0700 and credential mode 0600; do not change the
global kubeconfig/current context. kubectl always selects --kubeconfig/--context;
Helm selects --kubeconfig/--kube-context. Scoped operations select the confirmed
namespace/release. A runbook does not authorize a deployment, label/RBAC change,
driver/plugin installation or a GPU probe workload. Refresh read-only access
before an independently authorized P08 acceptance run.

CI/CD publishes artifacts under its owning packets; it does not deploy Home Lab.
Pre-release qualification precedes stable release. Failed, unavailable or skipped
required checks remain incomplete; no checksum, successful upload, rendered YAML
or reported GPU resource substitutes for actual required acceptance.

## Current checks and workflow setup

Keep GitHub Actions `run` blocks as short shell orchestration. Put Python logic
in typed, tested modules under `tools/` and invoke it through the locked uv
environment; do not embed Python heredocs or `python -c` programs in workflow YAML.

After changing GitHub Actions workflows or local composite actions, run the pinned
actionlint from the repository root. Follow the installation and local check in
[CONTRIBUTING.md](CONTRIBUTING.md#github-actions-linting); use automatic workflow
discovery so new workflows are checked too. Fix findings instead of suppressing
them, and report unavailable optional external checkers separately.

See [agent-setup.md](docs/agent-setup.md) for project instruction and skill sources.
There is no custom repository-file checker. Set up the pinned uv project with
`uv python install` and `uv sync --locked`; use its ignored `.venv/` environment.
Temporary task files remain under `.local/`. Use the checks and available harnesses
owned by each packet. Do not revive retired temporary bootstrap scripts as prerequisites.

Codex is the supported project agent. Keep local gpu-acceptance and
release-verification skills under .agents/skills. Do not add another client
adapter or copy external developer plugins into this repository.
External workflows remain operator-installed tools outside I03 acceptance.
When explicitly invoked, they still require their own matching manual/MCP;
missing prerequisites block that workflow, without a project-local substitute.
Optional Context7,
GitHub and CodeGraph connections depend on actual availability; do not claim
tools exist or make them service/build dependencies. Do not change global models,
providers, trust or approval settings to make a check pass.
