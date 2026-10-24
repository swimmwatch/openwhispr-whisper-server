# C02 — Complete build and release automation

Outcome: complete CI and a controlled multi-channel release pipeline with discriminating failure gates.
Requirements: REQ-015, REQ-019, REQ-023, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: local checks pass, incomplete pending actual remote qualification (2026-10-24). All 100 tooling tests pass without skips, including actual scanners/publishers, selected-image promotion and runner-loss alias recovery. All 22 security targets have zero blockers and no coverage gaps; three applicable native MEDIUM findings remain reported. Current image/Compose/Helm race checks pass (372.625s). Current exports are cached dirty-checkout evidence; trusted remote CI/current committed cold-build qualification remains required. The frozen technical proposal is unchanged. Older checkpoints below are historical; see the current local checkpoint at the end.

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

Read core/contracts.yaml delivery.security_checks and quality/scenarios.yaml Q-SECURITY. Create tools/security/check.py and narrowly scoped scanner configuration/exception validation, reusing existing project checks. Pin verified govulncheck, gosec, Hadolint and Trivy versions in the tooling manifest; test their actual compatibility with Go 1.27.3 and release build settings rather than installing latest. Keep optional gosec AI features disabled.

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

## Technical proposal

The release inventory is reviewed in a separate metadata commit. Its artifact
source revisions refer to already completed trusted main CI runs, not to the
inventory commit itself. The publisher verifies that the checkout has identical
runtime inputs and chart package inputs to the selected artifact revision;
only inventory/approval bookkeeping may differ. Candidate image provenance keeps
its original source SHA. Stable promotion selects the accepted candidate run,
checks rendered chart equivalence and rescans those unchanged image digests with
the stable chart. The approval revision never overwrites artifact provenance.

Extend the existing GitHub Actions contributor workflow, separating untrusted PR validation from protected trusted publication. Bind ../core/contracts.yaml delivery/pipeline/security_checks/images/chart/versioning/release and Q-CI/Q-RELEASEBUILD/Q-SECURITY/Q-DISTRIBUTION/Q-RELEASEGATE/Q-CONTRACTS. Reuse F01/P01/P05/P06 checks and the existing source-secret wrapper; do not duplicate service/image/chart verification. Keep hosted Linux amd64 builds GPU-free; no self-hosted GPU PR runner or Home Lab credentials/deployment.

Add only necessary maintained Python 3.11+ stdlib orchestration under tools/security and tools/release, with unittest fixtures under tests/tooling; reuse already approved parser/validator dependencies where needed. Binding CLIs are tools/security/check.py --stage complete --inventory <path>, tools/release/check.py --inventory <path> --stage candidate|stable, plus scoped publication/read-back operations owned by tools/release. Commands consume a non-secret versioned inventory with core release fields and per-channel state; exit nonzero on failed, missing, cancelled, skipped, stale, unsupported or wrong-identity required evidence. Inventory is serialized data, not a generic workflow engine or alternate specification journal. No actual candidate/stable publication in this packet.

Complete Go/module/format/vet/race/import/contracts/examples, actionlint, both clean images, CUDA/component/license/SBOM/provenance checks, Helm schema/render/package matrix and current docs/settings checks. Pin actual compatible action SHAs/tool versions/source/base digests and record identities; version resolution may not change fixed service baselines. Keep untrusted cache isolated and require a cold release build. Preserve/inspect actual BuildKit attestations on selected image manifests, not a detached unrelated report.

The named Security Check aggregates govulncheck and gosec on owned production packages/both entry points, Hadolint on both Dockerfiles and Trivy config/vulnerability/secret scans of source, exact images/config/history and default/custom/rendered/extracted-chart targets. Match release GOOS/GOARCH/tags/toolchain. Enforce core severity/applicability rules; JSON exit-zero findings still fail. Secret scans have no severity filter. Capture raw secret results privately and redact before console/artifact output. Validate scoped false-positive/non-applicability exceptions, inline suppressions, owner/rationale/expiry and coverage identities; never use blanket exclusions, ignore-unfixed, continue-on-error or stale data to get green. Explicitly reconcile scanner blind spots for native/FFmpeg/CUDA components.

Use serialized trusted publishers with minimum job permissions and cancellation disabled during index/release writes. Copy selected manifests unchanged across GHCR/Docker Hub, package one chart archive, merge/preserve Pages index and handle exact-byte idempotent retry. Test publication through disposable fixtures; R01/R02 own real writes/anonymous pulls. Gate stable on both actual target reports, current CI/security, matching runtime/build/rendered inputs and final docs; docs-only differences need verified input equivalence.

