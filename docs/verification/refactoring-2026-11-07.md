# Refactoring — 2026-11-07

**Status: complete for the selected local refactoring scope.**
This is a new refactoring phase after the [repository audit](<audit-2026-11-07.md>),
not a restatement of its fixes. The comparison baseline is the already-audited
working tree preserved before this phase, not Git HEAD or the published rc.6
images. Earlier dependency/security updates, short-WAV mitigation and lifecycle
bug fixes remain attributed to that audit. Dependency manifests/locks,
[tool pins](<../../tools/versions.yaml>), the [API](<../../containers/api.Dockerfile>)
and [backend](<../../containers/backend.Dockerfile>) build recipes, and the
[pinned external build-input manifest](<../../containers/build-inputs.json>) are
byte-identical to the phase baseline. Changed production Go source inputs were
rebuilt into new local images; the refactor introduces no new dependencies.

The requested work covers justified patterns, existing library reuse, removal of
redundancy and meaningful named constants. Separate reviews address DRY, SOLID,
YAGNI, low coupling, high cohesion and performance. A principle is not considered
verified merely because a helper was extracted or a checklist was completed.

## Implemented changes

| Scope | Before | After and preserved boundary |
| --- | --- | --- |
| Production Go: one private HTTP response owner | `Health` and `Infer` repeated request execution, bounded response reading, body closure and transport-error mapping. | [Client.exchange](<../../internal/adapter/upstream/client.go>) owns that sequence; the single-use `readBounded` wrapper is removed. Each endpoint retains its status/schema policy. Borrowed upload lifetime, generation checks, request IDs and no-retry behavior stay with their existing owners. |
| Production Go: standard-library reuse | Multipart cleanup retained a `sync.Once` plus cached error; cancellation manually copied, cleared and shortened the waiting queue. | [Multipart cleanup](<../../internal/adapter/httpapi/multipart.go>) uses `sync.OnceValue`; [waiting cancellation](<../../internal/application/transcription/service.go>) uses `slices.Delete`. Concurrent callers share the terminal cleanup result; queue order, reference clearing, capacity and resource cleanup remain covered by behavior tests. |
| Production Go: one canonical format owner and named constants | Audio generated WAV headers and PCM limits; backend separately validated/updated the same header; both commands duplicated saturated size arithmetic. | [The canonical WAV leaf](<../../internal/adapter/wav/wav.go>) owns `Header`, `ValidHeader`, `MaxFileBytes`, `MaxDataBytes` and the consumed format constants. Audio, backend, commands and upstream use one representation without a general codec registry. RIFF saturation and frame alignment remain distinct; native-specific 201-frame padding, borrowed input and process lifecycles remain outside the codec. |
| Production Go: reuse the existing validator | Partial configuration validation retained reflected field indexes and copied validation rules, then repeated numeric-bound checks already needed by complete validation. | [Bindings](<../../internal/platform/config/sources.go>) now select Go-field namespaces for `validator.StructPartial`; [registered aliases](<../../internal/platform/config/settings.go>) own positive-seconds and positive-finite bounds. A private `RateLimit.refillExceedsIdleWindow` shares the unchanged refill formula. After complete settings validation has recursively checked intrinsic fields, the performance follow-up avoids repeating standalone rate validation; standalone middleware validation and partial-source presence guards remain. Strict source representation and nested model checks are retained. |
| Production Go: remove unnecessary cache state | The cache retained its parent `os.Root` only to close it later, and separately stored a path already represented by the artifacts root. | [Cache construction](<../../internal/adapter/models/cache_linux.go>) closes the parent after opening independently owned child roots. Download/cache result paths derive from `artifacts.Name()`. Child confinement, locks, checksums and exact returned paths remain covered. |
| Production Go: correct record ownership | Backend options imported the provisioning/cache adapter solely for its plain `Artifact` and `Metadata` records. | [ModelArtifact and ModelMetadata](<../../internal/domain/model.go>) now belong to domain and have no I/O dependencies. Models return them and backend consumes them without a concrete models-adapter dependency. This is an explicitly approved internal Go type/signature migration without aliases; the historical [P01](<../specs/gpu-whisper-server/tasks/01_configuration_models.md>) names are not silently rewritten. Public HTTP/control schemas and record fields are unchanged. |
| Python tooling: native scanner configuration instead of scaffolding | Two scanner entry points created empty configuration/ignore files solely to disable ambient files. | One [native isolation flag tuple](<../../tools/security/check_secrets.py>) supplies empty paths to pinned Trivy. Four writes disappear; private reports/cache, environment filtering and explicit rules remain. This does not fix inline suppression reporting. |
| Python tooling: standard-library hashing and copying | Scanner identities buffered the entire executable and concatenated metadata; promotion copied JSON data by serializing and parsing it again. | [ScanSession](<../../tools/security/run.py>) streams through `hashlib.file_digest` and ordered `update`, preserving the emitted digest. [Promotion rendering](<../../tools/release/prepare.py>) uses `copy.deepcopy` for its JSON-decoded inputs; final serialization remains. Neither change proves a measured speedup. |
| Python tooling: remove redundant work and orchestration coupling | Inventory assembly read source metadata twice and repeated directory creation; security imported pure freshness validation through the release CLI/policy module. | [Assembly](<../../tools/release/inventory.py>) parses once and leaves directory creation to save. Unchanged [freshness validation](<../../tools/release/validation.py>) now lives with pure identifier checks; consumers import it directly and the old release export remains compatible. |
| Acceptance tooling: shared lifecycle ownership and constants | Four synchronous command callers repeated process-group creation, cancellation, a 200 ms wait bound and descendant cleanup. | [One owned-command runner](<../../tests/acceptance/owned_command.go>) now owns that sequence and its local wait constant. Docker commands, private exec transport, SDK commands and Kubernetes commands retain their own context, target arguments, environment, I/O bounds and error policy. Streaming GPU observation keeps its distinct Start/Wait lifecycle. |
| Acceptance tooling: standard-library reuse | Scratch snapshots were sorted and joined into strings for equality; two SDK fixtures copied map entries manually. | [Scratch checks](<../../tests/acceptance/kubernetes_scratch_baseline_test.go>) use `slices.Sort`/`slices.Equal`; [SDK fixtures](<../../tests/acceptance/client_cases_test.go>) use `maps.Clone` for the same shallow-copy requirement. Observation validity, deleted-open-file checks and thresholds remain unchanged. |

