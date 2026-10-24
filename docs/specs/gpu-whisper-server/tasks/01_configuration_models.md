# P01 — Canonical configuration and model artifacts

Outcome: one validated Resident configuration and trustworthy startup artifacts.
Requirements: REQ-003, REQ-004, REQ-005, REQ-006, REQ-008, REQ-017, REQ-021, REQ-023, REQ-031.

## Inputs and execution boundary

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-22). The explicit incremental invocation implemented and verified only P01 against the accepted Technical proposal. The pinned specification passed with Approved/reviewed state and the exact planning agreement remained ready before execution and completion. Unrelated F01 work is preserved. Evidence is recorded below; the frozen proposal is unchanged. No commit, publication, independent review, runtime wiring or next packet was performed.

Run all commands below from the project root with rtk. Named tests/harnesses are to create; none exist at planning time. Tests/checks precede behavior. Record RED, correct implementation to GREEN, then PROVE absence/wiring claims or assertions not exercised by RED with one reversible mutation. Restore mutations before final checks. A missing symbol/harness alone is not sufficient RED evidence.

Prerequisites: F01 Go/config/logging/validation foundation complete, plus fresh reviewed inputs. Repository evidence is spec.md:49 (configuration), :80 (artifacts), :88 (cache), :96 (fixed model); these are requirements, not existing implementation.
Read core/contracts.yaml selectors configuration, models, runtime.api; quality/scenarios.yaml Q-CONFIG/Q-CACHE/Q-FUTURE/Q-CONTRACTS.

## Technical proposal

Proposal ID: `p01-native-artifacts-v1`. Scope remains this packet's nine linked
requirements and Resident V1. Existing code is `config.Load` at
`internal/platform/config/load.go:17`, `Settings.Validate` at
`internal/platform/config/settings.go:102`, and `validateModel` at
`internal/platform/config/settings.go:181`. Model provisioning, metadata parsing
and fingerprint functions do not exist yet. Recheck those locations at execution.

Preserve `core/contracts.yaml` selectors `configuration` (precedence, strict
sources, CFG-CATALOG/CFG-SERVED/CFG-DOWNLOADS/CFG-CACHE/CFG-PROVISION/
CFG-ATTEMPTS and all existing bounds), `models` (sources, integrity, publication,
metadata and offline behavior), `runtime.backend.revision`, and
`go_foundation.boundaries/error_contract`. `quality/scenarios.yaml`
Q-CONFIG/Q-CACHE/Q-FUTURE/Q-CONTRACTS/Q-GOFOUNDATION owns the applicable checks.
Reconcile material changes to these references before execution; a saved Markdown
digest alone does not prove their continued compatibility.

**Binding units and signatures.** Extend the existing configuration package;
do not introduce a parallel loader. Add `func (m Models) Validate() error` and
`func CanonicalFingerprint(settings Settings) (string, error)` there.
`Settings.Validate` delegates model-set semantics to `Models.Validate`;
intrinsic source errors still reject before later overrides. Download-set IDs
must name download sources, and served/download sets must reference the catalog.
Invalid configuration returns the existing bounded `config.ErrInvalid`.

Add one concrete `internal/adapter/models` package with
`func Provision(ctx context.Context, opts Options, client *http.Client) (Artifact, error)`
and `func ReadMetadata(ctx context.Context, source io.Reader) (Metadata, error)`.
`Options` contains `Models config.Models`, `CacheDir string`, `Attempts int` and
`Timeout time.Duration`, mapped from validated settings by later command assembly.
`Artifact` contains owned `ID`, `Path`, `SHA256` strings and `Metadata`;
`Metadata` contains `VocabularySize int32` and `Multilingual bool`.
Return only the served artifact; extra downloads remain verified cache files.
These exported names/signatures, fields and ownership boundaries are binding;
file names and equivalent private helpers remain flexible. No generic repository,
download framework, mock-only interface, application layer or startup command is
needed. The adapter depends on config/domain and filesystem/HTTP facilities;
domain/application do not acquire adapter or config imports.

**Preparation and ownership.** Provision owns copies of collection inputs and
one context capped by the earlier parent deadline or configured total timeout,
covering every selected model, lock wait and retry. Process the configured download
set sequentially, then obtain the served artifact. A served download source outside
that set may only reuse an already valid cache file; missing/invalid data fails
without an unrequested download. Prepared sources are opened read-only, never
repaired, downloaded or deleted. No goroutine, file, response body or lock survives
the function; the persistent cache is the deliberate retained output.

