# P03 — Multipart validation, audio normalization and HTTP adaptation

Outcome: the exact public transcription contract with owned bounded inputs/results.
Requirements: REQ-003, REQ-006, REQ-009, REQ-010, REQ-012, REQ-013, REQ-018, REQ-019, REQ-021, REQ-023, REQ-029, REQ-030, REQ-031.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-22) for local P03 adapter implementation and required acceptance checks. The exact agreement and pinned Approved/reviewed specification remained fresh. Scheduler/command assembly, release FFmpeg packaging and actual GPU/container/cluster acceptance remain in their owning packets; none are claimed here.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: P01 and P02 private protocol/checks. Read contracts/openapi.yaml paths, TranscriptionForm/Transcription/Error schemas and x-error-registry; core/contracts.yaml runtime.decoder, configuration.settings input bounds and private_backend; quality/scenarios.yaml Q-INPUT/Q-BOUNDS/Q-ISOLATION/Q-FAILURE/Q-PRIVACY/Q-FUTURE/Q-OBSERVABILITY/Q-RATELIMIT/Q-GOFOUNDATION; core observability/rate_limit/go_foundation. Requirement evidence: spec.md:150, :160, :224.

## Ownership and scope

Allowed: internal/adapter/httpapi/**, internal/adapter/audio/**, internal/adapter/upstream/**, consumed plain DTO/port declarations in internal/domain/** and internal/application/transcription/**, related fixtures/integration tests and necessary approved-module bookkeeping. These declarations are consumed by actual adapters; scheduler behavior and cmd/api assembly remain P04. No GPU/model embedding, extra public endpoints, native modifications or new service dependency.

Reuse F01 middleware and validator integration on actual routes; no framework binding into domain and no duplicate limiter/store implementation. Preserve safe request ID across every produced response and private HTTP request; map limiter 429/store-error 503 distinctly from queue_full. Probes bypass rate limits. Denied requests perform zero body reads/decoder/queue/backend work. Use application-owned ports for normalized requests/results; framework objects stay in this adapter.

Reuse F01 MapFailure on the real owning handlers/shared error handler. Read go_foundation.error_contract and Q-ERRORS. Audio and upstream adapters turn decoder/native/private-HTTP failures into protocol-neutral Failure values before returning through Normalizer/InferenceBackend ports. Service errors stay independent of HTTP status and JSON; only the HTTP adapter adds the existing status/type/code/param/safe message. Wrapped known failures keep their mapping, unknown failures become safe 500 internal_error, and Error()/Cause are never copied into responses or logs. Transport-only framing/routes/limiter failures remain here. Preserve health-specific schemas, request ID, response commitment and no-response-after-disconnect rules.

ReadMultipart streams under body/part/field limits and reserved staging capacity; exactly one file/model, optional language/prompt/temperature/format/stream, duplicates and unknowns reject. Do not use automatic unbounded FormFile/ParseMultipartForm spooling. Uploaded filenames/MIME are not paths or validation truth. Keep UTF-8 prompt bytes bounded; finite temperature [0,1]; stream literal false only; fixed model ID only, other ID 404. Validate language against pinned upstream metadata/table and model capability (English-only auto resolves to English).

Normalize runs the pinned FFmpeg executable without a shell, uses bounded uploaded stdin through cache:pipe:0 and only pipe/cache protocols and supported demuxers, selects audio only, and counts mono 16 kHz signed 16-bit samples. Stop/reap on deadline/cancel, reject overlong audio instead of truncating. FFmpeg seek cache uses isolated /tmp; inspect open/unlinked descriptors as well as directory entries. Own randomized work directories, delete upload/normalized files on every terminal path and purge abandoned own work on startup. One decoder and bounded retained-request credits/storage prevent aggregate resource growth.

A body cap includes payload + prompt/fields + bounded multipart framing; exact-limit file must fit. Bound staging duration, subprocess output and upstream response; decode failure/unsupported/no-audio ->400, audio payload overflow ->413. No embedded URL/local-file access is allowed. Tests must cover MP4 with moov at end.

Upstream adapter sends only normalized file and validated/defaulted fields, fixed no-context/carry-prompt behavior, JSON native response plus private compatibility/deadline headers. Reset defaults each request, parse only this request's text and adapt JSON/text/error responses. Never use raw native errors/content in public failures. No automatic retries. Do not enqueue Echo Context/ResponseWriter or retain its pooled buffers; emit result in the owning handler. Public unknown/deferred routes return JSON 404; no automatic documentation routes.

## Acceptance and checks

Create httptest route/provider tests and real decoder integration tests (FFmpeg required; unavailable means this packet remains incomplete):

- TestTranscriptionMiddlewareWiring: supplied/generated IDs on real success/errors/404/probes, per-IP deny/refill, trusted proxy spoofing, storage-error 503 and zero rejected body/decoder/queue/backend work.
- TestHTTPFailureRouteConformance/TestPortFailureConversion: real routes exercise every registry cause, typed/wrapped domain and port failures, unknown/unsafe cause and logical field mapping; assert schema/status/headers, no double write and safe response/log output. PROVE with one changed known mapping or deliberately serialized Cause, then restore.
- TestTranscriptionFields: exact required/optional field grammar, every duplicate, wrong/unknown model, language/model compatibility, UTF-8/temperature/stream, statuses and schemas.
- TestAudioFamilies: WAV/MP3/MP4/MPEG/MPGA/M4A/OGG/FLAC/WebM, Opus, disguised filenames, corrupt/no-audio and moov-at-end.
- TestAudioBounds: exact cap succeeds, one extra byte gives 413; decoded duration over bound rejects; slow input/hung decoder stops, memory/disk/decoder slots are bounded.
- TestDecoderSourceConfinement: network and sentinel-file reference traps receive no reads; remove protocol/demuxer guard for PROVE.
- TestUpstreamContract/Isolation: default/empty prompt after nonempty, language/temperature reset, JSON/text adaptation, malformed replies/backend errors/socket loss never become false 200.
- TestPublicRouteAbsence/PrivacyCleanup: /load/inference/warmup/unload/metrics/admin/auth/docs absent; zero upstream calls for invalid input and no markers/files after terminal paths. PROVE via one deliberately forwarded invalid input, retained file, stale prompt or raw error.

Commands: `rtk proxy go test ./internal/adapter/httpapi ./internal/adapter/audio ./internal/adapter/upstream -count=1`; `rtk proxy go test -race ./internal/adapter/httpapi ./internal/adapter/audio ./internal/adapter/upstream -count=1`. Fake-backend checks do not complete the real CUDA/client checks in P07/P08. Record safe fixture provenance/encoding, no private audio.
## Completion

Recheck the pinned specification version and run the focused checks plus `rtk proxy go test ./...` and `rtk proxy go vet ./...` where Go behavior is in scope. Keep required failures/unavailable environments incomplete; never label planned work passed. Update this packet's evidence, the single [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop after this packet. No automatic next packet, review, commit or publication.

## Technical proposal

Preserve delivery order P02 -> P03 -> P04. This packet implements transport/decoder/upstream adapters and the consumer contracts they require; P04 implements the scheduler and final cmd/api assembly. Allow internal/domain plain DTOs and internal/application/transcription port/option declarations in addition to the adapter paths below. Do not add a placeholder scheduler or a second module.

Binding application ports are Normalizer.Normalize(ctx, Input) (Normalized, error), InferenceBackend.Health(ctx) (Health, error), and InferenceBackend.Infer(ctx, Normalized) (Result, error). Input carries an adapter-created owned upload path and validated model/language/prompt/temperature/request-ID values; it never accepts a client path. Normalized owns a single-use WAV io.ReadCloser, its byte length/duration and copied inference fields. Result and Health carry plain semantic data, with domain.Failure for errors. Normalizer implements cleanup through the returned reader's Close, which P04 calls only after execution/readers have stopped. Infer borrows that reader, does not close it or retry submitted work. Exact private DTO layout/names except these named ports/methods remain flexible; no HTTP/framework/config objects enter application.

Implement httpapi ReadMultipart and owning handlers with Echo 5.4.0, existing Install middleware/MapFailure and validator. A narrow handler-consumed Transcriber.Admit(ctx, Input) (Result, error) boundary permits adapter tests before P04. Reserve bounded HTTP staging capacity before reading the body, derived from max(1, waitingCapacity+1), with bounded aggregate raw/decoded disk/memory and upload/decode time. Fully validate before FIFO admission; transport-only errors stay in HTTP. The handler owns raw upload disposal until its blocking service call safely terminates. Service/domain code sees no Echo Context/ResponseWriter. Invalid/rate-denied input causes zero inference; denied rate work also causes zero body/decoder/admission work.

Bind ../contracts/openapi.yaml paths, TranscriptionForm/Transcription/Error/health schemas, absent routes, X-Request-ID and x-error-registry, plus ../core/contracts.yaml runtime.decoder, configuration/settings/transient_storage, go_foundation/error_contract, observability, rate_limit and private_backend. Enforce exactly one file/model, allowed optional fields, byte/UTF-8/range/language/capability limits, stream=false and JSON/text only. Unknown model never switches/downloads; return the contracted 404. Use bounded multipart framing/part counts and explicit file streaming, not automatic FormFile spooling. Filenames/MIME are untrusted.

audio.Normalize owns bounded FFmpeg n9.0.2 subprocess execution, no shell, stdin cache:pipe:0 with only pipe/cache protocols and an explicit safe demuxer set, including seekable moov-at-end MP4. Output is counted mono 16-kHz signed-16-bit PCM with a constructed WAV header; count overflow instead of truncating. Only uploaded bytes may be accessed; no nested external URL/file/playlist access. Reap on every stop, bound output and /tmp, and remove only owned files. A real compatible FFmpeg executable is an execution prerequisite, not permission to install it globally.

upstream Health/Infer implement the fixed private protocol with explicit fingerprint/generation/residual-timeout/request-ID headers, bounded bodies and no inference retries. Reset every field/default and no-context/carry-prompt policy for each call, parse only the current response, and convert private/native errors to semantic failures before returning. Headers/status/envelopes remain in HTTP adapters.

Binding checks: Q-INPUT/Q-BOUNDS/Q-ISOLATION/Q-FAILURE/Q-PRIVACY/Q-FUTURE/Q-OBSERVABILITY/Q-RATELIMIT/Q-GOFOUNDATION/Q-ERRORS; real httptest routes, decoder fixtures for every advertised family and malicious source traps, typed port failure conversion, cancellation/cleanup, body bounds and zero denied work. PROVE source confinement, no stale prompt/result, no deferred route and no raw cause leak. P04 adds real application wiring; P07/P08 add real CUDA/client evidence.

## Technical agreement

Selected proposal: `p03-http-audio-ports-v1`. Packet: `P03`.

- proposal_digest: `d7446921538e3abd4b907e6649040c80fe9ff57255f6d461d741227ac8d0a50e`
- agreement_ref: `{id: 94e10aa1-7886-444d-9a00-430d59b6c6c1, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Define only actual consumed DTO/port contracts and write adapter/route RED checks before behavior.
2. Implement bounded multipart/staging/field validation and reuse real F01 middleware/error mapping.
3. Implement and run real FFmpeg family/bounds/confinement/cleanup checks; no globally installed substitute is implied.
4. Implement private Health/Infer client, per-request reset and typed failures; test bodies, protocol and deadline ownership.
5. Run focused/race and full Go/vet/import/contract checks plus required PROVE; final application wiring remains P04.

## Implementation evidence — 2026-10-22

- Added consumed Input, owned Normalized, Result/Health aliases and
  Normalizer/InferenceBackend contracts in `internal/application/transcription`.
  No service, queue, command assembly or framework dependency was introduced.
- HTTP routes reuse F01 Install/error mapping and the selected validator.
  Streaming multipart parsing reserves bounded staging credits, validates
  body/fields/headers, IDs, model, language, UTF-8, temperature and formats before
  Admit, and retains owned upload files until the blocking call terminates.
  Denied rate/staging work performs no body/readiness/admission/downstream work.
  Unknown/deferred routes remain JSON 404. Marked abandoned uploads are purged
  under an exclusive lock; unmarked files/live writers are preserved.
- The handler-consumed Transcriber uses already agreed P04 Admit and Readiness
  operations. P03 supplies no queue/observer behavior. Language-name resolution
  uses the pinned native 100-language table; existing P02 codes are unchanged.
- FFmpeg normalization counts bounded PCM into a canonical WAV, limits active
  decoders/retained results and stops/reaps on expiry or overflow. Returned
  readers release normalized storage/credits on Close. Actual cache descriptors,
  their size bound and disappearance after cancellation were observed. Inherited
  FFREPORT is disabled. Fixture provenance is in the audio adapter README.
- The private client checks model/protocol/fingerprint/generation, copies IDs
  and residual deadlines, borrows normalized audio, bounds replies, rejects
  malformed responses/redirects and maps failures to domain errors. It never
  retries inference; consecutive requests verify prompt/default/result isolation.

RED: callable boundaries failed intended HTTP status, normalization, deadline
and private-protocol assertions before implementation. Logs remain in `.local/p03/`.
Corrected fixture failures are preserved: MPGA needed an explicit MP2 muxer;
staging cleanup needed to join its handler; the descriptor scan misidentified
`.owner.lock`; and FFREPORT initially affected the fixture encoder as well.

PROVE used one compiler source overlay at a time, leaving checkout bytes intact:

| Mutation | Discriminating test |
| --- | --- |
| Broaden protocol/demuxer access to external DASH sources | TestDecoderSourceConfinement |
| Reuse a prior nonempty prompt | TestUpstreamContractIsolation |
| Add a public metrics route | TestPublicRouteAbsence |
| Serialize a wrapped private Cause | TestHTTPFailureRouteConformance |
| Retain an upload session | TestPrivacyCleanup |
| Retain normalized output after Close | TestAudioFamilies/wav-pcm_s16le |
| Start concurrent decoders | TestDecoderSingleProcess |
| Bypass the real route limiter | TestTranscriptionMiddlewareWiring |
| Permit inherited FFREPORT | TestAudioNoStreamAndReporting |

All nine failed their intended assertions, with checkout source hashes unchanged.
The first confinement mutation was ineffective: FFmpeg rejected the HLS fixture
for missing filename/MIME hints before exercising the guard. That failed evidence
remains in `.local/p03/prove/`. Adding a valid DASH wrapper made file/network traps
discriminate the weakened guard; its separate run is in
`.local/p03/prove-confinement-2/`. Passing inventory: `.local/p03/prove-final.json`.

Final GREEN from the repository root, Go 1.27.3 first on PATH,
`GOTOOLCHAIN=local GOWORK=off`:

- `rtk proxy go test ./... -count=1`
- `rtk proxy go test -race ./internal/adapter/httpapi ./internal/adapter/audio ./internal/adapter/upstream ./internal/domain ./internal/application/transcription -count=1`
- `rtk proxy go vet ./...`
- `rtk proxy go mod verify`
- `rtk proxy go mod tidy -diff` (empty)
- `rtk proxy gofmt -l cmd internal` (empty)
- `rtk proxy .local/c01/venv/bin/python tools/check_go_architecture.py`
- `rtk proxy .local/c01/venv/bin/python tools/check_contracts.py`
- `rtk proxy .local/f01/bin/govulncheck ./...` (exit 0; no reachable/imported-package
  vulnerabilities; one advisory remains outside imported/called packages).

Logs: `.local/p03/final-*.log`. Real synthetic fixtures exercised WAV, MP3, MP4,
MPEG, MPGA, M4A, OGG/Vorbis, OGG/Opus, FLAC and WebM/Opus, including moov-at-end,
exact/over payload and decoded-duration bounds, no-audio/corrupt input, source
traps, actual PID/group lifetime, cache descriptors and cleanup. The compatible
developer executable was FFmpeg 6.1.1-3ubuntu5. The release baseline remains
n9.0.2; P05 builds/pins that artifact. No n9.0.2 build, API command, FIFO,
container/Helm/GPU/client acceptance, deployment, publication, commit or independent
review was performed. Sources/ownership match the frozen proposal; no shared
binding, native source, dependency, settings default or public contract changed.

### Post-review cancellation correction — 2026-10-22

The handler now checks request cancellation immediately after Readiness and
aborts the response before interpreting health or calling Admit. This closes
the early-return path that produced an implicit empty HTTP 200 on cancellation.
The permanent `TestTranscriptionReadinessCancellation` uses a real HTTP server
with a cancellable server context. Error and not-ready results previously
returned HTTP 200; a ready result previously reached Admit. All three cases now
abort without admission and leave no owned upload session.

RED: the new test failed all three intended assertions before the production
change. GREEN on Go 1.27.3 with the same PATH/toolchain settings as above:
`go test ./... -count=1`,
`go test -race ./internal/adapter/httpapi -count=1`, `go vet ./...` and
`tools/check_go_architecture.py` passed. No dependency, shared contract or
packet boundary changed; P04 remains unstarted.