The shared runner consolidates behavior already present at the baseline. It is
not a newly discovered process-retirement fix. The new
[private-transport regression](<../../tests/acceptance/resident_direct_controls_test.go>)
exercises actual child retirement after a leader exits while its child retains
stdout; it does not contact a GPU or Kubernetes target.

## Principle-review findings

| Principle | Concrete result or remaining limit |
| --- | --- |
| DRY | One canonical WAV owner, one private HTTP exchange, one synchronous acceptance runner and one refill-window predicate replace repeated knowledge. Public/private/native error policies and distinct process lifecycles are deliberately not merged. |
| SOLID | Plain model records no longer require a backend dependency on provisioning. Existing narrow consumed ports and constructor injection remain suitable; no inheritance hierarchy, factory or speculative strategy registry was introduced. |
| YAGNI | Constructor-only cache state, duplicate paths, temporary empty scanner files, repeated input parsing and an unnecessary JSON copy round trip are removed. Rare safety checks are not treated as dead code. |
| Low coupling | Backend consumes domain records, and Python security imports pure validation rather than release orchestration. Shared HTTP error presentation remains an intentional contract boundary, not a new generic transport abstraction. |
| High cohesion | Canonical byte format, response lifetime, field policy and pure freshness each have their actual owner. Model I/O stays in its adapter; native padding and retirement stay in backend; the coordinated queue lifecycle is not split by file length. |
| Performance | Measurement exposed configuration overhead and prompted removal of redundant nested validation. Final validation removes one allocation; warm configuration loading still costs about 3% and 96 additional allocations. This startup/assembly tradeoff is explicitly retained, not called zero-regression. No cache, further optimization or service-level speedup claim was selected. Container CPU evidence remains separate. |