Use standard `net/http`, `crypto/sha256`, `encoding/binary`, `encoding/json` and
`os` facilities; use `os.Root` confinement for the service-owned cache tree.
Use the already pinned `golang.org/x/sys/unix` v0.48.0 `Flock` binding for Linux
cross-process locks; promoting this module to a direct requirement is allowed,
without introducing a new module. Nonblocking acquisition and cancellation-aware
timer waits remain a small adapter policy, not a second locking algorithm.
No Windows/macOS support, Redis, C++ bindings or new dependency is introduced.

Downloaded artifacts live at `<cacheDir>/artifacts/<sha256>.bin`; stable lock
files live at `<cacheDir>/locks/<sha256>.lock` and are not unlinked on release.
All service-owned paths stay beneath the opened root; reject symlink/non-regular
artifact or lock targets. Hold the artifact lock across cache recheck, repair
attempts and publication. Stream each miss into a private random same-directory
`<sha256>.partial-*` file with restrictive permissions and constant memory.
Verify SHA256, optional expected size and metadata before file fsync, atomic
rename and parent-directory fsync. Check the verified cache before any network
request; never truncate or delete a previously valid artifact. Remove only own
partials, or stale partials for that same artifact while holding its lock.
At most `Attempts` downloads fit within the total provisioning deadline; retry
delays honor cancellation. Enforce an expected size while streaming when supplied;
do not add an undocumented model-size configuration/default. Storage exhaustion
fails safely and cleans the owned partial. Use an injected concrete HTTP client
without mutating or closing caller-owned transports; a local client configuration
enforces HTTPS redirects, rejects redirect credentials/downgrades, bounds redirect
count and closes every response. No cookies, URL/credential diagnostics or raw
transport logs are retained.

Read only the fixed GGML header needed for capability, using the exact native
loader/vocabulary rules at upstream revision
`6ac14546046ffb17d144a08bcad65337e535cf5` (`src/whisper.cpp` and
`ggml/include/ggml.h`). Reject bad/truncated headers; never allocate from untrusted
dimension values or infer multilingual capability from an ID/path. Header checks
do not prove full tensor/native/CUDA compatibility; P02 owns actual native load.
Filesystem/network/integrity/metadata/deadline failures use existing
`domain.Failure{Kind: domain.ProvisioningFailed}` with an unwrap-able cause;
no HTTP metadata or raw failure text becomes public. Invalid options fail before
side effects. Preserve `errors.Is` cancellation/deadline inspection.

**Configuration fingerprint.** Both binaries must compute it from validated
configuration without opening model files; the API never provisions models.
SHA256 hash deterministic JSON with all fields present in this fixed order:
`version: 1`, `model: {id, source: {kind, url, path, sha256, expected_bytes}}`,
`backend: {gpu_device}`. Select only the declared served catalog entry and use
its configured values, including an empty optional prepared checksum; return
lowercase hexadecimal. Exclude catalog ordering, unserved/download-only entries,
download set, cache directory, bind addresses/ports, paired backend URL, logging,
rate/proxy settings, queue/input/time budgets and request fields. This token
identifies configuration compatibility; it is not authentication or proof of
prepared-file contents. Actual file hashing/integrity and native capability are
checked separately. A fingerprint-schema change requires consumer reconciliation.

## Technical agreement

- packet_id: `P01`
- proposal_id: `p01-native-artifacts-v1`
- proposal_digest: `f23b7a0f0aac9d97f42e2ae9b0d1231c7acdd76c5cdc94f25f8112f7db6a89ac`
- agreement_ref: `{id: 3ade1343-9827-4eb4-9493-1472641cd19c, revision: 2}`
- source: `docs/specs/gpu-whisper-server/tasks/01_configuration_models.md`, heading `Technical proposal`
- readiness: `planning_packet_check` returned `ready`, no issues (2026-10-22).

The managed revision preserves the actual synchronous question and answer.
Keep these metadata outside the frozen proposal; do not edit that section after
agreement. A material technical change returns P01 to planning. Planning does
not authorize implementation, commit, real model downloads, deployment or P02.

## Ownership and scope

