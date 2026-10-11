# P09 — Durable developer and operator documentation

Outcome: a reader can build, run, configure, verify and recover the actual delivered Resident service.
Requirements: REQ-001, REQ-004, REQ-005, REQ-008, REQ-009, REQ-010, REQ-013, REQ-015, REQ-017, REQ-019, REQ-020, REQ-021, REQ-022, REQ-023, REQ-024, REQ-025, REQ-026, REQ-027, REQ-028, REQ-029, REQ-030, REQ-031.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit invocation, usable Git worktree, passing dependencies and named target access are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Read linked exact requirements via MCP, recheck guidance/.codegraph and pinned source before work and completion. All commands run from project root with rtk. Create/run discriminating checks before any fixes; record relevant RED and corrected GREEN. A missing environment is not semantic RED. PROVE newly written absence/wiring assertions with one reversible mutation and restore. Required missing hardware/access/checks keep the packet incomplete.

Prerequisites: I02/I03 foundation and agent setup, R01 public candidate, P07/P08 actual GPU/lifecycle/performance evidence and all earlier required checks passed. Otherwise this packet stays pending, because final documentation must describe verified behavior. Read core/contracts.yaml configuration/runtime/deployment/delivery; contracts/openapi.yaml; quality/scenarios.yaml Q-FUTURE/Q-CONTRACTS/Q-PERFORMANCE/Q-INIT/Q-AGENTS/Q-DISTRIBUTION/Q-RELEASEGATE and exact safe reports. Evidence: spec.md:258 (future directions), :274 (planned contracts) and REQ-024–028.

## Scope, content and ownership

Allowed: final README.md and charts/whisper-server/README.md; docs/operations.md, docs/development.md, docs/agent-setup.md and docs/releases.md (extend the nearer authoritative document when present); update I02 community/change-history/third-party material, links to safe verification reports and small example corrections. README is deliberately created here after real verification. No code features, content records, roadmap implementation, CI/publishing/auth/admin/metrics behavior.

Durable documentation is warranted by multiple processes/layers, costly-to-rediscover cancellation/Resident rules and operator misconfiguration risk. Keep artifacts English per repository instruction. Explain the actual flow with one compact diagram, stable entry symbols and source identities, not a file inventory/specification copy.

Cover:

- Explain cmd/internal layout, inward imports/consumer ports, selected Go packages/check commands, strict settings precedence, safe JSON logging/X-Request-ID, local per-IP rate/burst/TTL, trusted proxies and distinct 429/503 causes. Document replaceable store and future Redis without advertising distributed quotas; read Q-OBSERVABILITY/Q-RATELIMIT/Q-GOFOUNDATION.
- Explain protocol-neutral Failure/FailureKind and Error/Unwrap/Is/As, port conversion and the HTTP mapper's existing OpenAPI envelope/status/param/header authority; include one verified wrapped-failure example and safe unknown-error fallback. Read Q-ERRORS and go_foundation.error_contract. A future protocol needs its own adapter; V1 implements only HTTP.
- Build/configure the two images; Linux amd64 GPU-free build versus GPU runtime; exact tested architecture/CUDA/driver mapping, both tested GPUs and actual/generic Kubernetes/Helm coverage.
- Standalone Docker and Helm install/public endpoint, pair ownership, prepared/offline cache, catalog/download/served distinction and precedence/replacement.
- Public multipart/JSON/text/errors and default bounds; one fixed resident model with one active operation/16 waiting; no per-request switching, idle unload or synthetic warmup.
- Truthful readiness, API/supervisor/native control, cancellation's exceptional safe stop/reload, normal no-restart reuse, signal/drain/grace, single-GPU downtime, failed replacement/rollback/uninstall cache retention and explicit separate disposal.
- No API authentication; anyone reaching the endpoint can transcribe. Existing TLS/CNI/storage/GPU prerequisites and safe ways to limit exposure. No credentials or content logging; transient API resources clear, bounded latest native result is permitted without cross-request influence.
- Separate measured performance reports for RTX 5070 Ti Laptop and RTX 2060, exact sample/input/tool/image/model identities, failures/unrun limitations; no invented latency/quality promise.
- A clearly separate required future-development section referencing spec REQ-008–010/022: ram-cache/on-demand, model selection/policies/switch release, warmup/unload/FIFO/idempotency/reclamation, likely fork/extension investigation, access issuance/auth, proposed admin UI and Prometheus metrics. Do not claim delivered features or discard retained semantics.
- Exact public GitHub/license/third-party identity, anonymous pulls for both image registries, OCI/classic Helm install commands with explicit versions/digests, Helm registry overrides and chart/app version distinction.
- Real local/CI check commands and contributor workflow, agent setup/discovery and optional tools; secret names/roles only, never credentials or personal cache paths. Document core/contracts.yaml delivery.security_checks and quality/scenarios.yaml Q-SECURITY: govulncheck/gosec/Hadolint/Trivy commands, blocking criteria, exact source/image/chart evidence, safe reports, narrow expiry-bound false-positive handling and native component coverage gaps.
- Pre-release/fix/new-RC sequence, both GPU gates, final-source evidence invalidation, image digest promotion versus stable chart repackage, partial publication/retry/rollback and manual deployment. Stable publication is R02 after these documents; do not claim it already happened.