Two tempting replacements were rejected on observed semantics: `io.CopyN` can
hide a terminal read error after the requested byte count, while `io.NopCloser`
and plain process runners do not preserve borrowed-read or process-group ownership.
A `jsontext` migration was also deferred: the API is available, but exact null,
single-root, depth and numeric policies still require explicit project logic.
No availability failure or generic "all principles passed" claim is implied.

## Source and tooling verification

| Check | Actual result and limit |
| --- | --- |
| API/application response and cleanup checks | New [cleanup](<../../internal/adapter/httpapi/cleanup_result_test.go>), [FIFO cancellation](<../../internal/application/transcription/queue_removal_test.go>) and [transport-boundary](<../../internal/adapter/upstream/boundary_test.go>) controls passed before and after the refactor. Final scoped HTTP/audio/upstream/application/API-command normal, race and vet checks passed. An intermediate run hit a temporarily unregistered validator alias and an obsolete test reference; both were corrected before the coherent-source repeat. |
| WAV/backend/audio checks | Independent literal-wire, malformed-header, saturation and alignment [codec controls](<../../internal/adapter/wav/wav_test.go>) passed. Normal/race tests and vet passed for WAV, audio, backend and backend command; the real API assembly control also passed. One earlier attempt hit temporarily unused config imports during another owner's edits; its chained race/vet had not started. The coherent-source repeat passed. |
| Configuration/model checks | [Nested source and numeric-bound controls](<../../internal/platform/config/partial_validation_test.go>) and [download/offline path controls](<../../internal/adapter/models/cache_paths_test.go>) passed before and after changes. Complete config/models/domain/logging tests, race coverage and scoped vet passed. Whole-tree compile-only checks covered all fourteen packages; compiling opt-in tests is not running their targets. |
| Acceptance CPU regressions | Focused race subset repeated three times: PASS, 20.184 s. Complete ordinary suite: PASS, 16.862 s; race suite: PASS, 32.197 s, with the unchanged twelve-minute timeout. Each full run recorded forty runtime-gated skip events. These include no real GPU/cluster qualification; GPU-observer controls use command fixtures. |
| Python tooling and SDK checks | 164 tooling tests ran in 18.388 s: 163 passed, one pre-existing Trivy inline-suppression expected failure, no skips. All sixteen SDK-helper tests passed in 28.524 s. Ruff/mypy checks passed across 41 files; offline locked-environment validation passed. Expected failure is not a security-gate pass. |
| Native Trivy isolation equivalence | Nine hostile-config/ignore controls: ambient discovery hid the fixture finding; both previous empty files and new empty paths reported it. This establishes the selected configuration isolation, not complete scanner coverage. |
| Integrated Go source checks | After the final performance follow-up, all twelve production packages passed complete normal/race tests and vet; module verification and formatting also passed. The earlier full acceptance runs retain their original timing and scope rather than being relabeled as later reruns. |
| Final-source security checks | Refreshed after the performance follow-up: govulncheck reports zero reachable/imported-package vulnerabilities and module-only `GO-2026-5932` in unused OpenPGP; source secrets report zero findings. Gosec has six findings, raw exit 1, all exactly matched to five exception scopes with unchanged expiry and zero policy blockers. The obsolete backend G115 exception was removed. The separate 27-test security/compiled-Echo follow-up passed; it is not a rerun of all 164 tooling tests or a complete security-gate pass. |
| Performance-driven source follow-up | Expanded [typed-settings rejection controls](<../../internal/platform/config/validation_test.go>) retain invalid store, zero/negative rate/burst, idle, NaN/Inf and overflow rejection. Config and complete HTTP-adapter normal/race tests and vet passed. These checks do not establish that measured overhead has improved. |
| Comparable before/after performance | Both complete ten-pair batches are valid: 80 initial and 60 follow-up samples, none excluded. The second batch uses the unchanged baseline and final source. The measured residual cost and explicit disposition are recorded below. |
| Final Docker images and behavior | New builds, full P05 CPU checks, race-enabled integration checks, P06 rendering checks and the bounded current-image CPU observation passed on the identities below. Fresh scans and final source/image/resource verification completed; their scope and residual findings are recorded separately. |

