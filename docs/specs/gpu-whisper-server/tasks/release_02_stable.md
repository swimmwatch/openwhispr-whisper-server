# R02 — Qualified stable release

Outcome: stable artifacts and all public channels are verified from an accepted candidate, with preserved executable image identities.
Requirements: REQ-020, REQ-024, REQ-026, REQ-027, REQ-028.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Read linked requirements through workflow MCP; do not read manifest/decisions or review archives through shell. Recheck guidance and .codegraph before code lookup and pinned specification freshness before work and completion. Every packet requires its own explicit invocation; planned work is not implementation authority.

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
