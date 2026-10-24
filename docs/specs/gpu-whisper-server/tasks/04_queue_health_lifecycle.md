# P04 — FIFO, health, deadlines and shutdown

Outcome: one bounded request lifecycle owning the public API's work and resources.
Requirements: REQ-003, REQ-007, REQ-008, REQ-011, REQ-013, REQ-014, REQ-017, REQ-018, REQ-019, REQ-029, REQ-031.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-22) for local P04 service lifecycle and API assembly acceptance. All required local checks and sixteen discriminating PROVE mutations passed. The pinned specification and exact technical agreement remain fresh. Release FFmpeg packaging, containers/Helm and real GPU acceptance remain in their owning packets.

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

## Technical proposal

Implement internal/application/transcription Service using the P03 Normalizer and InferenceBackend ports, and cmd/api as the only assembly owner. Binding operations are NewService, Admit, Readiness and Shutdown; constructors receive typed options, ports and injected slog logger, not configuration loader or concrete adapters. Admit consumes the P03 owned Input, normalizes it through the port, then admits its Normalized resource to the FIFO and blocks until terminal result/cleanup. No Echo objects, config imports, service locator, DI framework, database or generic job system enter application/domain.

Bind ../core/contracts.yaml configuration/settings/transient_storage, private_backend, observability, rate_limit and go_foundation/error_contract; ../contracts/openapi.yaml public operation/health/error schemas; Q-FIFO/Q-CANCEL/Q-HEALTH/Q-FAILURE/Q-RESIDENT/Q-PRIVACY/Q-OBSERVABILITY/Q-GOFOUNDATION/Q-ERRORS. P03 owns bounded HTTP/raw-upload staging; P04 owns normalization completion, FIFO work and normalized resource lifetime. The service returns only typed semantic failures and plain result/health data. Pass the validated request ID as copied request data/context across ports; app logs only the fixed safe schema using slog, never import platform logging helpers.

Use one owned worker and a mutex-protected FIFO with explicit per-item cancellation/deadline/result ownership. Exactly one active operation; default 16 waiting; zero waiting permits an idle operation. The operation deadline starts successful normalized admission and includes queue wait and native execution; staging/decoding has its separate bounded context. Queue-full or waiting-cancelled/expired work never dispatches. Do not reset the deadline at dispatch, queue pooled request objects or retry uncertain inference. All workers and result channels have a bounded shutdown owner; private queue helpers remain flexible.

Maintain an independent bounded paired-health observer so live/ready never wait on the FIFO or trigger model reads/inference. Validate protocol/fingerprint/model/generation before every dispatch and send only positive remaining duration. At API startup, unclaimed backend busy work keeps readiness false until resolved; its own healthy active operation does not make it unready. Dependency loss, mismatch, recovery and shutdown fail closed.

Active cancellation/expiry marks unavailable and coordinates the P02 stop/reap/reload protocol. Keep request files/streams/slots owned until readers/writers stop and safe backend recovery is observed; closing HTTP alone is insufficient. The next operation cannot overlap old work. Release each Normalized reader exactly once on success, validation failure, queue rejection, waiting cancellation, active recovery and shutdown. Raw upload remains handler-owned until this call returns. If cleanup cannot be established within the existing cleanup budget, remain unready and take the bounded process exit/restart path.

cmd/api loads strict settings, constructs the existing logger/rate store/middleware, the actual P03 adapters and Service, then starts HTTP and handles signals. Shutdown first marks unready and rejects new/waiting work, allows active completion within shutdownSeconds, then cancels and cleans within cleanupSeconds. No new endpoint, distributed queue, global limiter, metrics or alternate protocol.

Binding tests are the existing packet's controlled FIFO/deadline, actual helper-process recovery, health and shutdown suites, plus cmd/api constructor/wiring/import and normalized-reader ownership checks. Test-first failures and PROVE removal of a waiting item, deadline reset, premature safe reuse and resource leaks. Full Go/race/vet/import/contract checks remain required; real GPU shutdown/retention is P07/P08.

## Technical agreement

Selected proposal: `p04-fifo-lifecycle-v1`. Packet: `P04`.

