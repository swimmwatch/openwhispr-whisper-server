# P07 — RTX 5070 Ti Laptop Docker acceptance and measurements

Outcome: reproducible real CUDA evidence for standalone public transcription.
Requirements: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-007, REQ-011, REQ-012, REQ-013, REQ-014, REQ-015, REQ-017, REQ-018, REQ-019, REQ-020, REQ-027, REQ-028.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: planned; technically agreed (2026-10-22), not executed. Exact agreement below checked ready against the pinned Approved/reviewed specification. A separate incremental invocation, passing dependencies and any scoped external authority remain required. Preserve completed work; recheck source and agreement freshness before execution and completion.

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

## Technical proposal

Extend a reusable Go acceptance harness under tests/acceptance, with isolated Docker target setup and sanitized outcome reports under docs/verification. Bind ../quality/scenarios.yaml docker_gpu, observation and all Q-CACHE/STARTUP/INPUT/BOUNDS/FIFO/CANCEL/ISOLATION/HEALTH/FAILURE/PRIVACY/RESIDENT/PACKAGING/ROLLOUT/PERFORMANCE/DISTRIBUTION/RELEASEGATE scenarios; ../core/contracts.yaml runtime/models/private_backend/configuration/delivery and public OpenAPI. Use existing integration checks rather than copy them.

Test only R01's anonymously pulled immutable API/backend candidate manifests on the actual RTX 5070 Ti Laptop target, using P05's one canonical Compose stack. Record actual GPU/driver/VRAM/OS/Engine/Compose/runtime/tool/model/source/image identities and effective limits/rate settings. Do not replace published images with a local rebuild or infer CUDA from enumeration/health alone. Positive native CUDA/model-buffer facts, correlated actual GPU inference and public transcription are required.

Use pinned licensed public audio and derive all advertised formats, including Opus and moov-at-end MP4. Test the default model plus a separately pinned fitting model through config/restart only. Exercise actual OpenAI-compatible client and OpenWhispr supported transport with recorded versions; handwritten HTTP alone is insufficient. Required unavailable client/hardware/driver/observation prerequisites remain incomplete, not waived.

Observe child/group/model-init identity, model-file open/read/mapping, safe resource/fd/file counts, RSS/VRAM and output marker traps, without content logging or production debug endpoints. Calibrate idle/longest-input envelope, then the contracted three blocks of 20 successful requests and failure/cancellation sequences; at least 30 steady requests and declared concurrent sample report startup/load separately from median/p95/public/backend/queue timings and processing/audio ratio. Do not invent a numeric performance SLO or require zero driver baseline memory. Bounded latest native result/allocator retention is documented, not treated as per-request accumulation.

Run all Compose recipes: downloaded cache, zero-source-access offline restart, read-only prepared file, missing-file failure, unchanged restart, changed-config pair recreation and ordinary down/up without volume deletion. Prove native stop/reap, truthful readiness, no old/new GPU overlap and cache retention. FIFO/resource workloads explicitly override rate settings so they reach queue paths; dedicated limiter checks remain separate.

Test harness assertions before trusting evidence and apply required reversible PROVE fixtures/mutations for file retention, per-success reload, overlap and raw output. Mutation images are isolated and cannot qualify the original candidate; restore and rerun the exact accepted identities. Runtime fixes require a new R01 RC and renewed affected checks. Execution needs explicit access to this target; no registry publication/cluster work. Reports record passed/failed/unrun checks and limitations; no substitute environment can complete P07.

## Technical agreement

Selected proposal: `p07-docker-gpu-evidence-v1`. Packet: `P07`.

- proposal_digest: `a91beca407917e6c1206f99d93223f0d72452634c9da1fe53f0efeb0a418b31e`
- agreement_ref: `{id: be6db71a-6ebf-4f95-bca4-b81f2da2171a, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Verify explicit Docker GPU target/client/observation access and exact R01 candidate identities.
2. Write shared acceptance assertions and target driver, calibrate baseline and validate assertion sensitivity.
3. Run every format/client, lifecycle/queue/cancel/privacy/resource and Compose recipe on actual CUDA.
4. Run configured retention and performance workloads; restore any isolated mutation and rerun exact candidate identities.
5. Write sanitized independent Docker report with passed/failed/unrun checks; fixes requiring changed inputs return through a new RC.
