# R01 — Build and publish a pre-release candidate

Outcome: an immutable candidate is published and anonymously retrievable from all required channels, ready for real GPU qualification.
Requirements: REQ-015, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-25): `v0.1.0-rc.2` is published as
`candidate_published_unqualified`. The user authorized the corrections,
commit/push/PR/merge after successful CI, publisher setup and publication, then
explicitly selected rc.2 because the existing rc.1 tag could not be moved.
PR #9 and release run 40212576458 passed; all six distribution channels and
GitHub assets passed anonymous read-back. Final pinned specification check passed
and the exact R01 agreement remained ready. GPU acceptance and stable publication
were not performed. See the execution evidence below and current handoff.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: I02/C02 complete, actual protected publisher access and usable Git; current complete non-GPU build/check evidence. Read spec.md REQ-015/026–028; core/contracts.yaml delivery.images/chart/versioning/pipeline/release; quality/scenarios.yaml Q-RELEASEBUILD/Q-DISTRIBUTION/Q-RELEASEGATE.

## Scope and ownership

Allowed: candidate release inputs/inventory/safe evidence, GitHub pre-release/tag/assets, both specified image channels and both chart channels through C02 automation. No stable/latest advancement, deployment, GPU success claim or unrelated registry writes. Runtime fixes belong to their affected packet; return there under a separate invocation rather than broadening R01.

U-RELEASE takes a chosen source and successful CI/build output, publishes exact image manifests and one chart archive, verifies every channel, and records candidate_published_unqualified. This state deliberately precedes P07/P08; R01 does not depend on GPU acceptance, preventing a dependency cycle. GitHub release must be marked pre-release and not latest. Clearly state qualification is outstanding.

Use v0.1.0-rc.N as the initial version convention; record exact Git/chart/image mapping. A failed build is fixed in its owning packet and the full affected pipeline rerun until correct. Do not weaken checks or relabel failure as qualified. Once a Git tag or artifact version exists, do not move it/change its bytes; choose the next RC for changed inputs. Retry the same version only with identical selected bytes and verified existing artifacts. R01 may be reinvoked for rc.2/rc.3 after fixes, with renewed affected evidence; there is no “latest RC” hidden mutable acceptance identity.

Publish API/backend manifests to GHCR and Docker Hub and verify their selected digest/attestations match. Publish one whisper-server tgz per version to GHCR OCI and Pages; preserve old index entries and compare extracted/downloaded tgz SHA256, recording the separate chart OCI manifest digest. Exercise chart image-registry overrides for both supported image registries. Pull with an empty disposable client credential config, never the maintainer's existing login; no secret prints. GitHub public visibility alone is insufficient.

Before any candidate publication, require C02's complete Security Check for exact source/build/chart identities and fresh supported DB/rule data; read core/contracts.yaml delivery.security_checks and Q-SECURITY. Record security_report_refs in the candidate inventory, including native component coverage gaps and any evidenced narrow non-applicability entry. Reconcile pre-push scan targets with actual uploaded manifests; stale, wrong-digest, failed or missing scans block. A fixed executable/dependency input after publication requires a new RC and renewed affected evidence.

## Verification and recovery

Write/run C02's candidate/inventory checks before publication. RED: failed build/job, digest mismatch, index loss or channel denial fixture cannot advance candidate state. GREEN: successful actual complete CI, clean builds, all uploads and anonymous read-back. PROVE newly added wiring checks by one reversible fixture bypass; never corrupt a real published artifact to prove a check.

Planned commands: `rtk proxy python3 tools/release/check.py --inventory <safe-inventory.json> --stage candidate` and C02's actual publication/read-back harness. Read back GitHub pre-release flags/source/tag and every channel identity; record safe CI refs, manifests, checksums, upstream/base/tool identities and qualification state. Hardware inference is explicitly not performed here.

**MANUAL GATE:** explicit R01 invocation with source/version and authority to publish to these channels. Protected namespace ownership/credentials must already be verified; do not ask again for writes already expressly authorized. Never publish from a dirty/unidentified build input or force-push tags.

Cross-channel publication is not atomic. A partial failure stays incomplete; record successful/missing channels and retry selected bytes without replacing prior stable/index versions. Do not set stable aliases to recover a candidate. Required access failure leaves R01 open with the credential/settings owner identified.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.

## Technical proposal

Use C02's release/security harness to select one immutable v0.1.0-rc.N candidate, build/fix/recheck until every required non-GPU check passes, then publish it as candidate_published_unqualified. Bind ../core/contracts.yaml delivery/versioning/pipeline/security_checks/images/chart/release and Q-RELEASEBUILD/Q-DISTRIBUTION/Q-SECURITY/Q-RELEASEGATE. A build/upload alone does not qualify GPU operation; P07/P08 remain mandatory.

The non-secret release inventory binds candidate source SHA, exact build/source/base/tool identities, both image manifests and Linux amd64 targets, native/model identities, actual SBOM/provenance, one chart archive SHA256 and a separate OCI manifest digest, successful CI/security references and per-channel state. Missing/skipped/cancelled/failed/stale/wrong-identity required results deny any release step. Fixes that change executable/runtime/build inputs get a new RC version; never overwrite a used version or silently move tags. Retry the same version only for identical selected bytes after inspecting already published state.