One independent four-consumer Python check initially failed before test execution
because PATH selected uv 0.13.0. Repeating it with pinned uv 0.13.5 passed all four.
The tooling suite used CPython 3.14.9 and uv 0.13.5; source Go checks used 1.27.3.

## CPU performance comparison

The first refactored snapshot was compared with the preserved post-audit baseline
using Go 1.27.3 on the same Linux/amd64 host. Identical task-local fixtures exercised
actual project functions; six binaries were compiled before timing. Ten sequential
before/after pairs reversed order on alternate pairs: four cases, 80 passing samples,
none excluded. Each case used `testing.B.Loop`, a one-second target, two Go processors,
`GOGC=100` and no race instrumentation. Setup, preflight and connection warmup were
outside the timed loop. These are private characterization fixtures, not permanent
benchmark commands available in an ordinary checkout.

### Initial comparison, before the performance follow-up

Times are median [minimum–maximum] microseconds per operation.

| Workload | Baseline | First refactor | Median change |
| --- | ---: | ---: | ---: |
| Public multipart staging/cleanup | 159.189 [150.665–167.324] | 149.654 [143.776–162.167] | −5.99% |
| Private health + inference HTTP | 112.0015 [99.964–154.345] | 120.1075 [103.657–142.558] | +7.24% |
| Mixed-source configuration load | 437.2635 [409.512–445.267] | 448.0115 [437.932–464.674] | +2.46% |
| Loaded-settings validation | 4.137 [3.515–4.626] | 4.5525 [4.195–4.921] | +10.04% |

Configuration loading was slower in all ten pairs and added 97 allocations and
2,732 allocated bytes per call. Validation retained 30 allocations; its median
increase was 0.4155 µs. These unfavorable observations prompted the explicit source
follow-up and are not discarded or claimed fixed before its comparison.

Multipart timing ranges overlap; its allocation count remained 156, with about
40 additional allocated bytes. Private HTTP paired changes ranged from −16.23%
to +27.40%, with 460 allocations unchanged; no stable latency conclusion follows.
The HTTP fixture validates real loopback protocol exchanges but does no native
inference. Multipart covers parsing, disk staging and cleanup, not middleware,
FIFO or decoding. Configuration tests repeat warm metadata-cache work, not cold
process startup. Allocated bytes are not retained heap, RSS or GPU memory. The host
was not CPU-affinity isolated, and no statistical-significance claim is made.

### Follow-up comparison

A separately coordinated quiet window compared the final source with the same
original baseline binaries. Inputs, fixtures, processor/GC settings and alternating
order were unchanged. Both configuration cases and the upstream control ran for ten
pairs: all 60 samples passed, with no exclusions. Multipart was not repeated because
its measured source was unchanged. The first complete batch remains valid evidence.

| Workload | Baseline in follow-up window | Final refactor | Median change |
| --- | ---: | ---: | ---: |
| Private health + inference HTTP control | 119.3415 [101.803–133.194] | 109.2185 [102.413–131.417] | −8.48% |
| Mixed-source configuration load | 431.4415 [420.542–441.893] | 444.350 [408.755–448.137] | +2.99% |
| Loaded-settings validation | 4.151 [3.545–4.668] | 4.3325 [3.323–4.636] | +4.37% |

Validation removes one allocation, 30 → 29, and about 48 allocated bytes per call.
Its paired timing-change median is +1.53%, with a −27.75% to +18.14% range; a timing
improvement is not established. Configuration loading still adds 96 allocations
and about 2,760.5 allocated bytes per call, with a +3.50% paired timing-change median.
The upstream control's aggregate median reverses direction despite its unchanged
hot path; its paired median is +3.04%, with a −20.79% to +15.76% range. This variability
is reported rather than selecting the favorable aggregate result.

**Disposition:** retain the simpler implementation with the residual warm-load cost
explicitly acknowledged. Configuration runs during startup/assembly, not each
inference; these repeated warm-load measurements are not cold-start timings. No
further caching, optimization or benchmark repeat was selected. This is not a claim
of zero regressions, improved service throughput, lower RSS or GPU performance.

## Final local Docker verification

Both Linux/amd64 images built successfully with user `10001:10001`. The label
`working-tree-refactor-20261107-final` identifies this uncommitted working tree,
not a committed source SHA or a published release.

