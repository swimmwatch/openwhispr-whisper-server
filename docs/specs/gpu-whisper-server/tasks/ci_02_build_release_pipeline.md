# C02 — Complete build and release automation

Outcome: complete CI and a controlled multi-channel release pipeline with discriminating failure gates.
Requirements: REQ-015, REQ-019, REQ-023, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Read linked requirements through workflow MCP; do not read manifest/decisions or review archives through shell. Recheck guidance and .codegraph before code lookup and pinned specification freshness before work and completion. Every packet requires its own explicit invocation; planned work is not implementation authority.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: C01/P05/P06 complete; both real image builds and chart checks exist. Read spec.md REQ-015/023/026–028; core/contracts.yaml delivery.images/chart/versioning/pipeline/release and packaging/deployment; quality/scenarios.yaml Q-CI/Q-RELEASEBUILD/Q-DISTRIBUTION/Q-RELEASEGATE/Q-CONTRACTS.

## Scope and architecture

Allowed: .github/workflows CI/release/Pages/security jobs, scoped check/update settings, tools/release/**, tools/security/**, narrow Hadolint/Trivy configuration and exception records, tests/tooling/**, tools manifests/check dependencies, docs/releases.md, actual local agent-skill command references. Reuse runtime/package/chart harnesses; do not reimplement them. No production service feature, real candidate/stable publication or cluster deployment in this packet.

U-CI validates source and builds artifacts; U-RELEASE validates a fixed inventory and performs bounded idempotent channel steps. Separate trusted build/validation from protected publisher jobs. Release inventory uses authoritative delivery names and fields: candidate/build/release source identities, immutable inputs, exact manifests/chart archive and OCI digest, CI/GPU report refs and per-channel state. API and backend are distinct images; preserve one selected manifest identity across GHCR/Docker Hub. Chart archive SHA256 is distinct from chart OCI manifest digest.

Complete required CI: module verification, formatting/vet/real Go tests/race and F01 import-boundary/strict-config/middleware checks, contract/schema/example checks, actionlint/Dockerfile lint, both clean Linux amd64 builds without GPU, source/CUDA-architecture metadata, image runtime/components/license/security reports, Helm lint/values schema/render/Kubernetes API checks/package and current docs/settings. At least one release build is cold with no reused layers; untrusted cache is not a trusted release input. Missing/skipped/cancelled required jobs fail the aggregate gate. Security findings are triaged by applicability; blocking exploitable findings cannot be ignored to make green.

BuildKit exporter must preserve actual SBOM/provenance attestations. Check uploaded-manifest compatibility and metadata for both channels during R01; an SBOM file from unrelated output is not evidence. Pin sources/bases/toolchains/action SHAs and scanner databases/report dates; record actual redistributed licenses. No secret in build args or provenance. Runtime image metadata retains original source revision at promotion.

Implement chart package/index operations preserving old index entries/checksums, serialized publication without cancelling an in-progress publisher, and retry with immutable-byte collision checks. Helm package/push uses actual pinned versions; OCI destination excludes chart name/tag. Pages carries the same packaged tgz and merged index. Check manifests/archive after upload and anonymous read-back before advancing state. Partial writes set incomplete and preserve prior stable; never rewrite a used version with different bytes.

The gate accepts exactly identified successful CI plus both successful target reports and final documentation against selected runtime/build inputs. Tests deny a missing/failed/skipped job, report mismatch, source/runtime drift, unfinished docs and channel failure. Stable preserves accepted executable manifests, packages a new stable chart, and validates metadata-only diff/rendering equivalence; changed runtime behavior requires a new candidate and affected acceptance.

## Required Security Check

Read core/contracts.yaml delivery.security_checks and quality/scenarios.yaml Q-SECURITY. Create tools/security/check.py and narrowly scoped scanner configuration/exception validation, reusing existing project checks. Pin verified govulncheck, gosec, Hadolint and Trivy versions in the tooling manifest; test their actual compatibility with Go 1.26.5 and release build settings rather than installing latest. Keep optional gosec AI features disabled.

Run govulncheck and gosec over owned production Go packages for both API and supervisor with release GOOS/GOARCH/build tags. Run Hadolint on both actual Dockerfiles and Trivy misconfiguration checks on Dockerfiles and Helm. Helm checks use default and supported P06 values, actual rendered manifests and the extracted packaged chart. Validate non-root/privilege/token/GPU/pairing invariants with existing contract checks where built-in rules do not cover them. Perform Trivy vulnerability and secret scans of both actual image identities, including image configuration/history, plus current project source and extracted chart secret scans.

An aggregate job named Security Check must depend on every required scanner result and validate target coverage/identities. No scanner error, missing/skipped matrix entry, stale data or empty/unsupported target can be reported clean. Trivy HIGH/CRITICAL vulnerabilities/misconfigurations block, including unfixed entries; secret findings and unresolved gosec/reachable govulncheck findings block independently. Do not add ignore-unfixed, no-fail or continue-on-error to obtain green. Collect lower-severity findings for triage.

Only demonstrated false-positive/non-applicability suppressions are permitted, scoped to exact rule/advisory and path/package with rationale, maintainer and expiry. Validate inline #nosec/Hadolint ignores as well as Trivy policy; no naked/global rule exclusion. Trivy's YAML ignore format is version-sensitive, so use the verified pinned format and an explicit project validator for mandatory expiry/owner/scope. An expired or broadened entry fails.

Reports bind scanner version, vulnerability DB/rule identity/time, source SHA, exact image manifest and resolved Linux amd64 target, chart SHA256 and values identity. Reconcile detected packages with native upstream/FFmpeg/CUDA source inventory/advisories; document coverage gaps instead of treating unidentified components as scanned. Redact secret matches, snippets and sensitive fields before logging or uploading JSON/SARIF; raw secret reports cannot be public artifacts. Fork-safe checks need no publisher/code-scanning write token; SARIF upload is presentation and never substitutes for a failing check.

Planned scanner commands, after resolving actual paths/identities and creating safe output directories:

- `rtk proxy govulncheck ./...` with the release Go/build configuration; default text failure status is the gate.
- `rtk proxy gosec -track-suppressions -fmt sarif -out artifacts/security/gosec.sarif ./...`.
- `rtk proxy hadolint <api-Dockerfile> <backend-Dockerfile>`.
- `rtk proxy trivy config --exit-code 1 --severity HIGH,CRITICAL containers`.
- `rtk proxy trivy config --exit-code 1 --severity HIGH,CRITICAL --helm-values <matrix-values.yaml> charts/whisper-server`, plus the same config check on saved actual helm template output.
- `rtk proxy trivy image --platform linux/amd64 --scanners vuln --exit-code 1 --severity HIGH,CRITICAL <image-by-digest>` for both images; before first push, scan the exact exported local build artifact and link its identity.
- `rtk proxy trivy image --platform linux/amd64 --scanners secret --image-config-scanners secret --exit-code 1 <image-by-digest>` without a severity filter.
- `rtk proxy python3 tools/security/check.py --stage complete --inventory <safe-inventory.json>`, the new aggregate/coverage/suppression/report-redaction harness to create.

Use the harness for current-source/extracted-chart secret scanning with explicit exclusions for Git/private state and MCP-owned workflow files/archives; do not read them as a workflow fallback. Execute secret-scanner calls through a wrapper that captures raw output in private disposable storage and sanitizes it before any console/artifact output; the scanner command examples name the underlying calls. These commands are planned interfaces, not previously passing runs.

Security RED evidence uses tiny isolated fixtures: a reachable Go advisory from a fixed test DB (no vulnerable code deployed), unsafe Go source, a root/unsafe Dockerfile, privileged/token-bearing rendered manifest, fake secret and a machine-readable govulncheck finding whose process exits zero. Assert expected rule/target plus aggregate denial, then GREEN with corrected fixture/project and real scans. Also reject stale/wrong-digest reports, expired/broad suppression and a missing matrix entry. PROVE an unexercised gate assertion by bypassing one predicate and observing the owning test fail; restore. Scanner DB/tool/network failure is an incomplete check, not expected vulnerable-fixture RED. Static scans do not substitute for P07/P08 actual resource/privacy/CNI tests.

## Commands and test-first checks

Create a small project release CLI/harness at tools/release, with unittest coverage in tests/tooling before workflow mutation. Proposed invocations to implement: `rtk proxy python3 -m unittest discover -s tests/tooling -v`; `rtk proxy python3 tools/release/check.py --inventory <safe-inventory.json> --stage candidate`; `rtk proxy python3 tools/release/check.py --inventory <safe-inventory.json> --stage stable`. These are new interfaces, not existing commands. Their documented input paths must never contain secret values.

RED uses failed required job, wrong pulled digest/archive, preserved-old-index loss and absent GPU report fixtures, one fault at a time; GREEN is correct state denial/recovery plus all real non-publishing CI jobs. PROVE by bypassing one gate predicate and observing its owning assertion fail. Run actual P05/P06 build/check commands and remote CI, not fixture-only builds. Use a disposable registry/index fixture for publisher failure/retry; real anonymous pulls are R01/R02.

**MANUAL GATE:** protected publisher credentials/settings require scoped user setup (GHCR job-scoped GITHUB_TOKEN, Docker Hub scoped token, separate Pages rights). Document secret names only; never print/store values or add kube credentials. Actual release writes remain separately invoked R01/R02.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.
