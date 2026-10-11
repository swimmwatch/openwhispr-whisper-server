# F01 — Go architecture, settings and HTTP foundation

Outcome: maintained Go dependencies, protocol-neutral failures and reusable strict startup, logging, HTTP error adaptation and admission middleware before transcription implementation.
Requirements: REQ-003, REQ-013, REQ-019, REQ-029, REQ-030, REQ-031.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Require this packet's explicit invocation and a usable Git worktree. Read linked requirements through workflow MCP, recheck guidance/.codegraph and source freshness before work/completion; never read workflow YAML or review archives directly. No packet execution is authorized by this plan.

Prerequisites: I01 module/tooling, I03 agent rules and C01 bootstrap contributor CI complete. Read core/contracts.yaml runtime.api, go_foundation, observability, rate_limit and configuration; contracts/openapi.yaml parameters/headers/x-error-registry; quality/scenarios.yaml Q-CONFIG/Q-OBSERVABILITY/Q-RATELIMIT/Q-GOFOUNDATION. Primary source links and proposed structure are in plan.md Go foundation decisions; no Go production code currently exists.

Run checks from the project root with rtk. Tests below are to create, not observed passes. Write/run discriminating RED before behavior, implement to GREEN, and PROVE absence/wiring claims not exercised by RED with one reversible mutation. Missing files/symbols alone are not semantic RED. Restore mutations before final checks.

## Ownership and scope

