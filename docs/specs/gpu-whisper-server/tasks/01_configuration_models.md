# P01 — Canonical configuration and model artifacts

Outcome: one validated Resident configuration and trustworthy startup artifacts.
Requirements: REQ-003, REQ-004, REQ-005, REQ-006, REQ-008, REQ-017, REQ-021, REQ-023, REQ-031.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit authorization for this packet and a usable Git worktree are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Re-verify repository guidance/.codegraph before code lookup, obtain exact linked requirements through MCP, and check pinned source freshness before work and completion. Do not read workflow YAML directly.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: F01 Go/config/logging/validation foundation complete, plus fresh reviewed inputs. Repository evidence is spec.md:49 (configuration), :80 (artifacts), :88 (cache), :96 (fixed model); these are requirements, not existing implementation.
Read core/contracts.yaml selectors configuration, models, runtime.api; quality/scenarios.yaml Q-CONFIG/Q-CACHE/Q-FUTURE/Q-CONTRACTS.

## Ownership and scope

Allowed: necessary go.mod/go.sum dependency updates; model-specific extensions to internal/platform/config/**; internal/adapter/models/** and their tests. Reuse I01's module `github.com/swimmwatch/openwhispr-whisper-server` and agreed Go baseline; do not initialize a competing local module. No server, Docker/Helm behavior or downloads of full models during unit tests.

Reuse F01 Koanf/validator strict loading and safe logging; extend typed model/catalog semantics instead of creating a competing loader/validator. Read go_foundation and Q-GOFOUNDATION. Logging/rate/proxy settings keep F01 defaults and validation, and component-only settings never enter the model fingerprint. Load/Validate combine CLI > WHISPER_ environment > JSON file > defaults, replace collections, reject unknown/duplicate keys and validate IDs/sources/sets/bounds. Defaults come from CFG entries, including resident/base, queue 16, upload 25 MiB, prompt 8 KiB, decoded 900 s, operation 600 s, cleanup 10 s, shutdown 30 s, provisioning 1800 s and 3 attempts. The active capacity is fixed at one. Deferred modes/policies/warmup and multiple served models reject before serving.

Provision prepares only configured downloads, then verifies the served artifact; a missing served artifact outside the download set fails instead of causing an unrequested download. Existing prepared sources remain read-only. Use per-artifact POSIX locking, bounded HTTPS streaming/retries, checksum/optional size verification, fsync/atomic publication and ownership-safe partial cleanup. A valid cache is checked before network access. Never delete cached model files during request/shutdown cleanup. ReadMetadata extracts pinned GGML capability (including multilingual vocabulary); do not embed inference or guess from ID.

CanonicalFingerprint covers the shared served artifact identity and backend inference configuration, excludes component bind addresses/paired URL, and supplies a safe deterministic compatibility token. Return owned immutable data and typed safe failures. Models depend on config/filesystem/HTTP, not Echo.

## Acceptance and test-first checks

| Criterion | Layer/check to create | Expected discriminating result |
| --- | --- | --- |
| Sources/overrides produce one canonical config | internal/platform/config TestConfigurationSources/InvalidConfiguration | Replaced catalogs contain no bootstrap leftovers; unknown/deferred/invalid values reject |
| Verified cache/prepared file works offline | internal/adapter/models TestOfflineCache/TestPreparedArtifact | Trap server receives zero requests; missing prepared file never downloads |
| Interrupt/checksum/size/retry failures preserve valid cache | TestDownloadFailures/RetryDeadline | No published partial; own partials do not accumulate; bounded failure and corrected retry |
| Concurrent writers never publish partial content | TestConcurrentPublication with separate helper processes | Same final verified SHA, no reader sees partial, no live writer's file removed |
| Header/capability/fingerprint is trustworthy | TestModelMetadata/TestCanonicalFingerprint | Invalid headers reject; English/multilingual metadata is not filename-driven; component addresses do not alter compatibility |

Use tiny generated header/file fixtures and httptest fault servers, not actual model downloads. Commands: `rtk proxy go test ./internal/platform/config ./internal/adapter/models -count=1`; `rtk proxy go test -race ./internal/platform/config ./internal/adapter/models -count=1`. PROVE by bypassing checksum, replacing collection merge semantics, or permitting one forbidden network access; each owning assertion must fail. Check prepared-file bytes/permissions before/after and lock semantics, not success logs.

No data migration; failed provisioning is unready. Correct configuration/restart is recovery; old valid cache remains usable. Source credentials/URLs must not appear in diagnostics.
## Completion

Recheck the pinned specification version and run the focused checks plus `rtk proxy go test ./...` and `rtk proxy go vet ./...` where Go behavior is in scope. Keep required failures/unavailable environments incomplete; never label planned work passed. Update this packet's evidence, the single [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop after this packet. No automatic next packet, review, commit or publication.

