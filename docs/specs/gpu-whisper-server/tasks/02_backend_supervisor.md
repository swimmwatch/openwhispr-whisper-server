# P02 — Private native-process supervisor

Outcome: safe ownership of one unchanged native server/model, private inference and recovery.
Requirements: REQ-006, REQ-007, REQ-008, REQ-011, REQ-013, REQ-014, REQ-017, REQ-018, REQ-019, REQ-029, REQ-031.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-22) for local P02 implementation and acceptance. The command, supervisor and private HTTP checks below passed on Go 1.27.3. Actual CUDA/container/cluster acceptance remains in P07/P08; no such runtime claim or external action is included here. The exact agreement and frozen proposal are unchanged.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: P01 checks pass. Read core/contracts.yaml runtime.backend/private_backend/configuration.transient_storage and quality/scenarios.yaml Q-STARTUP/Q-CANCEL/Q-FAILURE/Q-PRIVACY/Q-OBSERVABILITY; core observability/go_foundation. Source requirement sections: spec.md:96, :104, :138, :160, :172, :204, :214, :224.
Re-verify pinned upstream blob before execution. Native server.cpp:717–726 loads model; :983–988 observes closed connection via abort callback; :1206–1215 health has no inference lock. src/whisper.cpp:1290–1358 permits CPU fallback; :1854–1857 logs model-buffer allocation. These facts do not prove runtime timing.

## Ownership and scope

