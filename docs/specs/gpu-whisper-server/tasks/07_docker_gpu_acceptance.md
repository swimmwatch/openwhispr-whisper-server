# P07 — RTX 5070 Ti Laptop Docker acceptance and measurements

Outcome: reproducible real CUDA evidence for standalone public transcription.
Requirements: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-007, REQ-011, REQ-012, REQ-013, REQ-014, REQ-015, REQ-017, REQ-018, REQ-019, REQ-020, REQ-027, REQ-028.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: in progress (2026-10-25), explicitly selected by the user. Matching
incremental-implementation manual/Git/specification checks passed and the exact
agreement below remained ready. Shared acceptance helpers and a real rc.2 Docker
baseline are implemented; repeated requests exposed HTTP 503 not_ready failures.
See [current handoff](handoff.md) and [sanitized rc.2 checkpoint](../../../verification/docker-rtx5070ti-rc2.md).
The packet remains incomplete; no GPU qualification, new release, commit/push or
P08 execution is claimed. Preserve R01 artifacts and renew affected acceptance on
a new immutable RC after any runtime-input correction.

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

## Local pre-release stage — 2026-10-26 (not candidate qualification)

The user explicitly requested fixing discovered defects and verifying locally
before any corrected release. The frozen proposal/agreement is unchanged; P07
remains unchecked. Local tests do not qualify unchanged rc.2 or authorize R01,
commit/push, publication, tags/stable or P08/cluster operations.

Final local runtime input identity:
`sha256:2cbb5d9db5451a3cbba00d76af845f35e4e86454bfe113eca10bda45c99eddbd`.
The [private local receipt](../../../../.local/p07/local-fixed-Wd6DfmOw/receipt.json)
binds actual API/backend immutable digests and false release/publication flags.
Earlier `f420…`/`5ad…` execution is historical only. See the
[safe local report](../../../verification/docker-local-pre-release.md) for exact
identities, commands/results, measurement caveats and remaining coverage.

Stale owned-Busy completion and native TCP teardown/rebind defects are corrected.
Private-native `SetLinger(0)` plus close-idle-before-stop preserve complete reply
validation/no uncertain POST replay, fixed ports, capabilities, native source,
exclusive foreign-socket guards and native group stop/Wait/reap ownership. Actual
final-image lifecycle 8/8 passed in 45.14s, including native SIGKILL recovery 4.89s
and reduced 3s operation deadline 408 `operation_timeout` in 3.521s plus fresh
inference. No host sysctl, global trust or driver change was made.

Final images passed baseline 60 and measured 60, bounds/isolation/marker checks,
FIFO/overflow/waiting expiry/cancellation, API termination, absent-GPU fail-closed,
counted HTTPS/cache/offline/prepared/tiny-model and deterministic unsupported-ftype
init-failure paths. Actual CUDA SM/framebuffer activity was correlated to the
native PID; serial resource snapshots and scoped model open/read/mapping trace
recorded zero recurring model-path operations. No payload/argv buffers were dumped.
Measured public median 99.443ms / p95 436.563ms include observer overhead and introduce
no SLO. Native initialization-to-ready is not pure weight-file load.

**Local server defect verification is complete, not full P07 acceptance.**
The [final resumed command](<../../../../.local/p07/local-fixed-Wd6DfmOw/local-final-resumed.log>)
completed in 144.277s with exit 1 solely on OpenWhispr Electron sandbox startup
before HTTP; the command is not relabeled as an aggregate pass. All 51 public
HTTP cases, both genuine OpenAI SDK representations and all server runtime/GPU,
resource/model/lifecycle groups passed. The user clarified server HTTP checks as
the current local-fix scope. Electron compatibility remains unverified for full
P07; it does not block this server report or publication of an unqualified RC.
No sandbox bypass, system helper installation or host policy change was made.

Shared ordinary-input JFK speech checks require independent whole-word cues;
request-count/media/error/nonempty guards remain. Temperature 1 is protocol-only,
not lexical-quality qualification. Explicit public config/prepared/model/CA bind
modes were exercised under `umask 077`; private evidence remains 0700/0600.
Earlier rigid lexical and fixture failures and the interrupted permission-corrected
recording remain retained as failed/incomplete. Source permits native fallback,
not a proven cause of the lost original responses. Final measured evidence is
[recorded separately](<../../../../.local/p07/run/local-1385521375/resident-measurements.json>).

Full CPU race/vet, focused final harness race/vet, Ruff/mypy, actionlint and SDK12
checks passed; the earlier tooling117 run disclosed12 C02/Helm fixture skips.
The separately prepared pipeline monitor added23 passing behavioral tests; the
combined tooling140 run passed with the same12 skips and full mypy40 sources.
These are not candidate-security or distribution qualification. Full P07 still
requires current genuine OpenWhispr compatibility, the prescribed absence/leak
sensitivity, idle/retention/failure-envelope/comparison and immutable-channel
checks not performed here. The parent next owns release preparation after explicit
commit/push/publication authorization; no new workflow or publication has started.

## Execution checkpoint — 2026-10-25

The user selected only P07 after matching skill/manual verification. P05/R01
candidate inputs were preserved. Restricted-session GPU access initially failed,
then host and exact backend-container NVIDIA enumeration succeeded after the user
changed session access policy, without changing the driver or global settings.

Implemented shared HTTP/statistics/Compose helper slices, bounded official SDK
probe and durable fault-server tests. Existing CI now validates their non-GPU
behavior with the normal compiler, Ruff/mypy/unittest and native workflow lint;
it still runs only on pull requests and does not launch GPU workloads. Required
full target suite entry points/scenarios are not all implemented or executed yet.

Actual unchanged rc.2 reached readiness and transcribed the pinned public JFK
fixture. Its sixty-request steady baseline failed: 11 successes, 49 not_ready
responses in the diagnostic run. All submitted private inference succeeded;
backend health continued to return HTTP 200. Correct checks were not weakened.
Deterministic application regressions reproduced stale owned-Busy health after
completion and late observations. A narrow local correction serializes health
commits and confirms fresh health before retiring that owner, without altering
public/port contracts, FIFO, cancellation/recovery ownership or CUDA guards.
Its additional bounded health read requires renewed real performance evidence.

Final local evidence: full non-GPU Go suites with race and vet passed; readiness
package race ten times; isolated PROVE failures detected and originals restored;
locked Python format/lint/types passed; 117 tooling tests passed with 12 disclosed
native C02/Helm fixture skips; 12 actual-SDK local fault tests passed without skips.
Pinned actionlint 1.7.12 plus existing ShellCheck passed all three workflows.
Optional Pyflakes was unavailable and is not counted as passed. Docker fake-CLI
cancellation observation was corrected to arm its child before cancel; the initial
missing-PID failure was not a production cleanup defect. See private evidence in
`.local/p07/` and the sanitized report linked above.

P07 remains unchecked. No replacement image was built/published and no tag moved;
local unit success does not fix/qualify rc.2. Complete client/format/queue/failure,
model-I/O/GPU/resource/privacy/retention/performance acceptance remains outstanding.
A corrected immutable candidate must first come through a separately authorized
R01 invocation. No P08, Home Lab, stable/latest, commit or push was performed.