Allowed: necessary go.mod/go.sum dependency updates; model-specific extensions to internal/platform/config/**; internal/adapter/models/** and their tests. Reuse I01's module `github.com/swimmwatch/openwhispr-whisper-server` and agreed Go baseline; do not initialize a competing local module. No server, Docker/Helm behavior or downloads of full models during unit tests.

Reuse F01 Koanf/validator strict loading and safe logging; extend typed model/catalog semantics instead of creating a competing loader/validator. Read go_foundation and Q-GOFOUNDATION. Logging/rate/proxy settings keep F01 defaults and validation, and component-only settings never enter the model fingerprint. Load/Validate combine CLI > WHISPER_ environment > JSON file > defaults, replace collections, reject unknown/duplicate keys and validate IDs/sources/sets/bounds. Defaults come from CFG entries, including resident/base, queue 16, upload 25 MiB, prompt 8 KiB, decoded 900 s, operation 600 s, cleanup 10 s, shutdown 30 s, provisioning 1800 s and 3 attempts. The active capacity is fixed at one. Deferred modes/policies/warmup and multiple served models reject before serving.

Provision prepares only configured downloads, then verifies the served artifact; a missing served artifact outside the download set fails instead of causing an unrequested download. Existing prepared sources remain read-only. Use per-artifact POSIX locking, bounded HTTPS streaming/retries, checksum/optional size verification, fsync/atomic publication and ownership-safe partial cleanup. A valid cache is checked before network access. Never delete cached model files during request/shutdown cleanup. ReadMetadata extracts pinned GGML capability (including multilingual vocabulary); do not embed inference or guess from ID.

CanonicalFingerprint covers the shared served artifact identity and backend inference configuration, excludes component bind addresses/paired URL, and supplies a safe deterministic compatibility token. Return owned immutable data and typed safe failures. Models depend on config/filesystem/HTTP, not Echo.

## Acceptance and test-first checks

### Execution prerequisites and evidence

F01 is complete; actual configuration/error/logging/HTTP foundation packages
exist and passed local Go 1.27.3 checks. `Load` and model validation entry points
are located above; `internal/adapter/models` remains proposed. The preflight
observed a usable Git worktree and no staged files; existing unstaged/untracked
F01 work must be preserved. Reinspect every staged diff if that state changes.
Neither a clean checkout, commit nor an optional code review is required.

Run on Linux amd64 with the pinned Go 1.27.3 compiler selected on PATH,
GOTOOLCHAIN=local and GOWORK=off. The current host has that authenticated compiler
and the existing Python tooling environment; reverify before execution. RTK and
the project workflow MCP are required agent tools. No GPU, native binary, Docker,
Kubernetes credentials, global installation or privileged filesystem access is
needed. Tests use local temporary directories, small generated GGML headers,
httptest TLS servers and child copies of the Go test binary for lock/crash tests.
Actual shared-volume/POSIX semantics in Docker/Home Lab remain P07/P08 evidence.

### Stages within this one packet

1. Write failing configuration/fingerprint cases, then extend existing model
   semantics and add the canonical fingerprint. Preserve all F01 source/default
   regressions. Cover prepared entries in downloadSet and valid served models
   outside that set, plus collection-order/component-setting invariance.
2. Write header/capability fixtures from the pinned native source, then implement
   the bounded metadata reader. Keep full tensor/native/CUDA validation in P02.
3. Write offline prepared/cache and ownership checks, then implement verified
   read-only preparation and cache hits. Observe source HTTP requests directly.
4. Write TLS failure/retry/deadline/integrity/redirect tests, then implement the
   confined streaming downloader, publication and cleanup. Observe disk bytes,
   permissions and requests, not success logs or mock-only store calls.
5. Write cross-process lock/crash/publication tests, complete recovery behavior,
   apply required PROVE mutations and run final checks. Update nearby configuration
   documentation, this packet, todo and handoff; stop without P02 or a commit.

### Discriminating verification matrix

The following test names are planned checks to create, not existing harnesses.
Every row uses the test-first sequence in the execution boundary above.

| Requirements / boundary | Planned check and observation | Expected result |
| --- | --- | --- |
| REQ-003/004/021/031, config | TestModelSets and retained F01 config tests | Unknown/prepared download IDs and inconsistent sources fail; valid partial overrides and explicitly replaced collections remain valid; defaults and unsupported-setting rejection stay unchanged |
| REQ-003/006/031, fingerprint | TestCanonicalFingerprint with a golden vector and input perturbations | Served declaration/GPU changes alter the hash; catalog order, extra entries and excluded component settings do not; no model file or network is needed, including prepared files without a checksum |
| REQ-004/005/008, metadata | TestModelMetadata with bounded readers, bad magic, truncation and English/multilingual fixtures | Capability follows the pinned vocabulary rule; no filename inference, header-sized dimension allocation or unsupported-format acceptance |
| REQ-004/005/006, offline | TestOfflineCache/TestPreparedArtifact using a counting TLS server and read-only prepared files | Zero source requests on a valid hit/prepared file; missing served data outside downloadSet fails without fallback; prepared bytes/permissions remain unchanged |
| REQ-005/017, integrity/recovery | TestDownloadFailures with interrupted streams, checksum/size faults, disk/write failures and corrected retry | No partial is published; valid cache entries remain unchanged; bounded attempts and later successful recovery; expected-size counting is overflow-safe |
| REQ-005/017/031, cancellation | TestProvisionCancellation for lock wait, body streaming and retry delay | Total/parent deadline stops work; returned failure unwraps cancellation/deadline; response bodies, locks and owned partials are released |
| REQ-005, inter-process publication | TestConcurrentPublication and helper-process crash fixtures | A writer cannot request/publish while another process holds its artifact lock; observers see absent or complete verified files; abrupt loss permits a later owner to recover only that artifact's stale partials |
| REQ-005/031, containment/privacy | TestArtifactContainment/TestProvisionFailures for symlinks, unrelated sentinels and controlled private markers | No cache escape or broad deletion; invalid targets fail; ordinary Error() output is bounded and does not reveal URL/path/content markers; Cause remains internal |
| REQ-023/031, contracts/architecture | Existing contract and import checkers plus the real module graph | Canonical defaults/contracts and inward domain/application imports still pass; no Echo/native-process integration is introduced into this packet's production units |

For absence/structure assertions not already exercised by a semantic RED, PROVE
one relevant behavior at a time: force a request on a verified cache hit, bypass
checksum validation, bypass the held lock, or expose the final filename before
the stream completes; the owning check must fail. Mutate a fingerprint exclusion
if its original RED was only a missing harness. Restore every mutation before
final verification and record actual outcomes; do not add permanent mutation
scripts or fabricate a RED from missing symbols alone.

| Criterion | Layer/check to create | Expected discriminating result |
| --- | --- | --- |
| Sources/overrides produce one canonical config | internal/platform/config TestConfigurationSources/InvalidConfiguration | Replaced catalogs contain no bootstrap leftovers; unknown/deferred/invalid values reject |
| Verified cache/prepared file works offline | internal/adapter/models TestOfflineCache/TestPreparedArtifact | Trap server receives zero requests; missing prepared file never downloads |
| Interrupt/checksum/size/retry failures preserve valid cache | TestDownloadFailures/RetryDeadline | No published partial; own partials do not accumulate; bounded failure and corrected retry |
| Concurrent writers never publish partial content | TestConcurrentPublication with separate helper processes | Same final verified SHA, no reader sees partial, no live writer's file removed |
| Header/capability/fingerprint is trustworthy | TestModelMetadata/TestCanonicalFingerprint | Invalid headers reject; English/multilingual metadata is not filename-driven; component addresses do not alter compatibility |

Use tiny generated header/file fixtures and httptest fault servers, not actual model downloads. Commands: `rtk proxy go test ./internal/platform/config ./internal/adapter/models -count=1`; `rtk proxy go test -race ./internal/platform/config ./internal/adapter/models -count=1`. PROVE by bypassing checksum, replacing collection merge semantics, or permitting one forbidden network access; each owning assertion must fail. Check prepared-file bytes/permissions before/after and lock semantics, not success logs.

No data migration; failed provisioning is unready. Correct configuration/restart is recovery; old valid cache remains usable. Source credentials/URLs must not appear in diagnostics.

### Commands and documentation

From the repository root, after selecting the pinned compiler on PATH:

```sh
rtk proxy env GOTOOLCHAIN=local GOWORK=off go version
rtk proxy env GOTOOLCHAIN=local GOWORK=off go test ./internal/platform/config ./internal/adapter/models -count=1
rtk proxy env GOTOOLCHAIN=local GOWORK=off go test -race ./internal/platform/config ./internal/adapter/models -count=1
rtk proxy env GOTOOLCHAIN=local GOWORK=off go test ./... -count=1
rtk proxy env GOTOOLCHAIN=local GOWORK=off go vet ./...
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod verify
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod tidy -diff
rtk proxy gofmt -l internal
rtk proxy python3 tools/check_go_architecture.py
rtk proxy python3 tools/check_contracts.py
```

The first command must report go1.27.3 linux/amd64; gofmt and tidy-diff must
produce no differences. Use the prepared Python environment for its checker
dependencies. Required package/import/contract checks already exist; the models
package and its behavioral tests are created during this packet. If the module
graph changes, rerun the pinned govulncheck and record actual license/security
results; do not install scanners into the application module or change versions
without agreement. No current planning check is a successful P01 behavior test.

Durable developer documentation is warranted: config, storage, process startup
and operator cache ownership cross several boundaries; offline behavior and
cleanup rules are easy to misconfigure. Extend the existing config README during
P01 for implemented semantics, and retain the existing P09 final documentation
packet after integration/acceptance. Do not create a competing documentation
packet or describe GPU/native readiness as implemented here.

## Completion

Recheck the pinned specification version and run the focused checks plus `rtk proxy go test ./...` and `rtk proxy go vet ./...` where Go behavior is in scope. Keep required failures/unavailable environments incomplete; never label planned work passed. Update this packet's evidence, the single [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop after this packet. No automatic next packet, review, commit or publication.

## Implementation evidence — 2026-10-22

Implemented model-set validation and configuration-only CanonicalFingerprint in
`internal/platform/config`. Added concrete `internal/adapter/models` with the exact
agreed signatures and fields. It owns copied selections, one total deadline,
read-only prepared files, verified confined cache, stable cross-process locks,
HTTPS streaming/retries and atomic publication. Configuration documentation is in
`internal/platform/config/README.md`. Domain/application imports and F01 defaults
and loading semantics are preserved; no runtime wiring or eviction was added.

RED: TestModelSets exposed the original acceptance of prepared download entries.
Missing-symbol build failures were harness failures only. Callable initial
implementations then failed metadata, offline cache, prepared-file and invalid-option
assertions. Before downloading was implemented, TLS/integrity/retry/cancellation/
ordering tests failed on missing downloads and zero requests. Correct implementation
made them GREEN.

PROVE: five separate temporary mutations failed their owning assertions:

| Mutation | Discriminating assertion |
| --- | --- |
| Bypass verified cache hit | TestOfflineCache: valid cache contacted source |
| Bypass streamed checksum | TestDownloadFailures/checksum: expected provisioning failure |
| Bypass OS flock | TestConcurrentPublication: download started before artifact lock released |
| Publish before streaming completes | TestConcurrentPublication: partial exposed at final path |
| Include API port in fingerprint | TestCanonicalFingerprint: excluded configuration changed fingerprint |

All mutations were restored before final checks. Temporary runner and JSON evidence
are ignored under `.local/p01/`, not repository prerequisites. The frozen proposal's
raw SHA256 remained
`eddd4d71e6d85b2ce00cd4cc91aeb5e135fc0866d8935ccd7ac7765b0b9698ff`.

Final required checks passed on **go1.27.3 linux/amd64**, GOTOOLCHAIN=local and
GOWORK=off:

- Focused config/model tests and race checks; all project Go tests; `go vet ./...`.
- `go mod verify`; `go mod tidy -diff`; `gofmt -l internal` (no differences).
- Existing Go architecture and contract checkers, using the prepared Python
  environment and the pinned compiler on PATH.
- Helper processes verified lock waiting, complete-file publication, crash recovery
  and write-failure cleanup (child-only RLIMIT_FSIZE). TLS fixtures, lock/body/retry
  cancellation, a total multi-model deadline, copied collections, redirect/cookie/
  body ownership and unsafe filesystem targets passed.

Only already-pinned `golang.org/x/sys v0.48.0` was promoted to a direct dependency;
no module/version was added. Its LICENSE was inspected as BSD-3-Clause; the existing
THIRD_PARTY_NOTICES inventory covers it. Fresh govulncheck v1.8.0 source/symbol scan
exited 0: no reachable or imported-package findings; GO-2026-5932 remains a
module-only advisory for unimported x/crypto OpenPGP code. This is not a claim of
an advisory-free dependency graph.

Completion MCP checks: specification pass / Approved / reviewed, checkpoint
recorded with `approval_checkpoint_written=false`; exact P01 agreement revision 2
ready, no issues. This invocation did not write a checkpoint or run an independent
code review. No full-model download, native load, Docker, Helm or GPU execution was
performed; these belong to later packets.

Next packet: P02, requiring its own explicit invocation and fresh technical
agreement check. Owner: the executor of that future invocation.

