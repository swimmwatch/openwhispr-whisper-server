# P03 — Multipart validation, audio normalization and HTTP adaptation

Outcome: the exact public transcription contract with owned bounded inputs/results.
Requirements: REQ-003, REQ-006, REQ-009, REQ-010, REQ-012, REQ-013, REQ-018, REQ-019, REQ-021, REQ-023, REQ-029, REQ-030, REQ-031.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit authorization for this packet and a usable Git worktree are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Re-verify repository guidance/.codegraph before code lookup, obtain exact linked requirements through MCP, and check pinned source freshness before work and completion. Do not read workflow YAML directly.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: P01 and P02 private protocol/checks. Read contracts/openapi.yaml paths, TranscriptionForm/Transcription/Error schemas and x-error-registry; core/contracts.yaml runtime.decoder, configuration.settings input bounds and private_backend; quality/scenarios.yaml Q-INPUT/Q-BOUNDS/Q-ISOLATION/Q-FAILURE/Q-PRIVACY/Q-FUTURE/Q-OBSERVABILITY/Q-RATELIMIT/Q-GOFOUNDATION; core observability/rate_limit/go_foundation. Requirement evidence: spec.md:150, :160, :224.

## Ownership and scope

Allowed: internal/adapter/httpapi/**, internal/adapter/audio/**, internal/adapter/upstream/**, their test fixtures/integration tests, go.mod/go.sum for pinned Echo v5.4.0. No GPU/model library embedding, queue/scheduler behavior, auth/swagger/UI/metrics routes, lifecycle endpoints or native modifications.

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

