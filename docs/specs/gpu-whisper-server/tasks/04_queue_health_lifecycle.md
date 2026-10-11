# P04 — FIFO, health, deadlines and shutdown

Outcome: one bounded request lifecycle owning the public API's work and resources.
Requirements: REQ-003, REQ-007, REQ-008, REQ-011, REQ-013, REQ-014, REQ-017, REQ-018, REQ-019, REQ-029, REQ-031.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Require explicit invocation, usable Git worktree and passing dependencies. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Recheck guidance/.codegraph, exact packet requirements through MCP and pinned source before work/completion. Run from project root with rtk. Tests/checks below are to create; no implementation tests exist yet. Write/run discriminating RED before behavior, implement to GREEN, PROVE absence/wiring or assertions not reached by RED with a reversible mutation, then restore.

Prerequisites: P02/P03 checks pass. Read core/contracts.yaml configuration.settings queue/deadlines and private_backend; contracts/openapi.yaml paths/errors/health schemas; quality/scenarios.yaml Q-FIFO/Q-CANCEL/Q-HEALTH/Q-FAILURE/Q-PRIVACY. Source sections: spec.md:138, :160, :172, :204, :214, :224. Proposed code ownership appears in plan.md; no existing scheduler is assumed.

## Scope and ownership

Allowed: cmd/api/**, internal/application/transcription/**; narrow HTTP/upstream/backend integration and lifecycle tests. No distributed queue, auth, native changes, new public operations or alternate memory modes.

Admit owns a FIFO of fully validated owned requests, one active operation and 16 waiting by default. Separate bounded staging reservations never make invalid inputs admitted operations. Zero waiting capacity still accepts one idle operation; overflow 429 declares no execution. Deadline starts at valid admission and spans waiting/execution; staging has a separately bounded upload/decode duration. Waiting timeout/cancel removes work without forwarding (408 if response deliverable). Keep immutable data/result channels in the queue, never pooled Echo Context/ResponseWriter.

Before forwarding, match fresh private protocol/model/configuration/generation and send remaining deadline. At first API startup, unclaimed busy backend work prevents public readiness until resolved; during its own healthy active work readiness/probes remain responsive. Missing/failed backend returns bounded 503 and never false success. Do not retry submitted inference.

Active expiry/client cancellation coordinates P02 stop/reap/reload; closed transport alone cannot release safe reuse. Keep ownership until readers/writers finish, release files/slots on every terminal path, and prevent subsequent work during unsafe recovery. Unobserved disconnect cannot exceed the deadline. Queue/backend loss can drop the response; exactly-once delivery is not promised.

Liveness is independent of dependencies/queue, ready requires paired actual loaded CUDA model and no unsafe recovery/shutdown. Probe reads use a bounded independent health timeout, no model-load/inference calls. SIGTERM immediately becomes unready, rejects new requests and cancels waiting work with 503 when possible; active work may complete in 30 s default. At expiry stop/clean within 10 s, then exit. Forced loss clears only disposable work on restart and never touches cache.

Preserve F01 validated request ID in the application request context through queue wait, dispatch, timeout/cancellation and shutdown; pass it through the consumer-owned InferenceBackend port to the HTTP adapter. Use injected safe slog logging; never import Echo, Koanf, validator, platform config/logging helpers or concrete adapters into domain/application. cmd/api maps config into typed options and wires dependencies. Read Q-OBSERVABILITY/Q-GOFOUNDATION and core go_foundation/observability. Queue checks exercise actual correlation and import-boundary checks after wiring.

Return F01 typed semantic failures for queue capacity, operation deadline, readiness/backend loss and shutdown; no status code, response header, public JSON envelope or Echo error is created in Admit/Shutdown. Read go_foundation.error_contract and Q-ERRORS. Preserve errors.Is/As classification across queue/result channels and wrapper calls. The owning HTTP adapter adds the existing 429/408/503 response when delivery is possible; observed disconnect and committed responses do not trigger another write. Backend/private status parsing belongs to the upstream adapter, not this application package.

## Acceptance/test-first

| Criterion | Test to create | RED / PROVE observation |
| --- | --- | --- |
| FIFO, one active, 16 waiting/zero-capacity/overflow | TestFIFOAdmission/TestQueueCapacity | Controlled backend barriers measure entry/exit and max active; mutate active guard or order |
| Waiting timeout/cancel never dispatches | TestWaitingExpiry | Dispatch counter stays zero for expired ticket; mutate waiting removal |
| Total admitted deadline includes queue wait | TestOperationDeadline | Fake monotonic clock/barriers make residual execution budget expire, not reset |
| Active cancellation and API loss cannot overlap work | TestActiveRecovery/TestAPILoss | Actual helper child reaped before next entry, unready during reload; mutate safe reuse |
| Health stays responsive and dependency truthful | TestHealthDuringBlockedInference/TestBackendReadiness | Live/ready return without queue lock; startup/unclaimed busy/dead/GPU/recovery states are unready |
| Shutdown/resource ownership | TestGracefulShutdown/TestRequestCleanup | Reject/new/waiting/active outcomes, no freed-in-use files, no abandoned resources; mutate cleanup/drain |
| Service failure contract independent of HTTP | TestServiceFailureClassification/TestFailureThroughQueue | Domain/application-only tests recognize direct/wrapped queue/deadline/unavailable/shutdown failures without transport metadata; HTTP integration verifies existing mapping and no response after disconnect |

Commands: `rtk proxy go test ./internal/application/transcription ./internal/adapter/httpapi ./internal/adapter/upstream ./internal/adapter/backend -count=1`; repeat those packages with `-race`. Use safe actual dispatch/resource/process counts, not mutable state snapshots. Finish `rtk proxy go test ./...` and `rtk proxy go vet ./...`. Real CUDA stability and SIGTERM are still P07/P08 gates.
## Completion

Run focused checks, appropriate full Go tests/vet and final pinned specification freshness check. Record exact results and mutations; required unavailable/failed checks keep this packet incomplete. Update this packet, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop. No automatic following packet, review, commit or publication.

