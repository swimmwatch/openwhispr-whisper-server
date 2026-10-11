# P02 — Private native-process supervisor

Outcome: safe ownership of one unchanged native server/model, private inference and recovery.
Requirements: REQ-006, REQ-007, REQ-008, REQ-011, REQ-013, REQ-014, REQ-017, REQ-018, REQ-019, REQ-029, REQ-031.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit authorization for this packet and a usable Git worktree are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Re-verify repository guidance/.codegraph before code lookup, obtain exact linked requirements through MCP, and check pinned source freshness before work and completion. Do not read workflow YAML directly.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: P01 checks pass. Read core/contracts.yaml runtime.backend/private_backend/configuration.transient_storage and quality/scenarios.yaml Q-STARTUP/Q-CANCEL/Q-FAILURE/Q-PRIVACY/Q-OBSERVABILITY; core observability/go_foundation. Source requirement sections: spec.md:96, :104, :138, :160, :172, :204, :214, :224.
Re-verify pinned upstream blob before execution. Native server.cpp:717–726 loads model; :983–988 observes closed connection via abort callback; :1206–1215 health has no inference lock. src/whisper.cpp:1290–1358 permits CPU fallback; :1854–1857 logs model-buffer allocation. These facts do not prove runtime timing.

## Ownership and scope

Allowed: cmd/backend/**, internal/adapter/backend/**, internal/adapter/models/** and internal/platform/config/** narrow integration and tests/helpers scoped to the native-process boundary. No C++ source patches, images, public admin routes or alternate memory modes.

Supervisor.Run validates/provisions once, starts native Whisper on 127.0.0.1 with fixed model/GPU/no-context defaults, owns process group and Wait/reaping. It listens privately on backend.port and exports only POST /inference and GET /health. All other routes, including /load and native file serving, reject. Native conversion is disabled; only normalized WAV is forwarded.

Use F01 safe JSON startup/lifecycle logging. Accept the validated private X-Request-ID and bind it to owned request/recovery events, without requiring native C++ correlation changes. cmd/backend assembles adapters from validated options.

Reuse F01 Failure/FailureKind for subprocess/device/provisioning failures returned across internal boundaries; preserve causes for errors.Is/As without leaking native output. Only private HTTP handlers add their existing transport response metadata. Read go_foundation.error_contract and Q-ERRORS; no Echo/net/http error value or private response body escapes an application port.

Track starting/ready/busy/recovering/stopping/failed, a random per-child generation and shared config fingerprint. Busy healthy inference remains ready; recovering/shutdown is unready. Private health includes the core-listed safe fields and model capability, never paths/URLs/content.

Readiness requires native load, positive exact whitelisted CUDA backend/model-buffer startup evidence and compatible live GPU. A GPU list alone and native CPU fallback cannot satisfy it. Periodic bounded GPU/dependency health checks invalidate a failed device/context; public probes do not trigger inference. Read native stdout/stderr through bounded drains; discard every non-whitelisted line and never relay raw filenames/transcripts/errors.

Private inference checks protocol/configuration/generation, remaining positive capped deadline, normalized input/field allowlist and one active operation. It has no waiting queue. Success retains the same child. Active cancellation/expiry or uncertainty marks unready, terminates/kills/reaps the old process group within cleanup (10 s default), then reloads under startup bounds. Never infer against an uncertain still-active child; never retry submitted work. API death cannot bypass the supervisor's own deadline.

## Acceptance and checks

Create subprocess/HTTP tests with a helper native child that emits controlled startup facts, blocks inference, refuses graceful termination, crashes and tries to emit privacy markers.

| Criterion | Test to create | Observation / PROVE |
| --- | --- | --- |
| One child/model, no success restart | TestResidentSupervisor | Child identity/start count stable across successes; mutate restart-after-success |
| CUDA readiness, health independent of inference | TestStartupReadiness/TestHealthDuringInference | CPU-only/no-allocation startup unready, probe completes while inference blocked; mutate GPU guard |
| Cancellation/timeout/death safely reaps old child | TestCancellationRecovery/TestSupervisorDeadline | Old PID/group gone before replacement entry; mutate reap/wait ordering or deadline |
| Private route/compatibility validation | TestPrivateRoutes/TestGenerationMismatch | Wrong headers/generation and /load never reach child; mutate route/header guard |
| Native output bounded/redacted and scratch clears | TestNativeOutputPrivacy/TestAbandonedScratch | Marker trap sees no emitted/persisted content; mutate raw log passthrough |
| Native/dependency failures stay protocol-neutral internally | TestBackendFailureClassification | Typed and wrapped process/GPU/provisioning causes remain recognizable; private HTTP serialization stays at the adapter and cannot leak raw causes |

Commands: `rtk proxy go test ./internal/adapter/backend -count=1`; `rtk proxy go test -race ./internal/adapter/backend -count=1`. Assert actual helper process lifetime, not just a readiness flag/closed socket. A fast helper abort does not establish CUDA cancellation; P07/P08 must reproduce it on real GPUs.

SIGTERM becomes unready/rejects new work, allows active work for shutdown (30 s default), then safely stops/reaps with cleanup budget; meaningful bounded exit status. Startup/crash recovery preserves cache and clears disposable own scratch. No separate external gate for these disposable unit subprocesses.
## Completion

Recheck the pinned specification version and run the focused checks plus `rtk proxy go test ./...` and `rtk proxy go vet ./...` where Go behavior is in scope. Keep required failures/unavailable environments incomplete; never label planned work passed. Update this packet's evidence, the single [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop after this packet. No automatic next packet, review, commit or publication.

