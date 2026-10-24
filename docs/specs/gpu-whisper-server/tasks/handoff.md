# Current handoff

Packet: **C02 — local checks pass, required remote qualification incomplete
(2026-10-24)**. Baseline I01/I02/I03/C01/F01/P01–P06 is complete.
C02 stays unchecked; R01 is not eligible. No commit, push, publication, deployment,
external settings or global agent changes were performed.

## Verified workflow inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`
- Proposal: `c02-security-release-ci-v2`.
- agreement_ref: `{id: 0707c637-98d3-4781-bc32-f86d5ba105b6, revision: 2}`.
- proposal_digest: `582e82cbffbf7364569716554a5cc603dc22c3a2dad1fc98db89b5a3e423b6be`.
- Matching manual/Git preflight passed. Specification is Approved/reviewed.
  The frozen technical proposal is preserved. Final checks returned specification
  `pass` and exact v2 agreement `ready`, with no issues.

The user selected a separate inventory metadata commit. Artifact revisions name
already successful trusted main CI runs. Only the chosen inventory file may
differ in the approval checkout. Candidate image provenance is never rewritten.

## Implemented and checked locally

Trusted CI/security verification, immutable image/chart publication, index
preservation, explicit Pages deployment and selected-image stable promotion are
wired. Stable uses accepted image manifests and the current stable chart;
actual five-profile Helm equivalence denies runtime changes.

Alias publication snapshots previous identities before writes, performs exact
read-backs and restores existing aliases on failure. An immutable pre-write
artifact survives runner loss; same-run retries restore it by ID. Conservative
rollback includes remote writes lost before progress upload. New aliases without
a previous value remain explicit partial state; immutable manifests are never
deleted to conceal failure. Changed retry inputs and arbitrary conditional CI
bypasses are rejected. See [release operations](../../../releases.md).

Current backend GnuPG/OpenSSL package findings are remediated. Native review is
bound to actual build inputs and reverified primary-source hashes. All 22 targets
pass with no blocking findings or coverage gaps. Three applicable MEDIUM findings
remain visible: FFmpeg CVE-2026-107676/CVE-2026-107678 and whisper.cpp
CVE-2026-17513. Two exact Echo Proxy exceptions are supported by both binaries'
linked-symbol tests plus a positive Proxy fixture. Echo remains v5.4.0.

## Actual current local evidence

Pinned Go 1.27.3 and private Python/tool installations were used.

- Tooling: 100 tests, no skips, including actual scanner and disposable publisher
  fixtures (`.local/workflow-optimization/tooling.log`).
- Source: `go test -race -coverprofile=... ./cmd/... ./internal/...` passed.
  This invocation's command/internal statement coverage is 90.9%; it is not a
  task-wide or GPU coverage claim (`go-final.log`, `coverage-final.log`).
- Images/Compose/Helm: `go test -race ./tests/integration -count=1 -timeout=15m -v`
  with P05/P06 flags and the current images passed in 372.625s
  (`runtime-final.log`). The earlier unsupported FFmpeg `-parsers` harness
  failure was corrected with an actual libavcodec parser registry probe.
  The current profile is GREEN; the old API image is semantic RED.
- Format/vet/module verification, architecture, contracts, agent setup,
  actionlint, Hadolint, current-source secrets and the complete security CLI pass.
- Seven isolated in-memory guard mutations were detected without harness errors
  or source mutation (`prove-remaining.log`). The missing pre-write workflow
  snapshot was semantic RED and is now GREEN.
- Post-edit contracts/agents/actionlint/current-source secrets and the complete
  security CLI pass. Scoped tracked/untracked whitespace checks pass.
- Private logs/scripts are in `.local/c02/resume/` (mode 0700).
  They are excluded from source/image/security artifacts.

Current artifacts are `.local/c02/current-artifacts/`:

- API: `sha256:f499a34b2ee4f5c5a33ffe181d335d9f8157f78d0ce1f2bfb0f655442bbbbb24`.
- Backend: `sha256:797f5db62d423cab41169da42b885232994a4d8bc60f602281188dd7418670b2`.
- Chart archive: `2bbeab744544aa60f5cdf808920b7a65917154db223fd4b30cbaa7b587129ec0`.
- Both images retain actual SBOM/provenance and verified OCI identities.
  They are cached local builds (`cold=false`) from a dirty checkout. Their
  source labels do not prove those bytes exist in the labeled Git commit.
  Earlier cold exports remain historical, not current clean qualification.

## Workflow optimization — 2026-10-24

Both images now use a two-role matrix, preserving actual qualification names.
PR layers, Go/pip dependencies and SHA256-verified tool downloads are cached;
main image builds remain cold. Trivy caches only its vulnerability database,
without disabling refresh. Scans and runtime checks run independently before the
final always-run aggregate. Go/tool/BuildKit concurrency follows `nproc`; existing
native memory/quota limits remain authoritative.

Latest local evidence is `.local/workflow-optimization/`: 100 tooling tests,
four additional detected in-memory bypasses, actionlint/contracts/agents and
the actual 22-target Security Check with persistent DB input pass. A real
BuildKit fixture imported its cached COPY layer and retained OCI attestations.
Five tool/schema installations measured 6.383s cold versus 0.515s warm locally.
These are not full GitHub/CUDA build timings. Native runtime/image inputs are
unchanged, so the earlier actual image/Compose/Helm run remains applicable.

## Required remaining work and ownership

Next owner/action: the C02 implementation agent must run actual nonpublishing
GitHub CI on approved clean committed inputs, including current cold image builds,
after separately authorized commit/push scope is provided. No such authority is
implied by this packet invocation. Protected environments/credential prerequisites
must be confirmed before independently authorized publication.

C02 cannot be marked complete on local fixtures or dirty cached artifacts.
R01/R02 publication and P07/P08 hardware acceptance remain separate.