Publish both selected image manifests unchanged to the exact GHCR and Docker Hub repositories, one identical packaged chart to GHCR OCI and the classic Pages repository, and a GitHub pre-release/tag/assets. Preserve previous chart entries/checksums and stable aliases; candidate publication never advances stable/latest. Verify uploads and attestations and anonymously pull all channels with disposable empty credential configuration; use the chart with both image-registry choices. No maintainer-login pull counts as anonymous evidence.

Use the already-built C02 CLI's candidate gate before writes and bounded idempotent publisher/read-back operations afterward. Test failed build/security, identity mismatch, index loss and channel denial on disposable fixtures before real publication; PROVE only with fixtures, never corrupt a public artifact. Every per-channel partial write is recorded as incomplete and preserves prior stable artifacts. Do not report this packet complete before all required channels and read-backs pass.

Manual execution gate remains explicit authority for the selected source/version, protected publisher credentials/settings and external writes. This planning answer is not that authority and does not authorize commits/pushes, cluster deployment or GPU execution. If prerequisites are unavailable, keep R01 incomplete and name the established owner. Narrow implementation fixes return to their owning packet; this packet cannot bypass tests or publish changed bytes under the existing candidate version.

## Technical agreement

Selected proposal: `r01-immutable-candidate-v1`. Packet: `R01`.

- proposal_digest: `7823ded89bba909fb9da863071a0da3784104e9a2cd40a0f9704c33262af0ad0`
- agreement_ref: `{id: 19d3f11e-13ba-4c7a-b727-511a0c24a7d2, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Confirm separately authorized source/version/publisher target and current complete C02 evidence.
2. Run candidate denial/retry fixtures and the real gate before any public write.
3. Build/select immutable artifacts and publish the candidate through the authorized C02 harness.
4. Read back GitHub metadata, both images/registries, OCI/classic chart bytes/index and attestations anonymously.
5. Record candidate_published_unqualified only after all required checks pass; retain partial failure evidence and stop for GPU qualification.

## Execution evidence — 2026-10-25

- The user selected `v0.1.0-rc.2` after read-back showed the rc.1 tag already
  existed. Neither tag was moved. Candidate source:
  `3faa5cffa82917e590994cf1d2edcd2256d74c33` (merged PR #9).
- Historical RED: [release 40199668493](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40199668493)
  passed its builds/runtime/scans but failed inventory qualification before any
  registry writes. The qualifier required a build field absent from scanner output.
  PR #9 takes that field from the selected inventory and updates its behavior test.
- GREEN: [PR #9](https://github.com/swimmwatch/openwhispr-whisper-server/pull/9)
  [CI 40207794716](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40207794716)
  passed all seven jobs, including runtime and final Security Check, before merge.
  [Release 40212576458](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40212576458)
  passed cold builds, runtime/license/Compose, all 22 fresh security targets and
  every candidate publication job. Stable-only aliases were intentionally skipped.
  Existing CI publisher/scanner denial fixtures passed; this continuation added
  no behavior or absence/wiring assertion requiring another PROVE mutation.
- Anonymous local read-back used an empty task-local Docker configuration and
  isolated Helm settings: four `docker pull --platform linux/amd64` operations,
  OCI and classic `helm pull`, manifest/config/SPDX/SLSA reads in every image
  repository, and credential-free downloads of all three GitHub assets.
  `sha256sum --check SHA256SUMS` and archive `cmp` passed. The chart rendered
  successfully with each registry's exact image digests; normalized outputs match.
- `tools/release/check.py --stage candidate` on the downloaded inventory returned
  `candidate_published_unqualified`; `tools/security/check.py --stage complete`
  passed the actual downloaded release security report. Tools ran with project
  uv 0.13.5 / CPython 3.14.9; Helm 4.3.0 and Docker 29.9.2 performed local pulls.
  The older uv on default PATH was not used after its version rejection. The
  sandbox cache-access denial was retried through native approval, not bypassed.
- Exact API/backend index digests, chart archive SHA256, separate chart OCI digest,
  build/runtime identities and safe local evidence paths are in [handoff.md](handoff.md).
  The earlier Pages index was empty, so no previous published chart was available
  for a real older-version pull. Prior index/history preservation and denial/retry
  behavior remain covered by the successful CI publisher fixtures.
- GitHub metadata read-back confirms public pre-release, not draft and not latest.
  The inventory has no GPU reports. Three applicable native MEDIUM findings remain
  disclosed; zero blocking findings and zero coverage gaps is not zero findings.
- Final MCP checks: pinned Approved/reviewed specification `pass`; exact R01
  agreement `ready`, no issues. No runtime source, workflow, protection, global
  agent setting, stable/latest alias or GPU/cluster workload changed in this
  continuation beyond the already merged qualifier correction and authorized RC
  publication. Subsequent packets remain unchecked and are not started.
