# P07 — RTX 5070 Ti Laptop Docker acceptance and measurements

Outcome: reproducible real CUDA evidence for standalone public transcription.
Requirements: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-007, REQ-011, REQ-012, REQ-013, REQ-014, REQ-015, REQ-017, REQ-018, REQ-019, REQ-020, REQ-027, REQ-028.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit invocation, usable Git worktree, passing dependencies and named target access are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Read linked exact requirements via MCP, recheck guidance/.codegraph and pinned source before work and completion. All commands run from project root with rtk. Create/run discriminating checks before any fixes; record relevant RED and corrected GREEN. A missing environment is not semantic RED. PROVE newly written absence/wiring assertions with one reversible mutation and restore. Required missing hardware/access/checks keep the packet incomplete.

Prerequisites: P05 and R01 passed their own checks; exact public candidate inventory/pulled image digests are fixed. The current machine actually has the named GPU, compatible driver/Docker NVIDIA integration and sufficient VRAM for chosen models. These are unverified runtime prerequisites, not inferred from a build or previous GPU listing. Read quality/scenarios.yaml environments/observation and all scenarios referencing P07; core/contracts.yaml runtime/private_backend/configuration.settings/delivery; contracts/openapi.yaml operations/errors. Requirement evidence: spec.md:35, :214, :236 and REQ-027/028.

## Scope and measurement ownership

Allowed: shared tests/acceptance/**, public fixture metadata (no personal audio), safe reports under docs/verification/**, and narrow fixes in previously authorized implementation units needed for the correct checks. If a finding requires product/architecture change, return to specification/planning rather than inventing a fallback.

Create TestResidentSuite/TestClientCompatibility/TestResidentMeasurements. The harness uses deploy/compose.yaml from P05, prepares a disposable test cache/scratch, records actual image IDs/toolchains and serves only the public API. It collects native process/start/model-file observations, API resource counts, safe GPU/RSS samples and emitted-log/file marker traps. Trace model open/read/mapping without dumping user buffer contents; do not add a production debug/admin/metrics endpoint.

Override local image names with R01's pulled immutable API/backend digests; do not test an unrecorded local rebuild. Bind all reports to candidate source/build-input/model/image identities. If a fix changes executable/build/runtime inputs, reopen R01 for a new RC and renew affected checks before recording candidate qualification. This packet does not overwrite a published candidate or relax stable gates.

Use pinned public audio with recorded license/source/SHA256, derive all advertised formats including OGG/WebM Opus and moov-at-end MP4. Default multilingual base and a second compatible model (for example tiny from a separately pinned catalog entry) are tested through configuration/restart, never request switching. Both must fit this GPU. Include invalid artifact/init/GPU failure, actual fixed-model CUDA activity, idle retention, normal no-reload and model-context release after stop.

Create an actual client interoperability check against the public endpoint, using a pinned OpenAI-compatible client and OpenWhispr's real supported transport. Record client versions/request settings and result; a handwritten compatible HTTP request alone does not establish OpenWhispr interoperability. Runnable client/transport is a prerequisite; unavailable or incompatible client remains unverified and returns the affected integration to planning, without changing the native server.

### Compose example acceptance

Extend the existing acceptance harness to exercise P05's exact Compose definition
and example inputs. Keep one full Resident/performance suite on the fixed candidate;
use focused real transcription/lifecycle checks for the additional recipes rather
than duplicating the benchmark for every registry.

- Pull the candidate's two immutable images from GHCR and Docker Hub, use each
  channel through the same Compose inputs, and record actual container identities.
  Public readiness and multipart transcription must succeed with the selected GPU;
  neither API GPU allocation nor supervisor/native host ports are allowed.
- Test downloaded-cache startup, populated-cache offline restart and read-only
  prepared-file startup. Observe the model-source boundary for zero offline
  requests; success logs or disabled internet alone are insufficient. A missing
  prepared file fails without an unexpected download/model substitution.
- Exercise ordinary restart, pair recreation after a fixed model/configuration
  change, SIGTERM and normal down/up. Verify configured shutdown grace, actual
  stopped/reaped native process, no old/new GPU overlap, documented downtime,
  cache checksum retention and new transient state. Never delete operator cache
  or add --volumes to routine cleanup.
- Record the exact working directory, Compose inputs, Engine/Compose/NVIDIA
  runtime versions and safe evidence for P09. Local-build recipe evidence comes
  from P05's build/wiring checks; published candidate qualification always uses
  R01 image digests and never a substitute local rebuild.

## Required workload and assertions

- Execute Q-CACHE/STARTUP/INPUT/BOUNDS/FIFO/CANCEL/ISOLATION/HEALTH/FAILURE/PRIVACY/RESIDENT/PACKAGING/ROLLOUT/PERFORMANCE on actual CUDA.
- Confirm all file families and invalid field/model/content/bounds cases; invalid/overflow/waiting-expired work never enters native inference.
- Use controlled ordered submissions for FIFO/max active one; active timeout/client disconnect/API/native kill then next request must prove no old/new overlap and truthful readiness.
- Record default and reduced test deadlines explicitly; do not misrepresent shortened fixtures as default-budget measurements.
- Normal repeated successes keep native process/model identity and have no recurring model-file reads/init. Calibrate an idle/isolated longest-input memory envelope before the repeated run; three blocks of 20 requests and failure sequences cannot accumulate request/model copies. API files/resources disappear on every terminal path.
- For shutdown/restart and an invalid model replacement, capture connections/results, child reaping, model allocation release and unchanged valid cache SHA. Disposable work from forced kill clears on restart.
- Privacy markers are absent from emitted logs/persistent/transient files outside intended response. Permitted bounded native latest-result state is checked by isolation/memory, not immediate zeroization.
- Q-PERFORMANCE: at least 30 steady concurrency-one samples plus a stated queue workload, same model/audio/parameters; separate startup/load, queue wait, execution/public latency median/p95, processing/audio ratio, API overhead, RAM/VRAM/errors. No numeric latency SLO is introduced. Direct native comparison, where feasible, is test-local and never makes a release route public.

Planned command (harness to create):

```bash
rtk proxy env WHISPER_TEST_TARGET=docker go test ./tests/acceptance -run 'TestResidentSuite|TestClientCompatibility|TestResidentMeasurements' -count=1 -v
```

Use real GPU activity correlated with actual native inference and CUDA/model-allocation evidence; mocks/GPU listing cannot pass. Since implementation predates these tests, PROVE absence assertions by deliberately retaining one API file, resetting process after success, leaking raw native content or admitting overlapping work, one at a time; show each owning check detects it and restore before the real final run.

Local container/test-fixture cleanup is authorized by the packet; preserve operator model files/caches. No registry upload or cluster changes. If hardware/driver/client/tool observation is unavailable, stop affected verification with the exact missing prerequisite and owner (unknown when not established).
Record effective logging/rate/proxy settings with each workload. For FIFO/resource/resident bursts, explicitly raise/disable rate limiting in that scenario so assertions exercise queue/resource ownership; retain dedicated enabled-limiter checks elsewhere. A rate_limit_exceeded response is not evidence of queue_full. No candidate code/guard is changed to obtain acceptance.

## Completion

Record exact successful/failed/unrun checks, environment/artifact identities and any mutation/fix. Recheck pinned specification freshness, update this packet's evidence, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md), preserve unrelated work and stop. No automatic next packet, commit, publication or unrelated infrastructure work.

