# I02 — Public repository and open-source foundation

Outcome: public GitHub identity, community/license materials and verified distribution namespace access.
Requirements: REQ-024, REQ-027.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: completed on 2026-10-11. Local/community and scoped GitHub settings checks passed; GHCR authentication, selected namespace write access and absent package collisions verified; both empty Docker Hub repositories are public with authenticated read/write/admin and anonymous metadata readback. GHCR package visibility and actual anonymous artifact pulls remain first-publication work in C02/R01. See [current handoff](handoff.md) for evidence and limitations. Every later packet requires its own explicit invocation.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: I01 complete and usable Git. Read spec.md REQ-024/027; core/contracts.yaml delivery.repository/images.namespaces/chart; quality/scenarios.yaml Q-INIT/Q-DISTRIBUTION. Planning observed the GitHub login swimmwatch; repository availability and Docker Hub ownership remain unverified.

## Scope and boundaries

Allowed: LICENSE, CONTRIBUTING.md, CODE_OF_CONDUCT.md, SECURITY.md, SUPPORT.md, CHANGELOG.md, THIRD_PARTY_NOTICES.md, .github issue/PR templates, CODEOWNERS, dependency-update configuration and one-off .local/check_repository.py with disposable local fixtures; scoped creation/settings of swimmwatch/openwhispr-whisper-server and the named package channels only during explicitly authorized execution. Root/chart README are deferred to P09. No unsolicited announcements/messages, unrelated repository/account changes, implementation, token creation in files, stable release or cluster deployment.

U-REPOSITORY takes authenticated owner/access facts plus selected names and produces a public repository, community paths and scoped settings. Read back every write through GitHub/registry APIs. Inspect any existing name collision before changes; never replace an unrelated repository/package. Configure origin only after confirming the exact repository. A namespace without access is a failure; do not choose another namespace silently.

Own code/chart license is Apache-2.0; include its exact legal text and actual copyright/notice decisions. THIRD_PARTY_NOTICES.md inventories redistributed upstream/backend/decoder/base dependencies and their actual licenses; leave uninspected components explicitly pending until P05/C02, never mark all dependencies Apache-2.0. CONTRIBUTING lists real bootstrap/check/contribution steps and hardware prerequisites. Define support versus vulnerability reporting, enable GitHub private vulnerability reporting, and use a verified private conduct contact. Do not invent email addresses or promise an unstaffed response deadline. Issue templates discourage publishing credentials/audio/private transcripts.

Protect main against deletion/force pushes and require pull requests/checks where available for this repository/account. C01/C02 supply **actual** stable required-check names after successful runs; do not require names that do not exist. A single maintainer must have a usable workflow: do not require their own approving review or bypass failing checks to compensate. Pin dependency-update configuration to actual ecosystems (Go, GitHub Actions, Docker/tooling) as they appear; no invalid empty ecosystem directories.

Verify public GHCR package access, Docker Hub swimmwatch namespace/repositories and GitHub Pages eligibility/path. Set public package visibility independently from repository visibility. Do not publish images/chart to prove access in I02; R01 performs anonymous artifact pulls.

## Checks and recovery

Prepare a one-off .local/check_repository.py check before files/settings edits; it validates scoped file links, legal identity, placeholders and settings read-back fixtures. RED: deliberately incorrect owner/private visibility/missing reporting control in a disposable fixture. GREEN: local documents plus actual remote identity/settings checks. PROVE: remove a required control in a fixture and detect it.

Planned command: `rtk proxy python3 .local/check_repository.py --local`. Remote read-only verification: `rtk proxy gh repo view swimmwatch/openwhispr-whisper-server --json nameWithOwner,visibility,url,defaultBranchRef`; inspect exact rules/reporting/Pages and registry access through their APIs without printing credentials. Capture only safe settings evidence; “repository exists” is insufficient for the privacy/protection requirements.

**MANUAL GATE:** separate explicit invocation authorizing the scoped repository/settings writes; authenticated target ownership must be established before writing. Supply/verify the human private conduct route when it cannot be discovered. External settings changes require snapshot/read-back; partial setup stays incomplete with exact remaining controls/owner. Preserve existing protections and never overwrite a collision.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.
