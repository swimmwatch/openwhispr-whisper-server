# R02 — Qualified stable release

Outcome: stable artifacts and all public channels are verified from an accepted candidate, with preserved executable image identities.
Requirements: REQ-020, REQ-024, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: planned; technically agreed (2026-10-22), not executed. Exact agreement below checked ready against the pinned Approved/reviewed specification. A separate incremental invocation, passing dependencies and any scoped external authority remain required. Preserve completed work; recheck source and agreement freshness before execution and completion.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: R01 published candidate, P07/P08 complete for the selected exact candidate, P09 final documentation complete, and current full CI. Read spec.md REQ-020/024/026–028; core/contracts.yaml delivery.versioning/pipeline/release and quality/scenarios.yaml Q-INIT/Q-DISTRIBUTION/Q-RELEASEGATE. Reopen acceptance dependencies if inputs have drifted.

## Gate and promotion boundary

Allowed: stable version/tag/release inventory/assets, image semantic aliases, stable chart package/index and safe evidence through C02 automation. No runtime change, rebuild substituted for accepted images, invented GPU report, automatic deployment or unrelated account change.

U-RELEASE joins candidate source/build identity, final release source, current CI, both target reports and docs. Verify that required jobs passed rather than merely a top-level workflow being green. Each GPU report names the actual hardware, model/upstream/tool/runtime configuration and pulled image/chart identities; both targets are mandatory. If a runtime/build/rendered input changed, deny promotion, create a new R01 candidate and renew affected acceptance. A documentation-only source difference may reuse runtime evidence only after a verified build-input comparison; record both source identities.

Use v0.1.0 as the initial stable example. Retag/promote the **accepted immutable image manifests** without rebuilding or relabeling their original source/provenance. Current CI may perform verification builds; those outputs do not replace accepted manifests. This keeps the real hardware evidence applicable.

Stable chart version/appVersion and final chart README produce a new tgz identity. Compare to the accepted RC: only release/documentation metadata may differ without renewed runtime acceptance. Revalidate values/schema/rendered configuration, image digests, generic Kubernetes/Helm matrix and anonymous install/download behavior for both chart channels. Record new stable archive SHA256 and chart OCI manifest digest separately. A changed rendered resource/runtime parameter requires affected real target checks before publication. “Same templates” alone is insufficient if values or image aliases changed.

Before stable publication, refresh Security Check on the accepted image digests, final source and newly packaged stable chart/values matrix under core/contracts.yaml delivery.security_checks and Q-SECURITY. Preserve image digests; rescanning does not require a rebuild. Record current security_report_refs and block on a new applicable finding, stale/missing scan, expired suppression or identity mismatch. A DB/rule-only change renews security evidence; an executable/dependency fix returns to a new RC and affected GPU acceptance. Do not carry the old RC security badge forward without the current checks.

## Checks and channel order

Run C02's stable gate before any stable write. RED: absent target report, source/digest mismatch, required skipped job, unfinished README or channel failure fixture denies stable. GREEN: exact accepted reports/current CI/docs plus package/rendering checks. PROVE any new promotion predicate with a reversible fixture bypass and restore.

Planned command: `rtk proxy python3 tools/release/check.py --inventory <safe-inventory.json> --stage stable`; reuse actual C02 distribution/read-back harness and P06 package/render validation. Record the final stable inventory and safe report references.

Publish immutable versioned images/chart first and verify anonymous pulls, identical manifests across image registries, identical tgz across chart channels and retained older index entries. Finalize stable semantic aliases/latest and GitHub latest release designation **last**, then read them back. A GitHub release existence/success flag alone is not completion.

**MANUAL GATE:** explicit R02 invocation authorizing stable promotion of the selected candidate/version. User-approved publication does not authorize Home Lab deployment; deployment remains manual.

A partial publication keeps stable incomplete with exact per-channel state. Preserve prior stable versions/cache; restore prior aliases if a partial finalization advanced them incorrectly, using the recorded previous inventory. Retry identical selected bytes; never move tags or overwrite changed immutable versions. Fixes return to their owning packet and a new RC rather than bypassing the gate.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.

## Technical proposal

Promote an explicitly selected qualified candidate using C02's stable gate and inventory, not an image rebuild. Bind ../core/contracts.yaml delivery/release/versioning/images/chart/security_checks and Q-DISTRIBUTION/Q-RELEASEGATE/Q-SECURITY. Require successful current CI/security and both real P07/P08 target reports for the selected executable/runtime/build inputs plus completed P09 documentation. Failed/missing/skipped reports, source/runtime drift or incomplete channels deny promotion.

Preserve the accepted immutable API/backend image manifests and original source/provenance metadata across stable image tags and both registries. Verification builds may run but cannot replace accepted manifests. Record both candidate and release source identities; documentation-only differences may reuse runtime evidence only after comparing actual build inputs. Any executable/dependency/runtime/rendered-resource change goes through a new R01 candidate and affected GPU acceptance, not a stable-gate exception.

Package the stable chart version/appVersion/final README as a new archive with its own SHA256 and OCI manifest digest. Diff rendered resources/config/image digests and generic Helm/Kubernetes matrix against the accepted candidate. Only proven release/documentation metadata changes can avoid renewed runtime acceptance. Rescan accepted image digests plus final source and newly packaged chart/values with current DB/rules; preserve executable identities, and fail on applicable findings or expired/incorrect evidence.

Publish immutable versioned stable artifacts and merge the classic index first, anonymously read back every image/chart channel, then finalize stable aliases/latest and GitHub release designation. Serialize writes and retain exact per-channel state. A partial finalization is incomplete, preserves previous immutable versions/cache, and can restore prior aliases from recorded inventory; identical-byte retries are idempotent. Never force-move immutable versions or erase prior index entries.

Before writes, run denial fixtures for absent target, mismatched identities, skipped jobs, stale scans, unfinished docs, changed rendering and channel failure. PROVE any unexercised promotion predicate on fixtures, restore and require real gate plus all read-backs. This packet creates only stable inputs/inventory/evidence and authorized versioned release operations, not new service behavior.

Manual gate: explicit later R02 invocation and authority for the selected candidate/version and stable publication. It does not authorize Home Lab deployment, unrelated settings changes, automatic commits/pushes or moving release tags. No stable-complete claim until all required channel/alias/security/documentation checks pass.

## Technical agreement

Selected proposal: `r02-qualified-promotion-v1`. Packet: `R02`.

- proposal_digest: `bbb43fdeeefe76a806b62c75d8e9beca1cc67596a017d883130969e3f856edec`
- agreement_ref: `{id: 19d3f11e-13ba-4c7a-b727-511a0c24a7d2, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Select the separately authorized candidate/version; verify both GPU reports, final docs and unchanged runtime/build inputs.
2. Refresh source/image/chart security and stable-chart metadata/render/schema checks before any stable write.
3. Run stable denial/rollback fixture checks and the exact real gate.
4. Publish immutable versioned stable artifacts, anonymously verify every channel, then finalize aliases/latest/GitHub designation.
5. Read back final state; preserve prior immutable versions/aliases on partial failure and stop only with honest complete/incomplete evidence.
