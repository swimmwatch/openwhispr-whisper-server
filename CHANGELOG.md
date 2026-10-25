# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog 2.0.0](https://keepachangelog.com/en/2.0.0/),
and this project follows [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
No release has been published; release dates and tag/comparison links will be
added when releases exist.

## Unreleased

### Added

- Helm application chart with ordinal-paired API/backend StatefulSets, private per-pair policies, retained storage, canonical configuration and validated packaging.
- Separate pinned API/CUDA-backend image definitions and canonical Compose inputs, with packaging, source identity, isolation and standalone lifecycle checks.
- Public HTTP, bounded FFmpeg audio-normalization and paired-backend client adapters with validated input, resource ownership and protocol-neutral failures.
- Public API command with bounded FIFO admission, independent health observation, operation deadlines, safe cancellation/recovery and graceful shutdown.
- Resident backend command with private HTTP inference, CUDA-evidence readiness and supervised native-process recovery.
- Initial Go module, source/build exclusions and tool-version metadata.
- Apache-2.0 license for project-owned work, community policies and contribution templates.
- Initial Go dependency-update configuration.
- Pinned commit, release-version and changelog conventions with contributor examples.

### Changed

- Run tag-driven releases on GitHub Release publication or manual dispatch, build
  RC images in parallel, and share the native image-build Action with PR CI.
- Resolve release source qualification from the successful merged PR instead of
  requiring a duplicate CI run on `main`; preserve stable GPU acceptance and
  promotion of the accepted image manifests.
- Run contributor CI only on pull requests instead of repeating it after each
  push to `main`.
- Centralize workflow settings in one local CI configuration Action; read Python,
  uv and native tool versions from project files instead of repeated literals.
- Add Helm chart discovery keywords, SPDX license, OCI documentation and Artifact
  Hub links/category metadata; verify package, repository-index and OCI read-back.
- Add parameterized OCI image metadata and SHA-pinned native metadata generation
  for consistent image labels, manifest/index annotations and registry read-back.
- Parameterize Docker build inputs and package pins, use readable heredoc blocks
  and share CPU/quota/memory-aware compilation sizing without adding runtime tools.

- Use pinned Docker and Aqua Security Actions for CI builds, registry logins and
  source secret scans; preserve parallel builds, PR caches and cold release checks.

- Manage Python tooling with uv 0.13.5, pin CPython 3.14.9, and use one project
  lock for contributor setup and CI dependencies.
- Refactor Python tooling into typed policy, artifact and installation modules;
  add behavior tests, pinned Ruff/mypy checks and documented Python conventions.

- Cache CI dependencies and PR image layers, parallelize tool installation and independent checks, and preserve cold main-build qualification.
- Persist Go and C/CUDA compiler caches between image builds, select compile parallelism from available resources, and keep runtime layers reusable when OCI labels change.

### Removed

- Legacy main-push CI selection and inventory-only approval-commit paths from
  release tooling; tag resolution now verifies the successful merged PR.
- Custom YAML syntax restrictions, the Python Kubernetes-schema walker and its
  unused schema downloads and jsonschema dependency.
- Source-comment suppression checker and its tests; native GoSec and Hadolint
  options expose findings to the existing centralized exception policy instead.
- Exact-text assertion for generated BuildKit configuration.

### Fixed

- Respect cgroup v2 CPU quotas when automatically selecting image compilation parallelism, including fractional CPU limits.
- Propagate failures from every command in Docker build pipelines using Bash with `pipefail` in both images.
- Preserve string-valued Helm labels/selectors and reject cache paths overlapping configuration, transient or prepared mounts.
- Make Compose publication and healthchecks configurable for non-default API/backend listener ports, while keeping the API host port independent.
- Wait for confirmed native-process retirement during forced API shutdown; fail shutdown when backend recovery exceeds the cleanup budget.
- Abort cancelled transcription requests immediately after readiness checks, without implicit success responses or further admission.
- Bound private HTTP cleanup for incomplete uploads and early rejections; prevent stalled connections and implicit success responses after read cancellation.
- Reject equal backend/native ports within each configuration source before later overrides can hide the error.

### Security

- Update the pinned Go toolchain to 1.27.3 to address GO-2026-6809 in crypto/x509.