| Artifact | Identity |
| --- | --- |
| API local image ID | `sha256:0ff7119edbc04b87cdde431af722112a5b63db095e4315bc96a35e98015df6cb` |
| Backend local image ID | `sha256:4aa24b6e721787b9ec31a94d41c5e8b42166699664228bdea7e714e902dd75bd` |
| Runtime input identity | `sha256:e49ad63b2c314fa738b98b9b62980f0ac419c2ea89f611144c812fcf2aeec75b` |

Both Go executables were rebuilt. Native FFmpeg/CUDA/whisper.cpp stages and runtime
package layers were cached; this is not cold-build evidence. Earlier image
references were preserved. Non-fatal shared-library-symlink `ldconfig` warnings
remained in the API build; the build and actual image contract checks passed.

| Actual check | Result and scope |
| --- | --- |
| Full P05 CPU/image suite | PASS, 68.050 s; image contracts/inventories, twelve normalization formats, isolation, non-root/read-only execution, wiring/provisioning and cleanup controls. |
| New [public HTTP pipeline](<../../tests/integration/public_http_test.go>) | All eight cases passed, 3.12 s, within the full P05 run. |
| Focused image checks with race-enabled Go harness | PASS, 15.904 s; the same eight public HTTP cases passed in 4.25 s. |
| P06 render/schema matrix and chart checks | PASS, 9.288 s: Helm 3.22.0/4.3.0, Kubernetes schemas 1.35/1.36/1.37, five profiles and thirty renders, plus related checks. No cluster was contacted. |
| Fresh image vulnerability scans | Each image: 0 CRITICAL, 0 HIGH, 16 MEDIUM, 4 LOW, 1 UNKNOWN; zero exceptions. The twenty MEDIUM/LOW findings have no reported fixed version. The UNKNOWN OpenPGP advisory has a fixed version but is not imported/reachable in the source scan. These are residual findings, not zero vulnerabilities. |
| Image secrets | Zero findings in filesystem, configuration and Docker history scans. |
| Bounded current-image CPU observation | PASS: three warmups and ten serial observations, none excluded; median approximately 6.95 ms, range 6.23–11.49 ms. This is current-image fixture evidence, not a container before/after comparison. |

The image suites passed on their first attempts, with no failures or skipped
selected cases. Their elapsed times are test durations, not container performance
measurements. The public pipeline runs the real API and packaged FFmpeg against an
explicit CPU private-protocol fixture: WAV/JSON, MP4-AAC/JSON, Ogg-Opus/text, rejected
audio/multipart, malformed private replies, fingerprint mismatch, and active
cancellation followed by cleanup and reuse. Its readiness and generation are
synthetic; it does not prove native inference or GPU retirement.

The bounded observation ran after heavy checks/scans in a coordinated quiet window,
using a previously compiled private probe. Input was 100 ms of synthetic mono
16 kHz PCM16 WAV (3,244 bytes), with rate limiting disabled and a fixed CPU-fixture
reply. Timing covers the HTTP exchange through complete response-body close, not
request construction, probe-side validation, readiness checks or resource probes.
All thirteen public responses were individually checked for JSON/correlation. The
fixture counted thirteen valid private calls; forwarded private fields, request ID
and byte count were checked in the final observation, not separately sampled for
every request.
Scratch cleanup passed. API idle Docker counters were 8.68 → 8.359 MiB and 15 → 16
PIDs. These two idle snapshots are not peak memory, Go heap, leak or scalability
proof, and the timings are not a latency gate or native/GPU measurement.

Final verification at 00:43:11 UTC confirmed the same runtime binding, both new
image references and all nineteen earlier references. No owned P05 containers,
networks or volumes remained. Unrelated resources and operator caches were not
removed.

## Scope and limits

The selected local refactoring checks are complete with the recorded performance
tradeoff and security findings. Native/GPU inference and lifecycle acceptance,
Kubernetes runtime acceptance, cold native builds, full release/security
qualification and managed-workflow packet closure are not established here.
Nothing was deployed or published; these local images do not requalify published
rc.6. The [remaining security limitations](<security-audit-2026-11-07.md>) are not
cleared by this refactor.
