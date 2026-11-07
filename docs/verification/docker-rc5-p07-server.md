# P07 server verification on rc.5 — closed with accepted limitations

## Final disposition — 2026-10-28

**P07 is complete with explicitly accepted deviations, not an unconditional PASS.**
The user requested closure and selected the native question
`ask_user_question:p07_close_with_known_limitations` → “Закрыть с этими ограничениями”.
The accepted closure scope contains both unresolved items:

- **Memory qualification not established:** native resident-memory convergence
  failed; measured60 and calibrated terminal memory were not reached. The small
  RSS/Anonymous growth is accepted for packet closure. RSS does not prove a leak.
- **Native speech/performance limitation:** some complete HTTP200 responses did
  not pass the recognizability control, preventing a complete final uninstrumented
  timing comparison. Neither the control nor the benchmark is relabeled PASS.

The packet checkbox is complete on this explicit acceptance basis. Published
candidate state remains `candidate_published_unqualified`; no stable/release-wide
qualification or P08 result is implied. Functional Docker/public/queue/CUDA/
lifecycle/cache/isolation/privacy checks and the genuine SDK matrix passed as
recorded below. Final full acceptance race passed820.405s; CPU and vet passed.

The user declined a further GDB/all-stop profile, then requested ending memory
investigation. No debugger attach, symbol acquisition, allocator tuning, native
fork or image rebuild occurred. No further workload is needed for this invocation.
Actual Desktop/Electron interoperability remains excluded by the Approved REQ-012
amendment, not passed. The specification and exact v3 agreement remain unchanged.
The following execution history preserves actual failures and missing proofs;
its earlier “open/incomplete/next action” statements are superseded by this closure.

This is evidence for the [owning packet](<../specs/gpu-whisper-server/tasks/07_docker_gpu_acceptance.md>),
not P08, stable qualification, a new publication or permission to deploy.

## Specification and technical readiness — 2026-10-28

The native independent FULL review `9e3266d9-5c85-4cd1-a107-17b12542120f`
is complete: direct owning-MCP read-back confirmed 95/95 saved items,
`coverage_status: reviewed` and `consistency_status: consistent`. The author-made
Draft-to-Approved transition was then checkpointed by `specification_check_state`;
its latest result is pass / Approved / reviewed with no issues.

- source_version: `a3ec7bf82dcab75c5f621a68f7a4ffc12839d9f5314672105612f478f9526f90`
- specification_version: `e11e46218c26b12b4cab004ab4506bfa2fcedca731c539ea2dda5efbfab83f31`
- P07 proposal: `p07-go-heap-memory-evidence-v3`
- agreement_ref: `37645c04-f075-435c-8dc1-f9d334646ff8@4`
- proposal_digest: `79c2462ceae175fef4a48fcd0409f60123ce46f46178484940ca5ecc3b5cecc4`

The actual native user answer accepted only the P07 revision. Its exact
`planning_packet_check` returned ready / no issues. Revision 4 explicitly accepts
the v3 API marked-live/RSS-trend measurement method; v2 at revision 2 and its
failed observations remain historical. The v3 implementation and CPU controls are complete; actual memory qualification
remains unestablished under the explicitly accepted closure exception. P08/P09 bindings and the
shared hashed planning section were not rewritten. This readiness does not
qualify a GPU workload or authorize a commit, publication or cluster action.

Historical review `7ea7d321-d5ad-40f4-9467-c6ef2f544279` lost trust at 80/95 after
the checker disclosed summary-only source reuse. Supported MCP restart withdrew
its verdicts while preserving audit history; no old receipt or verdict was patched
into success. That earlier recovery hold is resolved, not a current blocker.

## Immutable target

The source/tag is `a98b7ccc8186ec8557d6430e005ad83ecfc2f4fa`, not a local
application rebuild. The image indices are:

- API: `sha256:f0415bd794f12d1154cbc06f8cad5520802593d31ae8d15f65d1aaa7c3a3fe16`.
- Backend: `sha256:017580bf66b109f5ccf16e243ed0d9d1ccdd3f6725af5b3c8149611b595150f9`.

