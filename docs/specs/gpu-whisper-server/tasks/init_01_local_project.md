# I01 — Local project initialization

Outcome: a portable Git repository, Go module and real contributor tooling without requiring a GPU.
Requirements: REQ-024.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: completed on 2026-10-10; standalone bootstrap checks passed. Confirmed scope and pinned Approved specification/review were verified through workflow MCP before work and completion. The evidence below covers only I01; subsequent packets still require explicit invocation and passing prerequisites.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: reviewed inputs and explicit bootstrap invocation. This is the one-time standalone bootstrap **before** incremental-implementation: that workflow requires usable Git and therefore cannot perform its own missing prerequisite. Planning has not initialized Git. A clean tree or initial commit is not required.

Evidence at planning: only docs/specs/gpu-whisper-server exists; no .git or .codegraph exists at the target root. Verify current state again. Read spec.md REQ-024 and core/contracts.yaml delivery.repository, delivery.pipeline.pins; quality/scenarios.yaml Q-INIT.

## Scope and ownership

Allowed: local Git initialization only in this root; go.mod/go.sum, .editorconfig, .gitattributes, .gitignore, initial .dockerignore, tools/versions.yaml. One-off verification scripts and their dependencies belong in ignored .local/; do not add a permanent bootstrap framework. Extend existing equivalent files instead of adding competing tooling. Preserve all specification files. No remote repository, runtime feature, generated dummy service/test, dependency upgrade outside agreed baselines, commit or publication.

U-BOOTSTRAP owns Git/tool/source boundaries and contributor checks. Inputs: selected repository identity, approved Go baseline and actual tool distribution/version data. Outputs: module github.com/swimmwatch/openwhispr-whisper-server, main branch, readable staged/untracked status, pinned check dependencies and documented invocations in tool metadata. Runtime layers later depend on this module; tooling never becomes a runtime dependency.

Initialize a new Git repository only when no enclosing Git repository owns this root. If an existing/enclosing repository is discovered, verify ownership and preserve it; do not reset/reinitialize another worktree. No initial commit is needed. Record actual Go 1.26.5 resolution and check-tool versions/immutable identities; do not invent downloads/digests. Use an isolated, pinned local verification environment under .local/ when needed, without modifying global Python or introducing project dependencies solely for one-off checks.

Record verified compatible scanner distribution identities for govulncheck, gosec, Hadolint and Trivy from core/contracts.yaml delivery.security_checks in the tool manifest. Actual source/image/Helm scans are C01/C02; tool presence in an empty repository is not passing product evidence.

Use LF for text, correct binary attributes and conventional editor defaults. Ignore build outputs, local credentials/config, agent histories, temporary audio, model weights/cache and local release evidence with private content. Keep public fixtures, safe reports, source, Go sums, chart inputs and project skill/config files trackable. Initial .dockerignore excludes .git/private state/weights/audio while retaining intended source and license inputs; P05 narrows it against real builds.

## Discriminating checks

Prepare a disposable verification script under .local/ before settings/module changes. It checks module identity, recorded tools, text/binary attributes, exclusions and required tracked inputs. Use a disposable fixture containing a seeded credential/history/model/audio plus legitimate source/license and verify exclusions are selective; no real secrets. RED is an incorrect classification/module/tool value, not merely absent files. PROVE by removing one ignore rule and observing detection.

Planned commands: a disposable .local/ probe plus `rtk proxy go mod download`, `rtk proxy go mod verify`, `rtk proxy git status --short --untracked-files=all`, `rtk proxy git check-ignore --no-index --stdin` with fixture paths supplied on stdin. Record actual binary/text round trips and a fresh disposable checkout/worktree check using only project prerequisites. Before a first commit exists, use a temporary local initialized repository with the same scoped files for checkout checks; do not make a project commit to enable the check.

No Go packages exist yet: do not present “no packages to test” as passing application tests or add fake packages. Contributor check runs may state the honest bootstrap stage; C01 adds contract checks and F01 adds real Go behavior/architecture checks.

## Recovery and gates

Missing tools or unsafe enclosing Git ownership fail clearly without touching global settings. Correct exclusions/pins and rerun. Local initialization is reversible but do not remove another .git or discard files. **MANUAL GATE:** explicit invocation of this standalone bootstrap is the authority for local initialization; it does not authorize commits or remote writes.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.

## Execution evidence — 2026-10-10

The user explicitly invoked standalone bootstrap I01. Git now has a main worktree, zero commits, no remotes and no staged diff. The linked specification remained Approved with the exact pinned source/specification/review versions at completion. Eight bootstrap files were created; specification requirements and workflow-owned YAML were preserved.

Historical commands executed on 2026-10-10 (the bootstrap script and its lock were retired to .local/ on 2026-10-11; these are evidence, not current contributor instructions):

```bash
rtk proxy .venv/bin/python -m pip install --require-hashes -r tools/requirements-checks.txt
rtk proxy .venv/bin/python tools/check_bootstrap.py --docker-context
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod download
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod verify
rtk proxy .venv/bin/python -m pip check
rtk proxy git rev-parse --is-inside-work-tree
rtk proxy git status --short --untracked-files=all
rtk proxy git diff --cached --exit-code
```

All commands exited 0. Bootstrap check covered 35 selective paths, real Git LF/binary round trips and real BuildKit context exclusions. Module download honestly reported no dependencies; no go.sum or fake Go packages were introduced. The contributor-only lock pins PyYAML 6.0.3, jsonschema 4.26.0 and their five resolved dependencies with publisher hashes.

RED used a seeded temporary Git policy that allowed private inputs; it failed on those paths, not on missing files. GREEN passed the real policy. PROVE removed the secrets rule independently in Git and Docker fixtures, then restored it; each removal was detected. Separate incorrect module identity, Go tool metadata, PNG attribute and EditorConfig line-ending mutations were detected and restored. Final checks passed.

A temporary local repository with the same eight bootstrap files was committed and cloned, then installed the hash lock into a new virtual environment and passed bootstrap/Docker/module/pip checks. Only that disposable repository had a verification commit. The temporary checkout probe and fixtures were removed after recording results.

tools/versions.yaml records the official Go 1.26.5 Linux amd64 archive SHA256 and actual compatible scanner identities: gosec v2.29.0, Hadolint v2.15.1 and Trivy v0.75.0 archive/binary checksums and executable versions; govulncheck v1.8.0 authenticated module sums, isolated Go 1.26.5 build and executable version. No full scanner, application, image, Helm or GPU acceptance checks were run; those remain later-packet work.