Allowed: go.mod/go.sum; internal/domain/**; internal/platform/config/** and logging/**; internal/adapter/httpapi middleware, errors and ratelimit/**; adjacent tests; tools/check_go_architecture.py and narrow existing contributor-CI/check integration. Add packages only for actual behavior, not empty scaffolding. Application/domain types cannot import adapters/platform/Echo/Koanf/validator. Runtime entry wiring belongs to cmd constructors in the later consuming packets; no dummy HTTP server, GPU/model implementation or Redis client in this foundation packet.

Pin Echo v5.4.0, Koanf v2.3.8 and validator/v10 v10.30.5 using the existing Go 1.26.5 module. Resolve verified compatible provider/transitive versions and commit-worthy go.sum; inspect licenses/security and actual module minimum versions. Use standard-library slog/flag/context/error facilities rather than adding alternative logging/CLI/DI frameworks.

Read core/contracts.yaml go_foundation.error_contract and Q-ERRORS. Define the small domain Failure/FailureKind format with semantic Kind, optional logical Field and wrapped Cause; implement ordinary Error/Unwrap and use errors.Is/As plus %w. No HTTP statuses/headers, Echo types, public messages/type or JSON tags belong in this format. Use standard Go errors, without an additional error framework.

Create the shared MapFailure/HTTPFailure in the HTTP adapter with table-driven correspondence to the existing OpenAPI x-error-registry. This adapter owns status, safe fixed public message, type/code, whitelisted param and allowed response headers. Preserve wrapped classification, use safe 500 internal_error for unknown failures, and never serialize Failure/Cause or match message text. Middleware/framework failures use the same HTTP presentation policy without pretending to be business failures; health retains its existing schema. Validate fixture handlers now; P02/P03/P04 verify actual port/route/lifecycle wiring, response commitment and disconnect behavior. No additional protocol adapter is implemented.

Implement the shared typed settings loader and startup field validation now; P01 adds model artifact/metadata behavior and semantic capability checks. Use Koanf layering/providers with explicit supplied-flag detection. Validate every source before merging; unknown keys may not disappear under later overrides. Use a JSON token precheck for duplicate keys/trailing documents, strict scalar decoding and ErrorUnused, and scoped collection replacement. Document explicit flag/env mappings for new logging/rate/proxy values. Unsupported/deferred storage/settings and invalid rate/burst/TTL/CIDR combinations reject before listener/native startup. Do not mistake StrictMerge or validator tags for the full configuration contract.

Logging initialization produces JSON stdout and an injected logger. Shared safe context helpers carry only validated request ID and safe event fields. Configure RequestLogger hooks and Recover so final mapped status/error code is logged once without raw error, URI/query, panic value/stack or request content. Early config failure uses a safe startup logger without dumping settings. Test logger callbacks at every severity.

Validate/replace X-Request-ID before Echo RequestID/RequestLogger, normalize the request header to one accepted value and copy the ID into owned context. The response header uses the same ID on success, errors, unknown routes and probes. Do not capture a pooled Echo Context in queued work. P02/P03/P04 extend the context through actual private transport/application calls.

Inject Echo's existing RateLimiterStore into the HTTP adapter constructor; use its RateLimiterMemoryStoreWithConfig in V1. Use RateLimiterStoreContext when available for existing retry headers/context. No parallel custom limiter algorithm or redundant store-interface hierarchy is needed. Normalize any store error to allow=false, including a misbehaving substitute returning true plus error; Echo otherwise admits that boolean. DenyHandler must distinguish quota denial (429 rate_limit_exceeded with positive Retry-After) from storage errors (503 rate_limit_unavailable). A substituted store is exercised in tests; future Redis is a separate implementation with bounded context-aware calls, not a current dependency.

Install the middleware chain in the documented execution order: safe request ID, request completion logging, safe panic recovery, transcription-only rate limit, bounded input/application. F01 tests the reusable chain on a fixture handler; P03 verifies wiring on real transcription routes. Probes bypass rate limiting. Direct peer is the default IP source; explicit XFF trust disables Echo's implicit private/loopback/link-local trust. Normalize valid addresses with net/netip and unmap IPv4-mapped addresses. Never use request ID or an untrusted header as the quota key.

## Discriminating checks

| Check to create | Expected result |
| --- | --- |
| TestStrictConfigurationSources/Types/Collections | CLI > env > JSON > defaults; only explicit flags override; duplicate/unknown/source-invalid/deferred fields reject; collections fully replace; bad settings cannot start listener/process |
| TestRequestIDMiddleware/ConcurrentCorrelation | Accepted/generated response and owned-context ID match for success/error/404/probe; duplicate/unsafe IDs are replaced and cannot inject; concurrent contexts remain independent |
| TestStructuredLogging/RecoverPrivacy | Every emitted line is valid JSON with actual final status; unique content/raw-error/panic/query/header/filename markers absent at debug/info/warn/error |
| TestFailureWrapping/TransportIndependence | Direct and %w-wrapped failures preserve Kind/Field and errors.Is/As cause lookup; domain type has no transport fields/tags/dependencies |
| TestHTTPFailureMapping/Unknown/Fields | Every registry cause maps to exact status/code/type/schema, wrapped known failures remain known, unknown cause/private markers stay out of body/logs, absent/unknown Field maps to null |
| TestHTTPFailureResponseOwnership | Effective request ID survives fixture error responses; committed/disconnected response is not written again, health-specific schema remains valid |
| TestRateLimitAdmission/Refill/ProbeBypass | First IP exhausts while second remains allowed; denied handler never reads body/starts decoder/enqueues/calls backend; refill and Retry-After work; probes stay responsive |
| TestTrustedClientIP | Direct mode ignores forged XFF/Forwarded/X-Real-IP; trusted/untrusted multi-hop IPv4/IPv6 and malformed chains cannot choose arbitrary keys |
| TestRateStoreSubstitution/Error/Expiration | Injected store controls decisions; errors (including true plus error) fail closed with 503; stale IPs clear after churn/TTL, independent stores and restart reset local state |
| check_go_architecture | go list import graph rejects adapter/platform/framework and concrete net/http/os/exec/filesystem integration imports from domain/application; intentional forbidden import detected; no generated/empty app placeholders |

Use deterministic clocks or a tiny test-only TTL with bounded eventual assertions against the actual maintained store; do not depend on arbitrary multi-minute sleeps or assert only fake-store behavior. For PROVE, bypass one ID guard/IP trust check/rate middleware/privacy allowlist or forbidden-import check and confirm the owning check fails, then restore.

Planned commands: `rtk proxy go mod verify`; `rtk proxy go test ./internal/domain/... ./internal/platform/... ./internal/adapter/httpapi/... -count=1`; repeat affected packages with `-race`; `rtk proxy python3 tools/check_go_architecture.py`; `rtk proxy go vet ./...`; check `gofmt -l` has no entries. Wire these real checks into existing contributor CI once packages exist. GPU tests and full image/chart Security Check remain later packet evidence.

## Completion

An incompatible/missing module or violated import/config/middleware contract leaves F01 incomplete. Fix the owning integration/dependency and rerun affected checks; do not relax configuration, privacy or trust guards to obtain green. Record module/tool identities and actual results plus RED/GREEN/PROVE evidence. Recheck source freshness, mark only F01 in [todo.md](todo.md) after required checks pass and update [handoff.md](handoff.md). Preserve unrelated work and stop after this packet; no automatic next packet, commit, publication or deployment.
