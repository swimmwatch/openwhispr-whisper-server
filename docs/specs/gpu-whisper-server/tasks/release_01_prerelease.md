# R01 — Build and publish a pre-release candidate

Outcome: an immutable candidate is published and anonymously retrievable from all required channels, ready for real GPU qualification.
Requirements: REQ-015, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Read linked requirements through workflow MCP; do not read manifest/decisions or review archives through shell. Recheck guidance and .codegraph before code lookup and pinned specification freshness before work and completion. Every packet requires its own explicit invocation; planned work is not implementation authority.

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