Use authoritative contract links for defaults/schemas rather than another mutable table. Explain vocabulary and non-obvious constraints once; no transient line numbers in durable docs.

### Docker Compose examples

Add a Docker Compose quickstart to README.md and the complete recipe section to
docs/operations.md. Reuse P05's deploy/compose.yaml and deploy/compose.env.example;
do not embed a second full Compose definition in Markdown. Examples must describe
actual verified configuration fields, mounts and published identities.

| Recipe | Required example content |
| --- | --- |
| GHCR images | Both exact release image references, environment/config preparation, pull, start and public transcription |
| Docker Hub images | Override both references from the same release inventory and run the same stack without rebuilding |
| Local source builds | Both GPU-free build commands, explicit local image references and separate NVIDIA GPU runtime prerequisites |
| Prepared model offline | Read-only file path, source/model identity and UID permissions; start with no implicit download fallback |
| Populated cache offline | Verified downloaded cache, restart without model-source requests, stable model identity/checksum |
| Restart, change configuration and stop | Restart unchanged settings; recreate the pair for changed settings; readiness recovery, SIGTERM and down without deleting cache |

Each launch recipe is a complete sequence from the repository root: prepare its
non-secret inputs, validate the resolved Compose configuration, pull or build,
start, inspect /health/live and wait for /health/ready, then send a real multipart
request to the public API. Include status/log inspection, stop commands and
expected safe failure/recovery signals. Publish only the public API; default local
examples use loopback, and deliberate external exposure must explain V1's lack of
authentication. Never put credentials, personal audio or transcripts in examples.

Document NVIDIA driver/container-runtime prerequisites, one selected backend GPU,
tested Compose plugin version, non-root mount permissions and the separate roles
of .env interpolation, container environment and application configuration. Model
startup may take time; a running container does not establish readiness. A config
or environment change requires the verified recreation command because compose
restart does not apply it. Preserve the configured shutdown grace (baseline at
least 45 seconds) and explain which storage is persistent versus transient.
Routine teardown must omit --volumes and any operator model/cache removal.

Final examples are written here after P05/P07/R01 evidence exists. Do not advertise
nonexistent release tags, fabricate digests, or claim Compose execution based only
on YAML rendering. This planning addition does not create runnable examples now.

## Discriminating checks

Before editing, define a reader checklist and runnable example checks. A reader who did not write the feature must correctly explain startup/Resident ownership, what cancellation/shutdown may restart, RAM/VRAM retention versus temp-file cleanup, one-GPU rollout/cache retention, access without authentication and V1/future boundaries.

Create TestDocumentedConfiguration/TestDocumentedRequests (or reuse actual contract/example tests); run every build/run/config/HTTP/Helm example against the completed relevant test environment. Never substitute rendering for deployment examples or copy previous reports as a new run.

Planned focused command: `rtk proxy go test ./tests/integration -run 'TestDocumented' -count=1 -v`; use the P07/P08 harness for examples requiring their environments. Validate document links and actual saved example YAML/JSON. Reader verification is required evidence, not an optional code review gate. Correct misleading examples/content and repeat affected checks; do not weaken reader criteria to obtain completion.

Include every Compose recipe in TestDocumentedConfiguration/TestDocumentedRequests
or the reused example harness. Execute published-image recipes against exact
candidate digests, local-build commands in a clean GPU-free builder and runtime
steps on P07's GPU environment. Check /health/live, /health/ready and real public
transcription; verify offline source-access, private ports, backend-only GPU,
config recreation, graceful stop and cache retention through owning harnesses.
The independent reader must be able to select a registry, find effective settings,
identify required mounts/GPU, diagnose unready startup and stop without deleting
models. An unavailable required environment keeps P09 incomplete.
## Completion

Record exact successful/failed/unrun checks, environment/artifact identities and any mutation/fix. Recheck pinned specification freshness, update this packet's evidence, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md), preserve unrelated work and stop. No automatic next packet, commit, publication or unrelated infrastructure work.