See the [publication report](<release-rc5.md>) for all six distribution channels,
security limitations and original readbacks. New acceptance cases use the strict
published selection and record actual engine/container references and mounted
configuration bytes. Expected startup-failure bindings retain the same immutable
identity/security checks without falsely asserting that stopped containers run.

The target is the RTX 5070 Ti Laptop GPU with driver 595.99.02 and 12,227 MiB VRAM,
Docker Engine 29.9.2 and Compose v5.6.1. The GPU is shared with operator processes;
none was terminated. These measurements are not isolated hardware SLO evidence.
Only the parent-owned acceptance process uses the exclusive test lock.

## New actual results

| Check | Observed result | Private receipt |
| --- | --- | --- |
| HTTPS/cache/prepared/model recipes | PASS, 50.75 s; prepared/no-source, counted cold download, offline zero HTTP/TLS attempts, down/up retention, truncated/checksum-corrupt transfer and retry, corrupt/missing prepared file, tiny fixed configuration, deterministic native initialization failure, base restoration | [Cache log](<../../.local/p07/final-parent/cache-gpu.log>) |
| Genuine OpenAI SDK 2.11.0 | PASS, 20 cases, 12.33 s: ten approved encoded fixtures through JSON and text; includes OGG Vorbis/Opus, WebM/Opus and trailing-moov MP4 | [SDK log](<../../.local/p07/final-parent/sdk-codecs-gpu.log>) |
| Assertion sensitivity | PASS, 12.56 s: actual owned restart after success, retained file in API tmpfs, synthetic marker in actual emitted logs | [Sensitivity log](<../../.local/p07/final-parent/sensitivity-gpu.log>) |
| Concurrent public and private-backend comparison | PASS, 11.45 s: two waves of three public calls and 30 direct-backend calls; six scoped native GPU samples, peak SM 23%, framebuffer 541,065,216 bytes | [Comparison](<../../.local/p07/run/published-2382374257/resident-independent-comparison.json>) |
| Terminal recovery and actual resource observations | PASS, 28.13 s: invalid/decode failures, active cancellation/native crash and controls, reduced three-second operation timeout/control, original settings restoration | [Terminal log](<../../.local/p07/final-parent/terminal-observations-gpu.log>) |
| Historical bounded resource conditioning | FAIL to establish calibration: six blocks of 20 completed with zero request errors, but required RSS convergence was not established | [Observer-renewal workload log](<../../.local/p07/final-parent/measurement-observer-fixed-gpu.log>) |
| Model-I/O during that observer-renewal workload | PASS observation: the corrected observer remained available beyond 120 seconds and recorded zero recurring model-path operations | [Finalization receipt](<../../.local/p07/run/published-3268365144/resident-calibration-finalization.json>) |
| Reordered comparisons and post-work observation procedure | FAIL, 116.67 s: 80 conditioning successes, zero request errors; API idle RSS exceeded its frozen bound by 61,440 bytes. Native tail convergence improved, but no envelope or new measured-60 proof was established | [Changed-procedure log](<../../.local/p07/final-parent/measurement-postwork-order-gpu.log>) |
| Integrated selected-server regression | FAIL, 205.35 s: the three-second fault fixture completed legitimately in 2.987105 s after queue admission, so it did not discriminate a timeout. Other selected groups passed; the historical aggregate remains failed | [Integrated log](<../../.local/p07/final-parent/integrated-server-gpu.log>) |
| Renewed strict deadline and recovery | PASS, 68.16 s: all eight lifecycle cases and terminal observations passed with the stricter one-second fixture, unchanged canonical timeout/recovery assertions and original-settings restoration | [Renewal log](<../../.local/p07/final-parent/deadline-one-second-renewal-gpu.log>) |

Terminal observations have `calibrated_envelope: null` and
`qualification: not_established`. Successful recovery/resource observations do not
retroactively establish memory calibration. Unknown observations are not zeros.
The historical three-second timeout and renewed one-second timeout are explicitly
reduced test settings, not evidence of the default 600-second operation budget.