Before workflow changes, test aggregate denial, fake-secret redaction, vulnerable/unsafe fixtures, missing targets, stale DB, digest mismatch, expired/broad suppressions and partial publisher retry. PROVE bypass of one gate predicate. Run actual nonpublishing jobs/remote CI under explicitly authorized push/settings scope; credentials/secrets/protections are a manual prerequisite and this agreement grants no push or settings authority. DB/tool/network failures remain incomplete.

## Technical agreement

Selected proposal: `c02-security-release-ci-v2`. Packet: `C02`.

- proposal_digest: `582e82cbffbf7364569716554a5cc603dc22c3a2dad1fc98db89b5a3e423b6be`
- agreement_ref: `{id: 0707c637-98d3-4781-bc32-f86d5ba105b6, revision: 2}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: the original C02 acceptance is retained in history. The actual synchronous Request User Input answer on 2026-10-24 selected a separate metadata commit for the release inventory; the managed refinement records that answer and preserves original artifact provenance.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Write inventory/gate/scanner/suppression/redaction/publisher failure fixtures before workflow changes.
2. Implement small repeated-check Python CLIs and actual pinned scanner adapters/coverage aggregation.
3. Wire untrusted PR and trusted build jobs, real P05/P06 checks and isolated caches with minimal permissions.
4. Exercise protected publisher mechanics in disposable fixtures and real nonpublishing CI under separately authorized remote scope.
5. Run tooling and full build/security/chart matrix plus gate PROVE; record required names/prerequisites and stop without a release.

## Execution checkpoint — 2026-10-23

Added `tools/release/check.py`, `tools/security/check.py`, `tools/install_ci.py`
and their `tests/tooling/test_release.py` / `test_security.py` tests. The release
gate denies unsuccessful/missing CI, wrong inputs/read-backs, partial channels,
missing target GPU reports, unfinished docs and image/render drift. Local
publisher primitives preserve old index entries and reject immutable collisions.
Security normalization covers exit-zero Go findings, secrets regardless of
severity, source/Dockerfile findings and blocking Trivy severity. Aggregate
coverage/identity/freshness and scoped exception validation have unit coverage.
These remain local foundations: complete scanner adapters, workflows, protected
publishers and real evidence acquisition are not implemented or qualified.

The initial module-import failures were harness RED, not semantic proof.
Subsequent isolated in-memory bypasses of the missing-GPU and scanner-freshness
predicates each failed the owning test, then were restored. All 34 tooling tests
passed, including the existing actual Trivy fake-secret/exclusion fixture.

Actual pinned scanner installation succeeded into `.local/c02/bin`. The
prebuilt gosec 2.29.0 could not decode Go 1.27.3 export data. Official-checksum
gosec 2.30.0 completed 33 production files without import errors; the verified
pin in `tools/versions.yaml` was updated. Its eight findings remain unresolved.
Hadolint 2.15.1 originally failed with five DL4006 findings. The user separately
authorized P05 rework on 2026-10-23. Both real Dockerfiles now use Bash with
`-euo pipefail`; actual Hadolint passes without ignores or suppressions. Twelve
Docker build probes verify all six stages propagate previously masked pipeline
failures. Both images rebuilt without GPU injection, and the full Go race suite
with actual P05 image/standalone checks passed. The latest [P05 CPU-quota correction](05_images_standalone.md#cpu-quota-correction--2026-10-23)
records current image/source identities and private evidence. The packaging
blocker is resolved; this does not complete C02 or authorize its automatic resume.

Actual govulncheck 1.8.0 completed: one non-call-reachable finding, zero blocking
call-level findings. Trivy 0.75.0 refreshed its vulnerability DB successfully.
None of those partial results constitutes a complete Security Check.

Private evidence: `.local/c02/` contains initial RED, gate/tooling/prove logs,
scanner reports under a private directory, `actual-scans-summary.json` and
`packaging-pipefail.patch`. [Release status](../../../releases.md) records usable
commands and missing integration. C02 did not change GitHub workflows, source
application, Dockerfiles or charts; Dockerfile changes belong to the separately
authorized P05 rework. No commit, push, settings, registry or cluster change was
performed.

## Resume checkpoint — 2026-10-23

Source delivery is available. Matching manual/Git preflight, Approved/reviewed
specification and the exact C02 technical agreement passed. Existing dirty work
and all production Go/Docker/chart sources were preserved.

Implemented actual scanner execution, exact gosec exceptions/inline policy,
private raw-output handling, safe normalized inventories, native-coverage review
validation, checksum-pinned installation, OCI/chart transfer verification and
real denial fixtures. Trivy Helm needs `--file-patterns helm:.*\.json` for the
chart's `.Files.Get` data files; omission silently skips the chart even with exit
zero. The actual unsafe-chart regression and an isolated PROVE mutation cover it.

Expanded contributor CI into source/chart/two build/runtime/Security Check jobs.
The aggregate uses `always()` only to reject unsuccessful dependencies, never to
bypass them. Safe scanner evidence is retained on failure. A separate manual,
protected main-only workflow wires immutable registry copies, chart OCI and
preserved Pages index operations with non-cancelling serialized publication.
Actual trusted GitHub run/jobs/security verification precedes credentials.
Stable metadata-only artifact selection and final alias integration are not yet
complete; the stable inventory validator alone is not a working promotion path.

### Actual local checks

- 59 tooling tests passed in 6.917s with all scanner/publisher flags enabled,
  no skipped tests. Fixed-DB reachable govulncheck, unsafe gosec/Dockerfile/Helm/
  Kubernetes, source-secret, registry copy/retry/collision and real Helm index
  fixtures ran. The final strengthened Helm assertion also passed separately.
- Go command/internal race tests, `go vet ./...`, module verification, contract
  checks, actionlint and agent structure passed.
- P06 actual Helm 3/4 and Kubernetes API-schema matrix passed in 11.482s.
- Both Linux amd64 images were cold-built without GPU injection or imported
  layer/compilation caches. Transient GitHub TLS/HTTP2 failures required retry.
  Initial export validation rejected empty subjects on unnamed BuildKit exports;
  the corrected inspector verifies their actual OCI attestation-reference binding.
  Both actual exports and their SBOM/provenance then passed the current inspector.
- Full real P05 image/metadata/license/Compose race checks passed in 338.748s
  against the newly loaded OCI artifacts, using separate C02 tags.
- Six isolated PROVE mutations (exact gosec scope, blob identity, inline scope,
  required jobs, publisher read-back and Helm JSON inclusion) failed their owning
  tests. They were in-memory only; no source mutation remains. Temporary probe
  code was removed; evidence stays under ignored `.local`.

Current local selected indexes (not published registry releases):

| Artifact | Identity |
| --- | --- |
| API OCI index | `sha256:c45a29caea189dcd016b28ec1bf7ccc9f93b941c81a64358cd1369781ad90345` |
| Backend OCI index | `sha256:405014d82fea9f8ba2043d1f830023924ad1c24cd5e6c29ebd1c75d51e64f861` |
| API Linux amd64 manifest | `sha256:23096e4fe2f9c1a4fe0a112fb00ed73f466de610151f5bb3f1794862d2afd7d4` |
| Backend Linux amd64 manifest | `sha256:864a557e44704b9095c1c9a52d483e0e137da68104f23c3641ab4e96c9bd15ff` |
| Build inputs | `sha256:dd3bf3dce701e591e17a9c157dd094237f202f85989a1acaf283c2cf155ebf9d` |
| Chart archive | `90373b1383e5183c29278a3ba459e3160d8ae40fa46b348e999eb1349490f2f2` |

The source label is local HEAD `776f8f601f0bbaf7ee1dc2c6931167d526c1bcce`;
uncommitted C02 tooling is not claimed to exist at that Git revision.

### Required failures and remaining work

All 22 scanner targets produced normalized reports. Gosec's eight findings are
covered by seven exact evidenced false-positive/non-applicability entries,
expiring 2026-11-22. Govulncheck has one non-call-reachable finding and no blocking
call finding. Both Hadolint checks, Docker config, five chart/five rendered cases,
extracted package config and all source/package/image secret scans have no
blocking findings. API vulnerability scanning has 29 lower-severity findings.

Backend vulnerability scanning has 311 findings, including 14 HIGH: ten GnuPG
package instances of CVE-2025-68973 and four OpenSSL/libssl instances of
CVE-2026-45447/CVE-2026-84782. No exception was added for these findings. Native
FFmpeg/whisper.cpp/CUDA advisory reconciliation is not complete. The aggregate
therefore correctly exits nonzero. Security Check is not passed.

Private evidence: `.local/c02/resume/` contains `tooling-final.log`,
`scanner-fixtures.log`, `publisher-fixtures.log`, `prove-current.log`,
`prove-helm.log`, `go-race.log`, `go-vet.log`, `chart.log`, `images-current.log`,
`security-current.json` and `security-current.log`. Actual immutable artifacts
are under `.local/c02/artifacts/`. None is uploaded or committed.

Next owner/action: the C02 implementation agent must finish native advisory
triage and stable artifact-selection/alias integration. Backend package
remediation is a P05 Dockerfile/build-input change outside this packet; explicit
additional scope is required before applying it. Remote CI additionally requires
an authorized push/run and protected publisher setup. No commit, push, settings,
real channel publication, deployment, global agent setting or Headroom change
was performed. C02 remains unchecked and R01 is not eligible.

Final freshness check: specification Approved/reviewed `pass` and exact C02
agreement `ready`, no issues. Current artifact transfer verification, formatting,
contracts, actionlint and scoped whitespace checks passed. The binding
`tools/security/check.py --stage complete` CLI rejected the actual inventory,
as required for the 14 HIGH findings and unresolved native coverage. Staging
remains empty. Next owner/action remains the C02 implementation agent for the
unfinished integration/triage; P05 remediation and remote execution need their
separate scope.

## Current local checkpoint — 2026-10-24

Supersedes the previous checkpoint's current blockers and agreement reference.
The active agreement is `c02-security-release-ci-v2`, reference
`{id: 0707c637-98d3-4781-bc32-f86d5ba105b6, revision: 2}`, digest
`582e82cbffbf7364569716554a5cc603dc22c3a2dad1fc98db89b5a3e423b6be`.
The user selected the separate inventory metadata-commit flow. Approval checkout
may differ from its successful trusted source CI commit only in that inventory.

Stable selects accepted image manifests and the current chart. Runtime/source/
manifest drift is denied. Five real Helm profiles verify metadata-only chart
promotion; selected images receive fresh scans with their original source SHA.
Pages deployment is explicit. Stable completion requires all immutable channels,
actual GPU/docs gates and exact alias read-backs.

Alias rollback now retains original identities across command interruption.
A separate pre-write snapshot artifact also survives full runner loss. Same-run
retry restores that immutable artifact before writes; conservative rollback
covers lost progress. Existing aliases are restored/verified on failure. A new
alias with no previous identity remains explicit partial state, without deleting
shared immutable manifests. Exact conditional-step checks preserve failure gates.

The former 14 backend HIGH package findings are remediated. Native review is
bound to build inputs, source hashes, owner and expiry. The current 22-target
Security Check passes with no coverage gaps. Applicable native MEDIUM findings
remain counted: FFmpeg CVE-2026-107676/CVE-2026-107678 and whisper.cpp
CVE-2026-17513. The two Echo HIGH/CRITICAL Proxy advisories are exactly scoped
non-applicability decisions backed by linked-symbol and positive-fixture tests;
the contracted Echo v5.4.0 remains unchanged. No applicable blocking waiver exists.

Actual local checks: 88 tooling tests without skips (including real scanner/
publisher fixtures), seven isolated semantic PROVE mutations, command/internal
race tests, full current image/Compose/Helm race tests (372.625s), vet, format,
module verification, architecture/contracts/agents, actionlint, Hadolint and
current-source secrets. The original unsupported parser probe was corrected;
actual runtime parser enumeration is GREEN on the current image and RED on the
old image. Final local identities/logs are in [handoff.md](handoff.md).

Current images have verified SBOM/provenance and OCI bytes, but `cold=false`
and an uncommitted checkout cannot prove a committed release revision. Earlier
cold artifacts are historical. No commit/push/publication/deployment occurred.
C02 remains unchecked; R01 is not eligible.

Next owner/action: the C02 implementation agent runs actual nonpublishing
GitHub CI on approved clean committed inputs, including current cold builds,
after separate commit/push authorization. Final specification/agreement freshness
and the local post-edit checks are recorded in the current handoff.

### Workflow optimization — 2026-10-24

The two image roles now share a parallel matrix without changing qualification
names. Go/pip dependencies and revalidated pinned tool downloads are cached.
PR-only layer caching uses local OCI export/import with bounded blob retention;
main builds remain exclusive and cold. The CLI rejects caches with `--cold`.
Trivy caches only its database and still performs normal freshness checks.

Security scans run independently of image/runtime checks. The final always-run
aggregate requires both plus source, chart and the complete matrix. Go/tool/
BuildKit parallelism follows available runner CPUs; native memory/quota caps
remain unchanged. Duplicate vet work is removed from tests while the standalone
vet gate remains required. Both Docker image loads are joined and fail the step
if either load fails.

Local validation: 100 tooling tests without skips, four detected cache-policy
mutations, real pinned BuildKit local-cache import/export and OCI attestations,
actual concurrent tool installation and the fresh 22-target Security Check pass.
The five-tool/schema installer measured 6.383s cold versus 0.515s warm locally.
This is not a GitHub or full CUDA-build timing. Parameters and verified sources
are documented in [release operations](../../../releases.md#ci-performance-and-cache-policy).
Private evidence is `.local/workflow-optimization/`.

Current remote CI/clean cold-build qualification remains required. Next
owner/action is unchanged: the C02 implementation agent runs it after separate
commit/push authorization. The frozen technical proposal is unchanged.
