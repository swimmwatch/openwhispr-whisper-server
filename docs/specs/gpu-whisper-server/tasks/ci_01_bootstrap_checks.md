# C01 — Early contributor CI

Outcome: fork-safe CI verifies actual bootstrap and contracts before service implementation starts.
Requirements: REQ-023, REQ-024, REQ-026.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: complete on 2026-10-11. Hosted main and disposable pull_request runs passed, the deliberate failure produced a failed job and BLOCKED PR, and main required-check settings were read back. The user authorized initial source commit/push and test PR with `ok`. See handoff.md for exact run/source evidence. The disposable PR uses the same repository; no independent fork run or release predicate is claimed. Read linked requirements through workflow MCP; recheck pinned specification freshness. Every later packet requires its own explicit invocation.

All commands below run from the project root and use rtk. C01's workflow, validator, tooling tests and source-secret checker now exist; install only the hash-locked permanent check dependencies and verified tool binaries to use them. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: I01/I02 complete, usable Git, remote repository/settings and scoped CI execution authorization. Read spec.md REQ-024/026, core/contracts.yaml delivery.pipeline.bootstrap_checks/untrusted_pr/pins and quality/scenarios.yaml Q-INIT/Q-CI/Q-CONTRACTS.

## Scope and boundary

Allowed: .github/workflows/ci.yml, .github action/update settings, tools/check_contracts.py and tests/tooling/**; pin only dependencies needed by permanent CI checks. One-off bootstrap/repository probes remain in ignored .local/ and are not CI prerequisites. No service code, publishing workflow, registry/cluster secrets, privileged/self-hosted runners, fake test packages or misleading complete-release badge.

U-CI takes ordinary PR/main source and produces stable named checks with real exits. A hosted Linux runner uses contents read only and pinned action SHAs, installs the recorded tools, and runs actual module/tool/source-exclusion/contract checks. Full service build/race/Docker/Helm gates belong to C02 after their inputs exist. An empty Go module gets explicit module verification, not a false test pass.

Create a contract validator using a real YAML parser rejecting duplicate keys and unsafe aliases/tags; validate core/scenario references and OpenAPI against the pinned applicable schema. Exclude MCP-owned manifest/decisions/review archives from local parsing; workflow scenarios own their validation. Validate safe settings/document links already present. Do not skip a mandatory input/check just because a file is absent.

Trigger untrusted source via pull_request, with no secrets, registry push rights, cluster credentials or privileged runner. Do not execute untrusted PR code in pull_request_target. Cache writes cannot seed future trusted release builds. Pin actual action revisions and use job-specific rights; no blanket write-all token. Expose check names only after real successful runs, then set main required checks through scoped settings read-back. Keep GitHub repository licensing/settings constraints visible instead of silently disabling protection.

Add Trivy current-source secret scanning and scanner/report-exit fixture checks at this bootstrap stage, with pinned verified tooling and no maintainer secrets. Read core/contracts.yaml delivery.security_checks and quality/scenarios.yaml Q-SECURITY. Allow tools/security/** and its narrow configuration/fixture tests for this purpose. Exclude MCP-owned workflow state/archives, Git internals and private local state from filesystem reads. Use fake non-credential markers and redact matches/snippets before artifacts/logs. F01 wires real Go formatting/vet/module/test/race/import checks after foundation packages exist; C02 adds the full Go security, Dockerfile, image and Helm scanners after actual inputs exist; an empty module never receives a full Security Check pass.

## Test-first verification

Before workflow creation, define executable permission/trigger/pin assertions and YAML/contract fixture tests. RED: a seeded writable PR job/unpinned action/duplicate YAML key/unknown scenario requirement is rejected. GREEN: local validator tests and real hosted CI for trusted and fork-safe untrusted source. A permission check must inspect job-level overrides/effective rights, not merely a top-level field. PROVE: inject a failed required check in a disposable branch and show it cannot report success or allow the release predicate.

Planned commands: `rtk proxy python3 tools/check_contracts.py`, `rtk proxy python3 -m unittest discover -s tests/tooling -v`, `rtk proxy actionlint .github/workflows/ci.yml`, `rtk proxy go mod verify`. The later Go application tests are not claimed here. Record verified actionlint and parser/schema identities in tools/versions.yaml and introduce a project dependency lock only for the permanent contract checks that actually need it. Do not rely on ignored local bootstrap tools.

Record actual CI run URLs/source SHAs/job conclusions and safe permission evidence. Local lint alone does not prove GitHub runs. Use a disposable PR/test namespace or an authorized fork-safe event; no real secret should be injected to demonstrate absence.

## Recovery and gates

**MANUAL GATE:** explicit invocation covers scoped workflow/settings writes and triggering CI; pushing a test branch/committing requires its own explicit authorization if not included. Failing remote checks keep C01 incomplete; correct configuration and rerun affected CI, then read back main required checks. Unavailable hosting/access is not a pass. Roll back only scoped workflow/settings changes, preserving existing protections.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.