- proposal_digest: `49e2f67b9d5747784403fa2156ebc3c734571818616101024c6b17061bbd32ef`
- agreement_ref: `{id: 94e10aa1-7886-444d-9a00-430d59b6c6c1, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Write service FIFO/capacity/residual-deadline/cancellation RED checks with consumed-port fakes and controlled clocks.
2. Implement one worker, waiting FIFO, normalized resource ownership and protocol-neutral result/failure paths.
3. Implement independent health/recovery/shutdown and actual helper-process safe-reuse checks.
4. Assemble cmd/api with real P03 adapters and F01 setup; verify constructor/route/request-ID/error wiring.
5. Run focused/race/full Go/vet/import/contracts and required PROVE, record actual evidence and stop.

## Implementation evidence — 2026-10-22

Implemented the selected proposal without changing its shared commitments:
- Service NewService/Admit/Readiness/Shutdown consumes the unchanged P03 ports.
  One worker owns the FIFO and active inference; a separate bounded observer
  owns paired health. Zero capacity permits one idle operation.
- Normalization is separately bounded staging. The admitted timeout starts
  after normalization and spans waiting/execution without a dispatch reset.
  Waiting cancellation removes both capacity and retained references.
- Every normalized reader closes exactly once after its consumers stop.
  Active cancellation/uncertain failure remains unready until a fresh different
  backend generation proves P02 retired the previous process. Subsequent load
  may exceed cleanup time without dispatching waiting work.
- Terminal cleanup failure is signalled through internal Done/Err lifecycle
  helpers to cmd/api. It exits with an error without another graceful drain or
  freeing a reader still in use. Existing application ports and public
  operations are unchanged.
- cmd/api wires strict configuration, safe slog, selected Echo rate store and
  middleware, actual FFmpeg/upstream adapters and Service. It does not provision
  or read model cache. SIGTERM closes admission, cancels waiting work, permits
  active completion within the grace budget and then cancels/cleans.
- Request IDs/context survive queue/dispatch/private HTTP. Readiness, recovery,
  startup/shutdown and queue diagnostics contain safe fields only.

RED: the initial callable service stub failed readiness assertions in the
FIFO/waiting/deadline suites; these were not treated as proof of their deeper
assertions. The callable API stub failed startup/logging assertions.
TestShutdownDuringNormalization then exposed internal-error classification in
place of shutting_down. TestAPIAssembly exposed missing readiness diagnostics.
TestAPIFatalReaderExit exposed an extra graceful drain after terminal cleanup.
Each was corrected and its intended assertion passed. Deeper queue, ownership,
absence and wiring claims were exercised by the mutations below.

| Mutation | Discriminating check |
| --- | --- |
| Replace FIFO with LIFO | TestFIFOAdmission/2 |
| Remove capacity guard | TestFIFOAdmission/0 |
| Keep an expired waiting entry | TestWaitingExpiry |
| Reset execution deadline at dispatch | TestOperationDeadline |
| Accept the old generation after cancellation | TestActiveRecoveryThroughPrivateHTTP |
| Omit normalized resource Close | TestFIFOAdmission/0 |
| Close a borrowed reader during cancellation | TestActiveRecovery |
| Replace the owned parent context | TestFIFOAdmission/0 |
| Disable the assembled rate limiter | TestAPIAssembly |
| Stop observing SIGTERM | TestAPISIGTERM |
| Ignore terminal service notification | TestAPITerminalExit |
| Change actual decoder output rate | TestAPIAssembly |
| Log the private prompt | TestAPIAssembly |
| Reject own healthy busy backend | TestHealthDuringBlockedInference |
| Bypass configuration compatibility | TestBackendReadiness/configuration |
| Force cancellation instead of graceful drain | TestGracefulShutdown/complete |

All sixteen compiler overlays failed intended assertions. Production source
hashes stayed unchanged. Final evidence is in ignored
`.local/p04/prove-final.json`; individual logs remain under `.local/p04/`.

Preserved failed evidence is not counted as a pass: the first runner could not
find child RTK on the isolated Go PATH; it was rerun using its verified absolute
path. The first waiting-removal mutation left an unused Go index and did not
compile; a separate `prove-waiting-2` variant compiled and failed the capacity
assertion. Initial final-race evidence found unsynchronized test-server
fingerprint access; atomic.Value corrected the fixture without changing
assertions. Full test/race/vet checks passed again after that correction.

Final GREEN commands from repository root, with Go 1.27.3 first on PATH and
GOTOOLCHAIN=local/GOWORK=off:
- `rtk proxy go test ./... -count=1`
- `rtk proxy go test -race ./cmd/api ./internal/application/transcription ./internal/adapter/httpapi ./internal/adapter/upstream ./internal/adapter/backend -count=1`
- `rtk proxy go vet ./...`
- `rtk proxy go mod verify`
- `rtk proxy go mod tidy -diff` (empty)
- `rtk proxy gofmt -l cmd internal` (empty)
- `rtk proxy .local/c01/venv/bin/python tools/check_go_architecture.py`
- `rtk proxy .local/c01/venv/bin/python tools/check_contracts.py`
- `rtk proxy .local/f01/bin/govulncheck ./...` (exit 0; no reachable/imported
  package vulnerabilities; one required-module advisory outside imported/called
  packages remains).

Synthetic WAV input at 8 kHz passed through real FFmpeg to canonical 16 kHz
output in the assembled API. Public live/ready probes remained responsive during
inference; denied work returned 429 before validation/inference. Real API helper
SIGTERM exited successfully. Separate actual helper-process tests exercised
private HTTP cancellation and abrupt API-process loss, observed old native PID
reaping before the next operation, and verified a usable replacement generation.

The development FFmpeg executable is 6.1.1-3ubuntu5. Release n9.0.2 remains P05.
Helper GPU/model evidence is synthetic: no actual CUDA/model allocation,
container, Helm, Home Lab, performance or GPU-release acceptance is claimed.
No dependency, configuration default, native source or public contract changed.
No deployment, model download, publication, commit, push or new review occurred.
Only P04 is newly complete; stop before P05.

## Forced shutdown correction (2026-10-22)

The review found that forced cancellation became `ShuttingDown`, bypassing
the recovery check. The service now waits for confirmed retirement of the old
native generation before completing forced shutdown. If retirement cannot be
confirmed within the cleanup budget, shutdown reports a terminal
`BackendUnavailable` failure rather than success.

Both new regression tests failed before the correction and passed afterward:

- `TestForcedShutdownThroughPrivateHTTP` uses the real private HTTP adapter and
  an actual helper process that ignores SIGTERM; successful shutdown requires
  the old PID to be reaped and normalized audio to be closed exactly once.
- `TestForcedShutdownRecoveryTimeout` keeps the old generation and verifies
  bounded failure, the preserved timeout cause and terminal command notification.

Final verification passed with Go 1.27.3, `GOTOOLCHAIN=local`, `GOWORK=off`:
`go test ./... -count=1`, the five-package P04 race suite, `go vet ./...`,
`tools/check_go_architecture.py`, `tools/check_contracts.py` and formatting.
RED/GREEN and final logs remain in ignored `.local/p04-review/fix-*.log`.
No real CUDA, container or Helm acceptance was performed for this correction.
