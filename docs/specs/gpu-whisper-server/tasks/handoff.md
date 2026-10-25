# Current handoff

Packet: **R01 — in progress (2026-10-25, Europe/Moscow)**.
Baseline I01/I02/I03/C01/F01/P01–P06 and C02 are complete.
The user explicitly authorized committing the current changes, opening and
merging a PR after successful CI, configuring publisher environments and
publishing `v0.1.0-rc.1`. Branch: `feature/tag-driven-prerelease`.
No candidate has been published. GPU/cluster execution remains separate.

`release` and `release-pages` environments now require the owner as reviewer and
allow only `main` and `v*` tags. The Docker Hub username is configured; the owner
must provide `DOCKERHUB_TOKEN` through the protected environment before publishing.
The implementation agent owns the remaining PR/CI, merge, release and anonymous
read-back work. R01 remains unchecked until every required channel passes.

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

## Current release workflow refactor

The later user-approved tag-driven flow supersedes the former main-CI artifact
selection described in the historical C02 record. CI runs only on pull requests.
Release starts on `release: published` or manual `version_tag`: RC builds cold
images and a versioned chart; stable preserves accepted RC image digests and
checks the new chart for runtime equivalence. Both paths require successful
merged-PR CI and fresh security qualification before publication. Stable still
requires reviewed acceptance metadata and actual P07/P08 reports.

Build and runtime verification are shared with PR CI. Operator instructions and
current inputs are in [release operations](../../../releases.md#tag-driven-release-workflow).
This is a local workflow change; no new tag, remote run, publication or hardware
acceptance was performed. Historical C02 results below do not qualify this refactor.

## Historical local implementation and validation

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

Historical local artifacts, superseded by the trusted main build below, are in
`.local/c02/current-artifacts/`:

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

## Trusted main qualification — 2026-10-25

[CI run 40138859398](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40138859398) passed all seven required jobs on committed
source `76279e618a2692551bbdad48d2dd77461ad4b530`. Both Linux amd64 images were built
without GPU injection, with fresh isolated builders, `--no-cache` and no imported
PR layer caches. Export checks retained and verified SBOM/provenance attestations.

Runtime, component, license and Compose checks passed in 204.491s. All ten
resource-budget cases passed; the first cold backend CPU probe took 66.12s.
The former 60s deadline had failed twice in main run 40124786409. PR #7 increased
only this bounded probe budget to 180s; CPU, affinity, memory and override
assertions remain unchanged. The source job also passed production Go race
checks; packaging artifact checks ran without race instrumentation.

Security Check passed all 22 required targets against these same image indexes,
chart archive and source revision. Main protection requires this aggregate check
with strict up-to-date checks; unrelated protection settings were preserved.

| Artifact | Qualified identity |
| --- | --- |
| API OCI index | `sha256:40b6ce3d7e9b2d4cb97608798358f0373c52cd807f6ad443c676a447f4b34f75` |
| Backend OCI index | `sha256:5dd326f3e5ff3abcd1cd20a96eb4a9aee5a8a7c19b2d89ebc8b1b174e0d132bc` |
| Chart archive SHA256 | `42b8f493ec7d707f6551cd5752b4792e431919b7be304bf719e8f7227d47ce57` |

The inventory field `image_manifest_digests` binds the OCI index identities above,
including their executable manifests and attestations.
The exported Linux amd64 manifests and attestation identities were also checked.
Private evidence is under `.local/c02/ci-followup/main-76279e6/`; the GitHub run
contains the corresponding public job results and artifacts. Local cached/dirty
exports in earlier checkpoints remain historical and are not the selected
qualification evidence. Release publication and GPU/cluster acceptance remain
outside this packet. Final pinned specification check passed and the exact C02 agreement remained
ready after these bookkeeping changes; the frozen technical proposal is unchanged.

## Required remaining work and ownership

Historical C02 qualification remains recorded above. The current R01 source must
include the tag-driven workflow changes and pass PR CI before release builds.
The implementation agent must finish the authorized PR/CI and RC publication;
the owner must supply the protected Docker Hub token. Preserve existing artifact
versions and report any partial publication. P07/P08 and R02 remain separate.