Allowed: cmd/backend/**, internal/adapter/backend/**, narrow P01 config/model integration, actual plain internal/domain DTOs, narrowly shared HTTP failure serialization in internal/adapter/httpapi/**, whitelisted lifecycle logging in internal/platform/logging/** and tests scoped to this boundary. No public API route or queue implementation. No C++ source patches, images, public admin routes or alternate memory modes.

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

## Technical proposal

Implement the existing Resident contract with one Go supervisor of the unchanged pinned native executable, not a C++ fork or a second scheduler. Binding units are cmd/backend assembly and internal/adapter/backend Supervisor.Run, Health, Infer and Shutdown. Run consumes validated typed options plus the P01 Artifact/fingerprint and an injected slog logger, owns startup, one native process group, bounded stdout/stderr drains and exactly one Wait owner. Health returns an immutable semantic snapshot; Infer consumes a borrowed normalized WAV stream and validated fields and returns domain result/failure; Shutdown drains then stops and reaps. Private helper/type layout is flexible.

Run P01 Load/Validate/Provision/CanonicalFingerprint in command assembly, without a competing config loader. Add only actual shared plain request/result/health DTOs to internal/domain. The private net/http handler remains an adapter and alone serializes status/headers/errors; reuse F01's HTTP failure mapping rather than inventing a second business error vocabulary. Narrow additions to internal/adapter/httpapi and internal/platform/logging are allowed only for shared HTTP error serialization and whitelisted backend lifecycle/request events. Application/domain never import these adapters or config.

Bind runtime.backend and private_backend in ../core/contracts.yaml, including exact pinned source, native loopback listener, private routes, header names, safe health fields, protocol 1 and the P01 configuration fingerprint. Every child gets a crypto-random generation. One active native call uses nonblocking admission; no backend waiting queue. Check protocol/fingerprint/generation and positive capped residual deadline before forwarding. Only normalized WAV and fixed request fields reach native /inference; conversion, /load and file-serving routes remain inaccessible.

Readiness combines native loaded health, exact positive CUDA backend/model-buffer log facts and bounded live GPU/dependency checks; listing alone never passes. Use bounded nvidia-smi subprocess queries without a shell for visible-device health, correlate selected-device evidence, and fail closed on ambiguous or failed checks. Probe polling is independent of inference, with no inference warmup. Native load/reload is bounded by the existing operation duration after P01's total provisioning deadline; periodic health checks have independent bounded contexts. Exact positive-log patterns are pinned-source fixtures, never substring acceptance of arbitrary output. Discard every other native line and overflow, retaining no raw content.

Successful calls retain child/model. Cancellation, deadline, native death or uncertain transport marks unready, terminates/kills the old group and Wait/reaps within cleanup before replacement; a new load may take longer but remains unready. Backend owns its own deadline even after API loss. Shutdown uses the existing drain plus cleanup budgets and clears only owned disposable work, preserving model cache/prepared files. Return protocol-neutral domain.Failure causes; no raw child/URL/path diagnostics cross the adapter.

Binding verification: Q-STARTUP/Q-CANCEL/Q-FAILURE/Q-PRIVACY/Q-OBSERVABILITY/Q-ERRORS/Q-GOFOUNDATION, using subprocess and private HTTP tests already named below. Test-first actual PID/group lifetime, CPU fallback rejection, independent health, request identity and bounded output; PROVE by bypassing GPU readiness, reap ordering and one private guard. Real CUDA remains P07/P08.

## Technical agreement

Selected proposal: `p02-resident-supervisor-v1`. Packet: `P02`.

- proposal_digest: `fbeebbf6f0b5fa5a652394e45725d9e535ba7c3fa47833b0166ed74f3aa99961`
- agreement_ref: `{id: 94e10aa1-7886-444d-9a00-430d59b6c6c1, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Write helper-process/private-protocol failure checks; establish controlled RED without a real GPU.
2. Implement model/fingerprint assembly and one owned child with bounded output and positive CUDA readiness guards.
3. Implement private handlers, compatibility/deadline checks and independent health; retain child on normal success.
4. Implement cancel/crash/shutdown stop-kill-Wait-reload ordering and owned-scratch recovery.
5. Run focused tests/race and full Go/vet/import/contract checks, required PROVE mutations, then update evidence and stop.

## Review correction evidence — 2026-10-22

At this earlier correction checkpoint, P02 was incomplete. The correction
addressed only the two reviewed defects; final packet acceptance is recorded below.

- HTTP cancellation now interrupts a blocked upload through the connection read
  deadline. The cancellation callback is stopped or joined before the outer
  handler resets deadlines, and owned request cleanup still precedes reuse.
- Native retirement failures are classified once in `Supervisor.retire` as
  `domain.BackendUnavailable`, preserving the underlying `retirementError` and
  `context.DeadlineExceeded` for `errors.As` and `errors.Is` on every exit path.
- Permanent tests: `TestPrivateStalledUpload` covers native crash recovery and
  graceful shutdown over a real HTTP connection; `TestBackendFailureClassification`
  checks recovery/shutdown error classification and wrapped causes.
- RED: both tests failed in all four subcases before the production fix. The
  stalled upload exhausted cleanup instead of recovering/stopping normally;
  cleanup errors did not contain `domain.Failure`.
- GREEN on Go 1.27.3, from the repository root using the pinned toolchain in PATH
  with `GOTOOLCHAIN=local GOWORK=off`:
  - `rtk proxy go test ./internal/adapter/backend -run '^(TestPrivateStalledUpload|TestBackendFailureClassification)$' -count=1 -v`
  - `rtk proxy go test -race ./internal/adapter/backend ./internal/adapter/httpapi ./internal/platform/logging ./internal/domain -count=1`
  - `rtk proxy go test ./... -count=1`
  - `rtk proxy go vet ./...`
  - `rtk proxy .local/c01/venv/bin/python tools/check_go_architecture.py`

The specification and exact P02 agreement passed checks before and after this correction.
Command assembly, packet-wide PROVE evidence and completion were still outstanding
at that checkpoint. Real native/CUDA, container and Kubernetes acceptance were not
run; no dependency, public contract, commit, publication or deployment changed.

## Implementation evidence — 2026-10-22

- `cmd/backend` now assembles Load/Validate, Provision and CanonicalFingerprint,
  then the private listener and supervisor. Invalid settings and an occupied
  private port cannot start the native child. SIGTERM drains before cancelling
  the lifecycle owner; native/server outcomes determine the actual process exit.
- Existing Run/Health/Infer/Shutdown ownership, protocol 1, fingerprint/generation
  guards, one active operation, bounded CUDA evidence, fixed native flags and
  stop/kill/Wait ordering are preserved. Only private helper/resource layout
  changed; no public contract, native source or dependency changed.
- Added command subprocess tests for prepared-model/fingerprint wiring, readiness,
  SIGTERM, native reaping, cache/prepared-file preservation and failed GPU startup.
  Both ordinary command output and failure output are parsed as JSON and scanned
  for controlled native/privacy markers.
- Hardening checks caught and corrected intrinsic multipart-size overflow, a
  socket-deadline/context-timer race and swallowed HTTP aborts that leaked staged
  files/descriptors. Staged-file ownership now remains with the handler even if
  parsing panics. `http.ErrAbortHandler` propagates after cleanup.
- Fresh P01 config/model tests passed. The three saved native source Git blobs
  still match `b87ef27375fd614d9e7c9698ecda94b03e548eb6`,
  `5ffc70af00e76fc121b961613259c7c179a4d2d4` and
  `61041bdc16b3f978795de5e789ed7399498fb5b7`; no C++ changes were made.

RED: callable command stubs failed configuration, listener, readiness and actual
process-exit assertions. New bounds/deadline/abort tests failed their intended
assertions before the corresponding fixes. Earlier review-correction RED/GREEN
evidence remains above.

PROVE: each mutation was applied alone and restored byte-for-byte before the next:

| Mutation | Test detecting the defect |
| --- | --- |
| Remove positive CUDA allocation requirement | TestStartupEvidence / TestStartupReadiness |
| Bypass private compatibility guard | TestGenerationMismatch |
| Announce retirement before stop/kill/Wait | TestCancellationRecovery |
| Forward raw native stdout/stderr | TestCommandProcess/gpu-failure |
| Wire the wrong GPU device in command assembly | TestCommandProcess/ready-sigterm |
| Recover/restart after an ordinary successful inference | TestResidentSupervisor |

All six failed at the intended assertions, not compilation or a harness timeout;
all original/restored hashes matched. Private evidence is under `.local/p02/prove/`.
The first runner attempt could not locate RTK in its pinned Go PATH; it restored
the source and supplied no behavioral evidence. Passing the discovered absolute
RTK executable corrected this local harness failure without changing global tools.
This ignored runner is not a repository or build prerequisite.

Final GREEN, repository-root working directory, Go 1.27.3 first on PATH,
`GOTOOLCHAIN=local GOWORK=off`:

- `rtk proxy go test ./... -count=1`
- `rtk proxy go test -race ./cmd/backend ./internal/adapter/backend ./internal/adapter/httpapi ./internal/platform/logging ./internal/domain -count=1`
- `rtk proxy go vet ./...`
- `rtk proxy go mod verify`
- `rtk proxy go mod tidy -diff`
- `rtk proxy gofmt -l cmd internal` (empty)
- `rtk proxy go build -o .local/p02/backend ./cmd/backend`
- `rtk proxy .local/c01/venv/bin/python tools/check_go_architecture.py`
- `rtk proxy .local/c01/venv/bin/python tools/check_contracts.py`
- `rtk proxy .local/f01/bin/govulncheck ./...` (exit 0, no reachable vulnerabilities;
  one advisory in a required module is outside the imported/called packages).

The prepared local Python/scanner paths above record actual tools used, not new
application dependencies. GPU commands and native inference in these tests are
controlled subprocess fixtures. They do not prove CUDA cancellation, memory
stability, image/chart behavior, hosted CI or real GPU performance. No model
download, publication, deployment, commit or automatic next packet was performed.

## Unread private body correction — 2026-10-22

- Private HTTP request bodies are closed while their read deadline still applies.
  Routing/header rejections receive the configured operation bound; valid private
  headers can shorten it. Connection deadlines remain owned by `net/http` through
  body cleanup and response flushing, rather than being reset by the handler.
- A cancelled request without an owned response propagates `http.ErrAbortHandler`.
  This prevents an implicit empty 200 after a socket read deadline cancels the
  request context. Existing safe error statuses remain intact when deliverable.
- `TestPrivateUnreadBodyCleanup` uses real TCP uploads that stop before the
  declared body length: operation expiry, stale generation, invalid headers and
  an unknown route. It checks bounded response/connection completion, HTTP server
  shutdown, no staged files, no native dispatch and an unchanged resident child.
- `TestPrivateSuccessfulKeepAlive` confirms that successful requests reuse one
  connection even after the previous request's deadline has elapsed.

RED: all four stalled-body cases blocked error delivery and HTTP shutdown on the
original implementation; successful keep-alive already passed. The first fix also
exposed an implicit 200 after read cancellation. Failed attempts remain separate
under `.local/p02-review-20261022/`; fixture corrections account for abort exits
and the standard server's bounded TCP half-close delay. Only the expired upload
may end with EOF instead of a response; no case accepts a socket timeout or a
false success.

Final GREEN on Go 1.27.3, with the same pinned PATH, `GOTOOLCHAIN=local GOWORK=off`:

- Focused unread-body/keep-alive/stalled-upload/abort/deadline tests passed;
  `.local/p02-review-20261022/fix-green-4.log` preserves their result.
- `rtk proxy go test ./... -count=1`
- `rtk proxy go test -race ./cmd/backend ./internal/adapter/backend ./internal/adapter/httpapi ./internal/platform/logging ./internal/domain -count=1`
- `rtk proxy go vet ./...`
- `rtk proxy gofmt -l internal/adapter/backend/http.go internal/adapter/backend/http_cleanup_test.go` (empty)
- `rtk proxy .local/c01/venv/bin/python tools/check_go_architecture.py`
- `rtk proxy .local/c01/venv/bin/python tools/check_contracts.py`

The specification and exact P02 agreement were rechecked before and after this
correction. No dependency, native source, public contract or packet scope changed.
Real CUDA/container/cluster acceptance remains in P07/P08; no external action or
independent post-fix review was performed.