The log sensitivity fixture inserts a public synthetic canary through a permitted
request-ID field and proves the same privacy scanner detects it. It is not a native
executable mutation. CPU PROVE mutations separately disabled image-identity,
generation, overlap and privacy checks, demonstrated failure, then were restored.
The concurrent guard rejects two overlapping dispatches even if a third request
legitimately waited; one waiting request alone is insufficient evidence.

## Performance interpretation

The concurrent public median/p95 were 228.395/415.731 ms; observed queue wait
96.706/304.992 ms and private HTTP backend duration 87.5/196 ms. The 30 direct-backend
wall observations were 166.131/486.358 ms. Direct-backend wall includes the supervisor,
health/inference and two Docker exec/curl starts; candidate FFmpeg normalization was
outside timing. It is not pure native execution, and it was not subtracted from
public wall latency as API overhead. No numeric performance SLO is introduced.

The subsequent [true direct-native comparison](<../../.local/p07/run/published-1521887186/resident-native-comparison.json>)
passed 30/30 public speech oracles, with zero errors and the same resident process.
Five scoped GPU samples recorded peak SM 21% and framebuffer 551,550,976 bytes.
The native HTTP wrapper-wall median/p95 were 106.289/413.649 ms; these include one
Docker exec/curl start and full response consumption, not pure CUDA time. Both
backend/native series reused the same 352,044-byte canonical PCM WAV produced by
the exact candidate FFmpeg, SHA256
`d7d4e74b8a333ed02186008bc109a1b1a19d16da668bd56e785d80d69a16a72f`.
The source public WAV is distinct wire bytes; no additional source-codec coverage
is claimed. The standard curl multipart request used only backend loopback and the
existing read-only prepared bind, with no new host port or native implementation.
The earlier private artifact's unexported-encoder explanation was not a valid
feasibility barrier; it is preserved as history, not reused as a limitation.

## Resource failures and observation correction

All earlier failed runs remain failed and retained:

- 26.51 s: a cold/one-longest completed-state bound rejected the first ordinary
  block's approximately 5 MiB extra backend RSS; the aggregate did not separate
  Go supervisor from native memory.
- 42.81 s: all 20/40/60 endpoints and idle were collected before the original FAIL.
  Native RSS was identical at all three endpoints and later idle; supervisor RSS
  grew then plateaued. This was bounded observed retention, not universal proof.
- 61.55 s: a separate 30-request ordinary calibration still missed a later native
  anonymous-memory jump; only the third measured prefix failed.
- 127.15 s: the upfront six-block/120-request conditioning budget was exhausted.
  An independent two-minute Docker command limit also cut off the attached
  model-I/O observer. No zero-I/O claim was made for that run.
- 131.65 s: after correcting only the observer lifetime, model-I/O was available
  with zero operations, but the unchanged resource-conditioning procedure still
  did not establish calibration. The 120-request cap and memory limits were not
  increased to obtain a pass.
- 134.27 s: a fresh class-specific observation procedure still failed calibration
  after 120 successful requests. The final native RSS endpoints were 607,580,160,
  608,432,128 and 610,103,296 bytes: increasing by 2,523,136 bytes across the tail,
  above the independently frozen 28,672-byte native variability. Native file RSS
  stayed constant; anonymous memory grew. Model-I/O remained available with zero
  operations. Go GC also included one [2 MiB, 3 MiB) observation among 21
  [1 MiB, 2 MiB) observations. No envelope was frozen and no measured-60 claim
  is made for that run; neither noise nor request budgets were increased.
- 116.67 s: the already-required 30 backend and 30 direct-native comparisons ran
  before ordinary calibration in the same resident generation. Conditioning then
  stopped after 80 successful requests: the API cap was 18,272,256 bytes and the
  fourth block's idle RSS reached 18,333,696 bytes, an excess of 61,440 bytes.
  The final three native RSS endpoints spanned only 8,192 bytes, within the frozen
  28,672-byte variability, with an equal final pair. Combined backend peak stayed
  below its original isolated-longest physical cap; model-I/O was available with
  zero operations. These observations do not cancel the API failure. No API live
  heap was observed, and the original isolated-longest API transient peak was not
  retained before failure. Later failed-population peaks cannot replace the frozen
  bound. No envelope was frozen and the new measured-60 phase did not start.

The observer now inherits the remaining bounded acceptance lifetime, while normal
Docker commands retain their original short limit and process/output cleanup.
REQ-007/018 permit bounded non-model allocator housekeeping; RSS drift alone does
not identify live request copies. A subsequent explicitly instrumented diagnostic run also failed the unchanged
conditioning rule (128.23 s). Its actual backend runtime flag was verified as
`GODEBUG=gctrace=1,scavtrace=1`, without GC tuning or forced collection. The
[diagnostic receipt](<../../.local/p07/run/published-664717126/supervisor-gc-conditioning.json>)
contains 22 natural GC observations through uptime 125.573 s. Every marked Go
live-heap observation lay in the quantized interval [1 MiB, 2 MiB); there were also
22 scavenger observations. This is additional evidence consistent with retained
allocator/physical pages rather than full live request copies in the Go supervisor,
not a proof about native memory, the API heap or every sub-resolution leak. The
failed RSS verdict has not been relabeled. The printed live-heap value is rounded
down to whole MiB, not exact bytes or RSS; see the [pinned Go runtime source](https://go.googlesource.com/go/+/refs/tags/go1.27.3/src/runtime/mgc.go#1394).

## Repository checks so far

Locked Python formatting, Ruff and mypy passed for 40 source files. The tooling suite
passed (141 cases counted, including 12 explicit environment-dependent real-tool
skips); acceptance Python tests passed 16 cases. The first tooling run failed because the pinned
Trivy was not on PATH, then passed with the existing pinned 0.75.0 executable;
no scanner was installed or release security result relabeled.

The full acceptance Go race suite and vet passed after the initial class-specific
memory and one-second deadline corrections. Focused race/vet checks then passed
for the subsequent post-work GC collector; the owning main integration compiled
and executed in the 116.67-second failed GPU run. Final full regression renewal
remains pending. All six parallel user-file byte hashes were rechecked and still
match their saved baseline; the Git index is empty and HEAD remains the
documentation checkpoint.

## Remaining actions

Specification review/approval and the exact P07 v2 agreement are complete; do not
repeat them for unchanged inputs. The P07 implementation agent owns the remaining
API-memory classification, calibrated steady/failure proof and final regressions.
Do not add fitted threshold padding, increase the 120-inference-request
conditioning budget, change the published candidate or relabel historical failed
runs. Apply only the separately accepted v3 replacement of the API absolute-RSS
gate with independently frozen natural-GC marked-live evidence; all other
mandatory gates and explicit measurement limitations remain.

The 2026-10-28 continuation renewed the unchanged memory check: FAIL in 74.85 s,
20 conditioning successes / zero request errors, zero recurring model-path I/O.
The independently frozen API limit was 18,296,832 bytes; a later idle value was
18,309,120 bytes (12,288 bytes above it). No envelope was frozen and the measured
sixty-request phase did not start. Private evidence is
[the retained run](<../../.local/p07/run/published-3759108690/resident-calibration-finalization.json>)
and [command log](<../../.local/p07/resume-20261028/memory-baseline.log>).

The required client path now runs only the genuine SDK codec matrix. Its
process-boundary regression failed with the prior Electron dispatch (RED), passed
after its removal, detected a one-line restored Desktop dispatch (PROVE), and
passed again after restoration under race. No Electron application was executed.
The existing parser for historical Electron receipts does not schedule a process.

An opt-in `P07_API_GC_TRACE=1` supplement now collects actual scoped API Go numeric
GC observations without GC tuning/forced collection or any change to resource
bounds. An actual subprocess test detected missing runtime propagation and
unwanted always-enabled diagnostics; those defects were corrected/restored and
focused race checks passed. This is diagnostic evidence only, not qualification.
The first renewed full helper race run passed in 177.332 s before these changes;
focused post-change race checks passed in 25.604 s. Final full renewal is pending.
The initial local-toolchain attempt failed because the shim selected Go 1.26.5;
subsequent commands explicitly use the actual Go 1.27.3 bin directory.

The API diagnostic run also remained FAIL (165.28 s; 80 successful conditioning
requests, zero request errors and recurring model-path operations). Its 26 natural
API collections all reported marked live heap in [1 MiB, 2 MiB), with verified
numeric-only runtime flags, no GOGC/GOMEMLIMIT override and no forced collection.
This supports bounded coarse live-heap observations, not proof of every retained
copy or a calibrated PASS. See the
[API diagnostic](<../../.local/p07/run/published-2822728049/api-gc-conditioning.json>)
and [unchanged memory verdict](<../../.local/p07/run/published-2822728049/resident-calibration-finalization.json>).

A measurement-source defect was identified independently of those failed values:
Linux documents `status/VmRSS` as approximate because of scalability optimizations
and recommends `smaps`/`smaps_rollup` for accurate RSS
([man-pages 6.19](https://man7.org/linux/man-pages/man5/proc_pid_statm.5.html)).
The API observer now requires PID 1 `smaps_rollup/Rss`, rejects missing, duplicate,
invalid-unit and overflowing observations, and has no VmRSS fallback. Backend
aggregate/breakdown status counters remain approximate and otherwise unchanged.
The RSS parser/owned-live-process checks failed for the previous counter (RED),
passed for the precise counter, rejected a one-line return to VmRSS (PROVE), then
passed again with race after restoration. Envelope formulas, resource limits,
conditioning budget and mandatory workloads are unchanged. New runs independently
calibrate the corrected source; historical failed runs/bounds are not relabeled.
An isolated-longest peak is now retained before possible calibration failure,
without using measured/failed-population peaks to refit any bound.

The corrected-counter v2 run remained FAIL (139.70 s): 100 conditioning successes,
zero request errors/recurring model-path operations, resource-tail convergence but
incomplete passive heap proof. API RSS reached 18,526,208 bytes versus the frozen
18,460,672-byte bound during passive observation. See
[the finalization](<../../.local/p07/run/published-2406037419/resident-calibration-finalization.json>)
and [passive observations](<../../.local/p07/run/published-2406037419/supervisor-heap-calibration.json>).
This disproves neither a leak nor bounded housekeeping; precise RSS alone did not
establish qualification. No measured sixty-request phase or calibrated terminal
pass is claimed.

The renewed unfiltered `TestClientCompatibility` passed all 20 genuine SDK
JSON/text codec cases in 13.91 s on the same exact published rc.5 references;
no Electron process was scheduled. See
[the full command result](<../../.local/p07/resume-20261028/sdk-matrix.log>).

After disclosure of the precise-RSS failure and proposed method/limits, the user
explicitly accepted v3. It generalizes only the existing private Go-heap collector
for real API/supervisor identities, freezes premeasurement live intervals,
requires fresh comparable natural cycles and retains precise API RSS plus
cross-block growth checks. If needed, at most 4096 sequential public health GETs
per observation phase may induce ordinary allocations under the unchanged caller
deadline, identically in calibration and measurement, without inference, forced
GC, tuning or fabricated observations. Quantization remains explicit and missing
cycles remain incomplete. Other FD/files/model/native/GPU/isolation/terminal
checks, the 120-inference-request cap and measured 3x20 remain mandatory. This is
an explicitly changed technical method, not retroactive acceptance of v2 failures.

Independent continuation regressions now passed for every unchanged application
and integration Go package with race, plus their go vet. The initial models fixture
failure was traced to the log-protection umask077 changing a test-created0444
fixture to0400; the focused and full models race reruns with normal umask022
passed, without a code/test waiver. Current acceptance Go regression renewal is
still pending the v3 implementation. See
[application results](<../../.local/p07/resume-20261028/go-application-regressions.log>)
and [models/vet recovery](<../../.local/p07/resume-20261028/go-models-recovery-and-vet.log>).

Pinned uv0.13.5/Python3.14.9 checks passed: Ruff format/lint and mypy (40 files),
tooling141 tests with12 explicitly optional C02/native/Helm fixture skips, and
all16 acceptance Python cases. The first tooling run failed because Trivy was
not on the default PATH; the existing binary was verified against the recorded
SHA256/version0.75.0 and supplied through C01_TRIVY_BINARY. No check was disabled,
no tool was installed, and no fresh vulnerability-DB/release-security conclusion
is inferred. See [initial checks](<../../.local/p07/resume-20261028/python-checks.log>)
and [successful full Python suites](<../../.local/p07/resume-20261028/python-regressions-fixed-env.log>).

The first actual v3 run also remained FAIL (162.82 s): all 120 conditioning
requests succeeded, with zero recurring model-path operations, but native resource
tail convergence was not established. Post-work heap calibration, measured3x20 and
calibrated terminal phases did not start; see
[the v3 finalization](<../../.local/p07/run/published-1554390448/resident-calibration-finalization.json>)
and [command result](<../../.local/p07/resume-20261028/memory-v3.log>).

The same documented approximate-counter issue was still present in backend/native
`status/VmRSS` and `RssAnon`. The shared counter producer now requires precise
rollup Rss/Anonymous for those processes as well; aggregate totals and component
RSS use the same source. Only status/RssFile remains explicitly approximate; it
is not relabeled PSS or RSS-minus-anonymous. A duplicated parser was removed.
A consumed-counter regression failed for the old producer (RED), passed after
correction, rejected a restored actual VmRSS fallback (PROVE), and passed after
restoration. Failed readers cannot become zero or approximate substitutes. A real
owned-process probe exercises the producer and consumer together. Current parent
acceptance CPU passed5.711s, affected race1.058s and vet passed. No native cap,
formula, measured/inference count or conditioning budget was increased.

The corrected-process-counter run still FAILed (159.82s):120 conditioning successes,
zero errors/model-path operations, no resource convergence. Between80 and120
requests, native exact RSS and Anonymous both increased339,968bytes (332KiB),
against independently measured variation28,672bytes (28KiB); File remained constant.
This is real physical anonymous growth, but RSS alone does **not** establish a
live-object leak. The earlier counter defect was real and was not a sufficient
explanation. See [corrected finalization](<../../.local/p07/run/published-1802005645/resident-calibration-finalization.json>).

### Deeper attribution requested by the user

The user asked whether the cause might be outside our Go implementation. This did
not approve extra observer capabilities, native changes or weaker qualification.
The diagnostic reuses the published constructor, existing model tracer, canonical
normalization, speech oracle, process identity checks and fixed budgets. Matching
preparation uses longest1/direct-backend30/direct-native30/ordinary-public30;
target measurements then use at most6x20 requests. The idle control sends zero
foreground inference. All receipts stay `qualification: not_established` and do
not replace or repair the failed qualification run.

| Target route | Result | Native RSS/Anonymous change | Evidence |
| --- | --- | --- | --- |
| Direct native loopback, bypassing both Go inference handlers |120 valid requests,0 errors;130.43s |+348KiB across requests20→120/~69s |[native](<../../.local/p07/run/published-3461331781/native-attribution-native.json>) |
| Public API, matched fresh cohort |120 valid requests,0 errors;138.10s |+312KiB across requests20→120/~70s |[API](<../../.local/p07/run/published-2785743987/native-attribution-api.json>) |
| Background supervision only after the same preparation |0 target inference;128.60s |+4KiB across first→last window/~62s |[idle](<../../.local/p07/run/published-1857965968/native-attribution-idle.json>) |

File counters and GPU568,328,192bytes remained constant; no recurring model-path
operations were observed. Therefore our Go **inference routing is not necessary**
for the growth, and background alone did not reproduce its magnitude. This does
not yet distinguish normal native/CUDA/allocator caching from retained live
objects. Direct curl uses new connections while Go reuses them; the supervision
background remains present, so avoid broader causal claims.

An earlier API diagnostic failed after21 successes/1 error (62.74s), with the old
receipt lacking actual status/transport/speech detail. It remains unexplained,
not fixed or converted to PASS; [original failed receipt](<../../.local/p07/run/published-1548481271/native-attribution-api.json>).
The subsequent diagnostic records nullable actual failure details using only
allowlisted speech facts/hash, never transcript content. The failure did not
recur in the current120-request API cohort. Current acceptance CPU passed5.596s
and vet passed after these diagnostic-only additions.

The pinned native archive/source checksums were verified against build inputs;
no upstream files changed. Its handler resets parameters and owns audio/PCM
objects locally, and `no_context` defaults true. Those source facts are not a
runtime liveness proof. In particular, its ggml CUDA graph cache **already** evicts
unused graphs (10s age,5s sweep); an older unrelated llama RPC report cannot be
used as our diagnosis. Old [whisper server2605](https://github.com/ggml-org/whisper.cpp/issues/2605)
and [llama RPC20315](https://github.com/ggml-org/llama.cpp/issues/20315) were inspected
as context, not treated as proof of our exact version/problem.

### Read-only mapping localization

A numeric-only procfs observer now records mapping class/permissions and
Size/RSS/Anonymous/LazyFree/PrivateDirty, with hashed ranges rather than addresses.
No pathname or memory content is emitted. Unknown, malformed or missing counters
fail; a removed LazyFree-presence check was caught by PROVE and restored. Current
CPU acceptance passed5.723s; affected mapping race1.031s, native-response race1.300s
and vet passed. This diagnostic does not change qualification decisions.

The mapped native cohort stopped on its83rd request after82 valid responses. Its
four earlier windows (requests20→80) show+479,232bytes (468KiB) RSS, Anonymous and
PrivateDirty; LazyFree remained zero. **All** that increase falls within29 fixed
RW/PROT_NONE anonymous reservation pairs whose combined sizes are64MiB. Their
count remained29. This shape is consistent with non-main glibc heap arenas, not
proof of live allocations or their absence. Boundary changes can change a hashed
range identity; apparent removed/new rows are not themselves proof of unmap/remap.
See [numeric derived summary](<../../.local/p07/resume-20261028/mapping-summary-reservations.json>)
and [original mapped receipt](<../../.local/p07/run/published-2926664583/native-attribution-native.json>).

The exact backend image reports Ubuntu GLIBC2.39-0ubuntu8.3. Its matching
[2.39 allocator documentation](https://sourceware.org/glibc/manual/2.39/html_node/Memory-Allocation-Tunables.html)
describes dynamic mmap/trim thresholds and per-thread caches; the matching
[arena implementation](https://raw.githubusercontent.com/bminor/glibc/glibc-2.39/malloc/arena.c)
reserves heaps with a writable prefix and inaccessible remainder. This explains
why residency is not equivalent to live allocation, but does not establish the
exact runtime cause. Context7 was attempted and unavailable due to quota; these
version-matched primary sources were used instead. No documented tunable was set.
No extra VMA decoration or mapping-name mode was enabled.

The83rd response was complete HTTP200/valid JSON with148 transcript characters,
but failed speech recognizability: Fellow/Americans remained present while the
independent clause/action cues were absent. Only allowlisted flags/hash were
retained, never transcript content. Native failure now preserves actual status,
stage and safe speech facts before returning the same error and stopping the
owned target. A real-curl RED regression reproduced the previous loss of failure
facts; the corrected consumer and fail-closed teardown passed. The isolated
model observer consequently exited137 after teardown, so no model-I/O absence
proof is claimed for this failed cohort. The earlier mapped cohort also failed
and supplied only one mapping window; neither failure is converted to PASS.

### Final uninstrumented functional regression

The selected implemented `TestResidentSuite` branches completed FAIL144.74s;
[full command result](<../../.local/p07/resume-20261028/final-functional-rc5.log>).
Baseline60 requests, public codecs/options/rejections, input bounds/isolation,
FIFO/queued cancellation/deadline, both API shutdown modes, sensitivity controls,
terminal observations, model observer and no-GPU fail-closed checks passed.
Terminal observations without an independently established envelope still do not
qualify calibrated terminal memory.

Three branches remained failed:

- Lifecycle active operation deadline: the full Docker observation could not
  complete inside the operation budget (`context deadline exceeded`), so actual
  Busy was unavailable. This is not yet a demonstrated application defect.
- Cache/models: the required explicit verified Docker-bridge source bind IP was
  missing from this invocation. This is an unfulfilled test prerequisite, not
  evidence that cache behavior failed.
- Uninstrumented performance: native speech recognizability failed on the9th
  sample after8 valid native samples. Actual response HTTP200/valid JSON/65chars,
  but independent clause/action cues were absent, as in the separate native
  diagnostic failure. Safe flags/hash retained; owned target down succeeded;
  [actual native receipt](<../../.local/p07/run/published-513283645/resident-native-comparison.json>).
  No complete timing-component PASS is claimed, nor is the diagnostic profile
  substituted for this required entry.

### Corrections and affected reruns

The Busy failure was an observer defect: full serial resource sampling cannot
reliably fit the one-second active-state window. The call now binds its owned API
container and original native kernel start identity **before** submission. The
short barrier reads actual private health plus the same live host/namespace PID,
checks generation/model/protocol/fingerprint/CUDA, and leaves resource fields
unknown. Full ready/terminal resource checks and the independently armed scoped
GPU/replacement watcher remain mandatory; old resource samples are not relabeled
fresh. No operation or observation budget was increased. A CPU regression through
the real wait barrier passed; restoring the full observer reproduced the exact
context-deadline failure (PROVE), then restoration passed. Changed process start,
namespace, generation, model, configuration, protocol and CPU-only claims are
rejected. Full acceptance CPU5.339s, affected race1.035s and vet passed.

Docker network metadata and the actual host bridge interface verified the test
source bind address, then the existing test consumed it explicitly. No network
configuration was changed; the private address is not a new default.

The affected actual rc.5 rerun **PASSed93.53s**:
[lifecycle/cache result](<../../.local/p07/resume-20261028/final-lifecycle-cache-fixed.log>).
All8 lifecycle cases and all12 cache/model cases passed. The reduced one-second
operation produced actual HTTP408/operation_timeout without transport error, with
observed Busy in the same generation. Offline cache reuse, retention, source deny
traps, corrupt/missing prepared fail-closed and selected tiny/base model changes
were exercised. These are new successful runs, not retroactive repair of the
previous failed invocation.

Other affected Busy consumers also **PASSed41.34s**:
[API shutdown/terminal observations](<../../.local/p07/resume-20261028/final-busy-callers.log>).
Both API stop modes and all observational terminal paths retained their guards;
this still does not establish a calibrated memory envelope.

P07 remains open. Native live-allocation stability and repeated native speech
failures in the uninstrumented comparison remain unresolved; no retry-until-PASS,
weaker oracle, larger conditioning budget or native/image change was performed.
Parent's next action is to consolidate final evidence/freshness and identify the
exact still-blocking qualification inputs without asserting a native leak or an
unsupported PASS. Qualification criteria, image bytes, budgets and application privileges remain
unchanged. No forced GC, allocator trim, new BPF capability, native fork or profiler
installation has been performed.

The session's earlier restricted file sandbox hid NVIDIA device nodes. A scoped
read-only query in the exact candidate container succeeded, and an approved host
query confirmed driver/NVML availability. The user then selected unrestricted file
access; no driver, host configuration or application image was changed. The
unneeded alternative-observer test was removed before implementing a replacement.

Completion metadata stays open until every required gate and renewed MCP
freshness check passes. No commit, push, registry publication, cluster action or
operator-cache deletion was performed. Next owner/action: the P07 implementation
agent implements and verifies the explicitly agreed v3 Go-heap/RSS-trend method,
then obtains fresh steady/terminal evidence and final regressions. Retain every
failed memory verdict and leave P07 incomplete until the new mandatory evidence
and final MCP checks pass.
