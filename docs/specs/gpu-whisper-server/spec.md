# GPU Whisper Server — First Release

Status: Approved

Depth: Deep
Active profiles: core, backend, operations, security

This specification defines V1 as one deployable transcription service: a separate public API calling the unchanged OpenWhispr/whisper.cpp HTTP server, packaged as separate Docker images and deployed by one Helm chart on NVIDIA-enabled Kubernetes. V1 has one configured GPU-resident model, transcription and health only. Other memory modes and model-management controls are explicitly retained as future work, not cancelled. This document authorizes specification work only.

The decision journal is the authoritative source-to-requirement link map. The latest user amendment narrows the earlier combined contract for V1 without deleting deferred requirements. The confirmed Deep depth, four active profiles, and three planning-artifact names are retained; the public API and inference backend are separately packaged components. Configurable bounds and the bootstrap model are recorded assumptions, not performance guarantees. Only a read-only local GPU identity query was run; no image build, GPU transcription, benchmark, or Kubernetes deployment has been performed during specification authoring.

## Evidence and terminology

The inspected upstream revision is `6ac14546046ffb17d144a08bcad65337e535cf5`. This identifies research evidence; each implementation release must record the exact upstream revision it packages.

Verified source facts:

- The upstream HTTP example loads one model on startup and has a model replacement route. It has no model unload route or API authentication check. See [server initialization](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/examples/server/server.cpp#L717-L731) and [load/health routes](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/examples/server/server.cpp#L1163-L1215).
- The unchanged inference handler retains its latest result in the reusable backend context after sending the response. Old results are cleared at the beginning of the next inference invocation. This source fact does not prove runtime memory stability or response isolation. See [response completion](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/examples/server/server.cpp#L1152-L1162) and [result reset](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/src/whisper.cpp#L6813-L6829).
- The public API supports initialization from a memory buffer and freeing a complete context. See [public API](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/include/whisper.h#L203-L271) and [context release](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/src/whisper.cpp#L3861-L3875). This does not establish actual runtime memory reclamation.
- The upstream Dockerfile uses CUDA 13.0 and a specific architecture list, while the backend supports a wider architecture set with suitable toolchains. See [CUDA Dockerfile](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/.devops/main-cuda.Dockerfile#L1-L28), [backend architecture selection](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/ggml/src/ggml-cuda/CMakeLists.txt#L8-L55), and [CUDA 13 architecture restrictions](https://docs.nvidia.com/cuda/archive/13.0.0/cuda-toolkit-release-notes/index.html#deprecated-architectures).

`RAM` means host memory. `VRAM` means GPU memory. A memory location describes model-owned allocations; a GPU-resident model may also require host allocations. Disk-cached model files are separate from RAM/VRAM residency. A service instance is one logical serving unit consisting of the separate API component and its dedicated Whisper backend. The API owns that instance's queue, while its backend owns the resident model context; neither is global across instances. The user selected Go with Echo for the public API and separate Kubernetes Pods for API and backend under REQ-001 and REQ-016. Controller selection, pairing mechanics, and private connectivity remain planning decisions within those constraints.

## Product and platform contract

### REQ-001: Resident-only packaged V1

V1 must deliver an unmodified OpenWhispr/whisper.cpp HTTP server in its own Docker image, a separate public API service with its own container image, one working Helm chart deploying both components, and documented standalone Docker operation. The public API service calls the Whisper-server HTTP API; it does not embed the inference library or require changes to the upstream C++ server. Build configuration, architecture selection, runtime dependencies, and container packaging may be adapted without changing server source or inference algorithms.

The immediate outcome is synchronous transcription using one GPU-resident model. The public API is implemented in Go with Echo. In Kubernetes, the API and its dedicated Whisper backend run in separate Pods, with private connectivity preserving the per-instance ownership and access boundaries. Documentation covers configuration, building, installation, verification, measured performance, upgrade, rollback, and removal. Framework/toolchain version pins, controller selection, pairing mechanics, and other implementation details remain planning decisions.

Acceptance: the public API serves real CUDA transcriptions through the packaged, unchanged backend in standalone Docker and the required Kubernetes environment. A chart scaffold, a public API mock, a direct library implementation instead of the agreed HTTP boundary, or an undocumented upstream C++ patch does not satisfy V1.

### REQ-002: Platforms, architectures, and required GPU targets

Build/runtime target Linux amd64. Building either image must not require a GPU. Runtime requires NVIDIA driver/container integration compatible with the selected CUDA toolchain. No ARM, Windows/macOS containers, CPU-only fallback, or distributed multi-GPU inference is required.

Architecture coverage must preserve the packaged source revision's CUDA backend coverage rather than copy a narrower Dockerfile list. Source-compatible CUDA/image variants are permitted with a published architecture/toolchain/driver mapping. In particular, the release must support RTX 2060 and RTX 5070 Ti Laptop GPU. NVIDIA lists RTX 2060 under compute capability 7.5 and RTX 5070 Ti under 12.0: see [NVIDIA CUDA GPUs](https://developer.nvidia.com/cuda/gpus). GPU support does not guarantee that every model fits available VRAM.

Two concrete runtime verifications are mandatory: standalone Docker on the current machine with RTX 5070 Ti Laptop GPU, and Helm deployment on the user's Home Lab Kubernetes cluster with RTX 2060. The local GPU identity was verified by a read-only `nvidia-smi` query; inference was not run. Exact GPU memory, driver, OS, runtime, Kubernetes, and Helm versions are recorded when testing each environment.

The initial generic Kubernetes matrix remains maintained branches 1.35, 1.36, and 1.37, with compatible Helm 3 and Helm 4 reference minors 3.22 and 4.3. The explicitly requested Home Lab target must also be qualified at its actual version; its presently unknown version cannot be silently assumed to match or excluded by the generic matrix. Release evidence records exact patches and the resulting support matrix. See [Kubernetes releases](https://kubernetes.io/releases/), [Helm 3 compatibility](https://helm.sh/docs/v3/topics/version_skew/), and [Helm 4 compatibility](https://helm.sh/docs/topics/version_skew/).

The existing cluster supplies NVIDIA drivers, container integration, and device plugin. Ingress controller/storage provisioner are prerequisites when enabled. The chart does not provision them.

Acceptance: both named GPUs execute actual transcription via CUDA, with source/toolchain/driver/architecture and environment identities recorded. GPU visibility, successful compilation, an unavailable test target, and CPU execution cannot be reported as a passed GPU test. Generic chart compatibility and actual Home Lab compatibility are reported separately.

### REQ-003: Configuration and operating defaults

Docker parameters/environment/configuration and Helm values expose equivalent model catalog, download set, exactly one served startup model, artifact sources/cache, GPU selection, addresses/ports, queue/input/deadline limits, and provisioning/shutdown limits. Helm additionally exposes both image identities, scheduling/resources, storage, Service, and Ingress.

Model IDs are unique nonempty ASCII identifiers of letters/digits/dot/underscore/hyphen, beginning with a letter/digit and at most 128 characters. Model sets reference declared catalog entries. Exactly one model is served in V1; configuring multiple served models, `ram-cache`, `on-demand`, per-model serving policies, or HTTP warmup/unload is rejected as unavailable V1 behavior rather than silently ignored. An explicit catalog replaces the bootstrap catalog. Requests cannot supply URLs/paths or change configuration.

Docker precedence is command-line parameters, then environment, then declarative configuration, then defaults; collection overrides replace the collection. Helm applies its normal values precedence and produces the same validated service contract. Changes require restart/Helm upgrade. Unknown settings, malformed values, duplicate IDs, inconsistent model sources/sets, and invalid bounds fail before serving.

The reversible bootstrap default is multilingual upstream `base` from an official release-pinned source. V1 policy is `resident`; loading the startup model is mandatory, without a synthetic transcription. The public API port defaults to 8080. The backend uses a separately configurable private address/port and is not publicly exposed by the release.

Assumed configurable bounds, retained from the original specification:

| Setting | Default | Validation |
| --- | --- | --- |
| Active GPU operations per instance | 1 | V1 invariant |
| Waiting operation capacity | 16 | Integer >= 0; excludes active work |
| Uploaded audio payload | 25 MiB | Positive |
| Prompt size | 8 KiB | Positive UTF-8 byte bound |
| Decoded audio duration | 900 seconds | Positive, bounded decoding |
| Operation deadline | 600 seconds | Positive; queue plus execution |
| Cleanup deadline | 10 seconds | Positive |
| Graceful shutdown deadline | 30 seconds | Positive; orchestration also accommodates cleanup |
| Model provisioning deadline | 1800 seconds | Positive total provisioning bound |
| Download attempts | 3 | Positive integer; fit provisioning bound |

These are resource/recovery bounds, not measured latency or throughput guarantees. Their adjustment cannot weaken GPU execution, isolation, or cleanup obligations.

Acceptance: equivalent inputs produce the same startup model and limits; overrides do not retain an unintended bootstrap entry. Every invalid configuration, including deferred mode selection, gives a bounded configuration failure rather than a partly ready service.

## Model artifacts and lifecycle

### REQ-004: Configured model artifacts

The catalog/download set remains configurable independently of the single served startup model. Operators may download additional declared models without exposing them. The served model may use a release-pinned public HTTPS download or an operator-prepared file in mounted storage. A request's `model` field identifies the configured startup model; another ID never causes loading or switching.

Provisioning prepares the selected download set before startup readiness and reuses valid cache entries. Existing-file mode works offline. Sources change only through configuration.

Acceptance: one declared model is actually loaded and served; download-only or other IDs are rejected. A valid prepopulated cache permits offline restart with no model-source HTTP traffic. A missing prepared file fails provisioning instead of triggering an unexpected download or substituting another model.

### REQ-005: Cache integrity and ownership

Downloaded artifacts require pinned identity and expected checksum; optional checksums on prepared files are enforced when present. Only a complete verified download becomes a reusable entry. Interrupted, truncated, corrupt, or checksum-failing output is not served.

Bounded provisioning cleans incomplete output, preserves previously valid artifacts, and prevents shared writers from publishing partial/corrupted entries. Prepared files are read-only to the service. Shutdown and request cleanup never delete cached model files.

Acceptance: interrupted/checksum-failing downloads leave no reusable partial files or accumulating incomplete output; a corrected retry recovers. Cache identity/checksum survives requests, process restart, rollout, and rollback. Offline/cache-hit behavior is verified at the source access boundary rather than by a success log alone.

### REQ-006: One fixed model per instance

Each instance has one configured served model and one inference backend. It loads the model during startup and keeps it through normal successful requests. The public request field selects only this already configured ID; no per-request switching or upstream `/load` operation is exposed.

Changing the served model requires configuration and a controlled restart/upgrade. Failure to load the replacement prevents readiness and cannot produce a mislabeled transcript from another model. Different replicas have independent models/state; a replica serving a given release must use that release's configured model.

Acceptance: repeated requests do not reload or change the model. A different model ID is rejected before inference and leaves the current model untouched. A failed startup replacement is unready; restoring valid configuration and restarting recovers.

### REQ-007: Resident lifecycle in V1

The configured model is GPU-resident after successful startup. Necessary host allocations may also remain. A normal completed transcription releases API-owned request resources and transient files while retaining reusable backend/model state. The unchanged backend may retain bounded internal working buffers and its latest transcription result in that context until reuse or backend/process shutdown. V1 does not require immediate erasure or zeroization of all backend audio-derived, decoder, or output state after each response, nor a normal per-request model reload/restart. Retained internal state must not influence another request's result under REQ-013 or accumulate without bound under REQ-018. Recoverable failure/cancellation may retain valid model state; a corrupt/unusable backend must be stopped and recovered rather than reused.

The instance releases residency on backend/process shutdown, terminal failure, or configuration replacement. V1 does not promise idle GPU release after every request. `ram-cache` and `on-demand` are required future work under REQ-022; they are unavailable in V1.

Acceptance: real repeated CUDA requests reuse loaded model state without recurring model-file reads or model initialization. Stable model/context allocations and a latest internal result are permitted; accumulating request/model copies and cross-request influence are excluded by REQ-013 and REQ-018. API-owned request resources and transient files are released on terminal paths. An unsafe backend follows bounded stop/restart recovery instead of returning a false success.

### REQ-008: Startup load and deferred automatic warmup controls

V1 automatically loads the fixed model on startup before readiness, using the existing server startup load. It performs no synthetic transcription. There is no optional per-model RAM/VRAM warmup configuration in V1.

Future automatic warmup retains the previously agreed contract: per-model target `ram` or `vram`, disabled by default; weights loading only; catalog-order execution; one managed model, so only the last remains resident; completion before readiness; failed configured warmup prevents readiness with a model-specific diagnostic. These controls are deferred with the other memory modes.

Acceptance: V1 becomes ready only after its actual model load; probes cause no additional load or inference. Deferred automatic warmup settings are rejected. Future contract coverage is explicit and is not claimed as a delivered V1 feature.

### REQ-009: Deferred HTTP warmup

`POST /v1/models/{model}/warmup` is future work and returns 404 in V1. Its retained future contract accepts required JSON target `ram` or `vram` for a served model, rejects missing/invalid/unknown fields with 400 without mutation, and serializes with transcription and model mutations.

RAM warmup retains reusable weights without model GPU allocation. VRAM warmup retains the GPU model context and needed host allocations. Neither runs transcription. Repeated same-model/target calls are idempotent and do not duplicate allocations. A successful response acknowledges completed loading with JSON `model`, `state` (`ram`/`vram`), and configured `mode`.

Warmup may temporarily override `on-demand` until the next actual processing applies configured cleanup, another model is selected, explicit unload, or shutdown. Validation rejection or waiting timeout/cancellation does not end the override. Unserved IDs return 404 without evicting another model; GPU-load failure cannot acknowledge VRAM residency.

Acceptance: V1 route inspection and a call demonstrate absence (404), with resident state unchanged. Future completion requires both residency targets, persistence until the stated transitions, no test inference, and allocation-based idempotency evidence.

### REQ-010: Deferred HTTP unload

`POST /v1/models/{model}/unload` is future work and returns 404 in V1. The retained future contract accepts an empty body/object, rejects unknown fields, serializes behind active work, and releases the selected model's RAM and GPU model/inference allocations without deleting files, changing policy, removing allowed IDs, or interrupting active inference.

Repeated unload and already-unloaded served IDs return 200 with `model`, `state: "unloaded"`, and `mode`. Unloading another ID leaves current residency untouched; unserved IDs return 404 without mutation. Success means allocation release is complete, not merely queued. A later request may load the model again without restart.

Acceptance: V1 route calls return 404 without stopping/reloading the backend. Future acceptance requires serialized, completed reclamation and repeated-call behavior, disk preservation, and successful later reuse.

### REQ-011: Transcription queue, deadlines, and cancellation

The API serializes valid transcriptions in a FIFO queue with one active GPU operation and the REQ-003 waiting capacity. Validation failures do not enter the queue or affect residency. Overflow returns 429 with a cause code and disclosure that work did not execute and may be retried. Zero waiting capacity permits one idle operation and rejects concurrent work.

The deadline starts at admission and includes waiting and backend execution. Waiting expiry returns 408 when deliverable and never forwards work. Active expiry/client cancellation stops work safely, cleans request resources, and prevents overlapping reuse of the backend. Aborting the proxy connection alone is not evidence that upstream inference stopped: the next request cannot run against uncertain active state. If safe recovery exceeds cleanup bounds, the instance becomes unready and stops/restarts the backend/instance.

Unobserved disconnect cannot extend work beyond deadline. Process/network loss may prevent an error response. Repeated transcription has no retained business side effect; exactly-once delivery and silent retries of completed inference are not promised.

Acceptance: concurrency observes FIFO and no overlapping GPU inference. Overflow/waiting timeout are not forwarded. Active cancellation/timeout recovers or takes the explicit unready/restart path; no resources are freed in use and no stale operation runs concurrently with the next request.

## HTTP and state contract

### REQ-012: Public synchronous transcription API

The separate API exposes `POST /v1/audio/transcriptions` using multipart/form-data with exactly one `file` and one `model`. Model must equal the configured served ID (bootstrap example `base`). Another ID is rejected with 404, without fallback, path loading, download, or switching. The API adapts the upstream route/fields rather than claiming that its request model field selects a different upstream model.

Optional fields are `language` (supported upstream language identifier or `auto`, default `auto`), UTF-8 `prompt` under the configurable byte bound, finite `temperature` in [0,1] (default 0), and `response_format` `json`/`text` (default `json`). `stream=false` is accepted; `stream=true`, duplicates, unsupported fields/formats, invalid values, and incompatible language/model choices are rejected with 400.

Inputs include decodable audio in WAV, MP3, MP4, MPEG, MPGA, M4A, OGG, FLAC, and WebM, including WebM/Opus and OGG/Opus. See [official file contract](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create). Content validation/normalization must not rely only on filename/MIME. Unsupported/corrupt/no-audio content returns 400; exceeding the audio payload bound returns 413; duration is checked by bounded decoding. A file at the configured bound must fit multipart framing. Upload/conversion cannot consume unbounded RAM or transient storage before rejection.

Acceptance: intended OpenAI-compatible clients, including OpenWhispr, transcribe all advertised file families through the public API and actual resident CUDA backend. Missing/duplicate fields, a different model, unsupported streaming, invalid values, oversized or overlong/corrupt audio reject without unintended inference or model reload.

### REQ-013: Results, errors, private backend, and isolation

JSON success is 200 application/json with `{"text":"..."}`; text success is 200 UTF-8 text/plain. Errors use JSON `{"error":{"message":"...","type":"...","param":null,"code":"..."}}`, setting `param` to a relevant public field when applicable. Cause codes are distinct; bounded messages expose no paths, secrets, audio, prompt, or transcript.

Status mapping: 400 invalid input, 404 other/unserved model or unknown/deferred route, 413 payload limit, 429 full queue, 408 operation deadline, 503 unavailable/not-loaded/backend/GPU/provisioning/shutdown, and 500 unrecoverable in-process API failure. Backend HTTP failures, process exit, and lost connections cannot be converted into successful transcripts. Process/driver death may lose the response and follows recovery.

The supported API is the synchronous transcription subset plus health. No model listing, translation, streaming, raw-path `/load`, or lifecycle extensions are exposed in V1. Clients must reach the public API boundary; backend HTTP endpoints must be private and cannot bypass configured model/input/privacy controls through release-published access.

Each response contains only its request's result. Resident reuse cannot carry another request's prompt, language, transcript, decoder history, or output into it. Recoverable malformed requests leave subsequent valid work usable.

Acceptance: clients parse adapted responses/errors; distinct audio/language/prompt sequences demonstrate isolation. Backend errors and death give the declared failure/recovery rather than false 200. Direct/raw path/deferred-route access is denied or unreachable at the client boundary; public unknown routes return 404.

### REQ-014: Health and readiness

`GET /health/live` returns 200 JSON `{"status":"live"}` for a responsive API process independently of the inference queue. `GET /health/ready` returns 200 JSON `{"status":"ready"}` only after configuration/provisioning, compatible GPU backend startup, and the fixed model's successful load. Otherwise it returns 503 with a bounded reason or is unreachable before listening.

A responsive public API with a dead/unloaded/starting backend is not ready. Dependency/GPU loss, shutdown, and unsafe cancellation prevent readiness. Probe calls never load or transcribe a model and do not wait behind long inference. Readiness does not prove every model fits the GPU or establish performance.

Acceptance: both probes remain responsive during inference. Repeated probes cause no model reads/load/inference. Delayed/failed model load, backend process death, incompatible GPU, and shutdown are unready. GPU listing alone does not substitute for release transcription evidence.

## Packaging, operations, and privacy

### REQ-015: Two container images and standalone operation

The backend image packages unchanged pinned upstream server source with compatible CUDA and required audio-decoding dependencies. Both images record immutable build/source/base-image/dependency identities; backend metadata includes upstream/toolchain/runtime identity. Model selection/artifact changes do not require rebuilding executable images.

Standalone Docker documentation starts the separate API and backend with their private HTTP connectivity, mounted cache/prepared model, selected GPU, public port publication, and termination handling. Both services run without root/admin privileges; writes are confined to configured cache/transient storage. API needs no GPU allocation of its own. Diagnostics use bounded stdout/stderr and meaningful exit status.

No Kubernetes dependency is needed for core transcription behavior. Backend readiness failure cannot leave the API falsely ready; source/image contents must not include user credentials.

Acceptance: a clean GPU-free Linux amd64 builder produces both images. The current RTX 5070 Ti Laptop machine runs real public-API CUDA transcription via standalone Docker, with verified identities, UID/permissions, private backend, mounts, and signal behavior. A CUDA compiler or copied source tree alone is insufficient.

### REQ-016: Helm deploys the complete transcription service

One chart deploys the Go/Echo public API and its dedicated Whisper backend in separate Pods with configurable image repository/tag/digest for each, CPU/RAM resources, persistent cache/existing claim, read-only prepared models, and isolated transient storage. Each API instance is paired with its own backend; a shared backend pool cannot bypass instance-local queue/model ownership. The backend receives one `nvidia.com/gpu` by default; explicit GPU request/limit must match and cannot be zero for the inference backend. API does not consume an additional GPU. Scheduling selectors/affinity, tolerations, and optional runtime class are configurable.

Default replicas are one. Each extra inference instance needs a compatible allocated GPU and appropriate storage; queues/state are local, with the same release-configured served ID. No global state, autoscaling, or cross-replica lifecycle management is promised.

Public Service is internal by default; optional Ingress is disabled. Enabled Ingress has configurable host/class/annotations/TLS references through an existing controller and exposes only the public API. Neither backend route publication, controller/certificate provisioning, nor authorization is supplied.

Startup/readiness/liveness probes accommodate provisioning/model-load budgets and distinguish backend from public API readiness. Termination grace accommodates shutdown plus cleanup. Invalid chart values fail validation/rendering. Insufficient GPU capacity may leave workload pending with clear events; CPU serving is not a scheduling fallback.

Acceptance: Helm 3/4 render/validate compatible matrix settings, including both image identities and backend-only GPU allocation. The Home Lab RTX 2060 installation performs real public-API CUDA transcription. Overrides/probes/grace/volumes and private backend access are observed. Ingress-disabled and enabled manifests expose exactly the intended public boundary.

### REQ-017: Startup, shutdown, rollout, and recovery

Startup validates/provisions the model, starts the backend, loads the fixed model once, and becomes ready only after success. On termination the API becomes unready, rejects new work, cancels queued work with 503 when possible, and allows active work within shutdown bounds. At expiry it safely stops backend work and cleans resources, without freeing a running context. Forced process loss may drop connections.

Default upgrade/rollback must work without a spare GPU, permitting old instance shutdown before replacement with documented downtime. Zero-downtime rollout is not promised. Failed release/configuration must preserve prior valid model cache; identities remain distinguishable for rollback. Restart begins with empty queue/process-local residency and loads the configured model again.

Uninstall does not delete/modify operator-provided claims/files. Chart-owned cache is retained by default, with separate explicit disposal. Provisioning failures, out-of-memory/init errors, GPU absence, backend/API crashes, and abrupt termination have bounded diagnostics and documented correction/restart paths. Transient or terminal failure cannot fabricate successful inference.

Acceptance: SIGTERM, forced kill, API/backend failure, bad startup, failed rollout, and rollback preserve valid disk data and restore the fixed resident model. Accepted/queued requests show declared completion/cancellation/loss outcomes. Single-GPU upgrade does not wait indefinitely for a surge GPU; uninstall retains the named storage.

### REQ-018: Resident stability and cleanup evidence

V1 retains model RAM/VRAM allocations while ready and idle. API-owned upload, normalization, prompt, and response resources and transient files must be released on terminal paths; repeated requests must not accumulate request data or grow model copies. The backend may retain the bounded internal context state described in REQ-007, including its latest result. V1 verifies memory stability and request isolation rather than immediate erasure of all native decoder/output buffers. Successful shutdown/configuration replacement releases application-owned model/context allocations, not just a status label.

Cleanup/recovery is bounded by REQ-003. Driver/runtime/allocator housekeeping may leave bounded non-model process memory; zero total RSS/GPU usage is not promised. Evidence combines actual CPU/GPU allocation observations, baseline/envelope, model-load/file-I/O observations, repeated requests, and stop/restart cycles. An envelope cannot excuse accumulating allocations or unreleased model-size data after worker stop.

For future ram-cache/on-demand/warmup/unload work, retain the original stronger per-transition proof: ram-cache frees model/inference GPU allocations but keeps only last model's reusable RAM weights; on-demand/unload free model RAM/VRAM; switching frees the prior model before acquiring the next. Use at least two sizes, successful/failed/cancelled/timed-out work, repeated warmup/switch/unload cycles, and absence checks able to detect intentionally suppressed release. Those future checks are not V1 completion gates.

Acceptance: both required V1 environments demonstrate stable repeated resident inference, API-owned resource and transient-file cleanup after successful/invalid/cancelled/timed-out work and failures, isolated responses, no recurring model load, and model/context release after shutdown. A latest internal result alone is not a failure; accumulating allocations, cross-request influence, or ordinary per-request restart are failures. Status/logs, GPU enumeration, and CPU mocks alone cannot establish this criterion.

### REQ-019: V1 access, least privilege, and privacy

V1 has no API authentication/authorization: no required key, accounts, client/operator roles, or built-in proxy auth. Anyone reaching the public API may transcribe. Deployment/access instructions state this; TLS and internal Service are not identity controls. Lifecycle endpoints are deferred, so no V1 public model mutation is advertised.

Both components enforce the configured model boundary, input/resource bounds, private backend access, and least privilege. IDs/uploaded filenames cannot become arbitrary filesystem access or shell instructions. Audio/prompt/transcript are transient and must not be persisted as application records or content logs. API-owned buffers and transient files are released on success, rejection, conversion failure, cancellation, timeout, and shutdown. Bounded upstream context state may remain as described in REQ-007; V1 does not promise secure zeroization of every native RAM/VRAM buffer immediately after a response. This permission does not permit cross-request influence, durable audio/transcript storage, content logging, or unbounded retention. Abrupt kill leaves only disposable instance-local work storage, cleared on restart/replacement and never reused as another request's input.

Diagnostics from API and backend contain bounded stage/model/outcome/error information without audio, transcript/prompt, credentials, client filenames, or unsafe raw decoder output. Source credentials stay out of image layers and logs.

Acceptance: valid unauthenticated calls succeed; unknown models/path injection/oversized inputs reject despite no identity controls. Recognizable test markers are absent from emitted logs and transient/persistent files outside the response after each terminal path; permitted native in-memory context state is assessed for boundedness and response isolation under REQ-007, REQ-013, and REQ-018. No recording/transcript database is created; backend access cannot circumvent public controls.

## Completion and future scope

### REQ-020: Two mandatory runtime verifications and performance measurements

Implementation verification requires (1) standalone Docker on the current RTX 5070 Ti Laptop GPU machine and (2) Helm/public-API operation on the Home Lab RTX 2060 Kubernetes cluster. Neither substitutes for the other. Include both-image builds/configuration, Helm rendering/schema/compatibility checks, actual CUDA transcription, resident reuse, queue/isolation/failure/privacy/recovery, and shutdown cleanup.

Exercise the configured compatible model, another model through configuration/restart, an invalid artifact, and all advertised audio families. Models used on each GPU must fit its available VRAM; no requirement says all catalog models fit all GPUs.

Record startup/model-load time, repeated steady-state latency (including median/p95 for a declared sample), audio duration and processing-time/audio-duration ratio, queue conditions, VRAM/RAM observations, and errors. Use reproducible public audio and identical model/inference parameters; distinguish first load, normal resident reuse, transcription execution, queue wait, and API overhead. Where possible compare direct backend and public API on the same setup. No numeric performance SLO was selected; measured results must be reported without inventing a guarantee. Repeated requests cannot reload weights or restart a healthy backend as normal behavior.

Evidence identifies sources/images/chart, model/format, both GPUs/drivers, OS/runtime, Kubernetes/Helm/toolchain versions, inputs/parameters/sample counts, passed/failed/unrun checks, and limitations. Missing access or compatible hardware is unverified, never passed. Rendering, GPU enumeration, source review, and unit tests are not runtime evidence.

Acceptance: both named environments have actual reproducible transcription and lifecycle evidence plus separate performance results. No healthy per-request model reload, accumulating request allocations, false CPU proof, or hidden failed environment is accepted. Approved specification status proves requirements completeness only, not a built/accepted release.

### REQ-021: V1 exclusions

V1 excludes ram-cache/on-demand policies, per-request model switching, per-model policy overrides, optional RAM/VRAM warmup controls, HTTP warmup/unload, and an upstream Whisper-server fork/source modification. These are explicitly deferred rather than cancelled; REQ-008–REQ-010 and REQ-022 retain their future contract.

Other exclusions remain: UI/admin web server, auth/access management, Prometheus exposition, audio/transcript persistence, full OpenAI API/streaming/translation/model listing, inference algorithms, GPU/Kubernetes infrastructure provisioning, automatic Home Lab deployment, distributed model state, multi-model residency, multi-GPU inference, autoscaling, and unmeasured quality/latency/throughput promises. Project initialization, public repository setup, agent tooling, CI and artifact-release automation are now included by the user's follow-up; the earlier release-infrastructure exclusion no longer applies.

Included: a separate API service, unchanged Whisper-server image, complete Helm deployment, one resident startup model, transcription, health, resource bounds/privacy/recovery, both required real GPU verifications, and the project/release outcomes in REQ-024–REQ-028.

Acceptance: configuration/routes/docs accurately distinguish V1 and future work. Deferred settings reject, deferred/unknown routes return 404, no /metrics/admin/auth feature is claimed, and packaging/transcription cannot be deferred under these exclusions.

### REQ-022: Required development after V1

A separate future-development section must retain these required directions:

- Implement `ram-cache` and `on-demand`, the earlier model-selection/lifecycle controls, and HTTP warmup/unload. A fork or extension of the upstream Whisper server will likely be needed; this is an expected design investigation, not an assertion that HTTP proxying already supports them or a mandatory future implementation technology.
- Preserve per-request selection from the configured served catalog, independent download/served sets, global default `ram-cache` plus per-model overrides, and one active operation/one managed model. A switch releases previous RAM/GPU weights before new acquisition and never invalidates in-flight work; failed replacement cannot be served as the requested model.
- `ram-cache`: retain only the last model's reusable RAM weights and release GPU model/inference allocations after processing; same-model reuse must not reread its file. GPU context recreation/transfer remains permitted. `on-demand`: release model RAM/VRAM after processing while keeping disk files, except the explicit temporary warmup override in REQ-009.
- Retain future FIFO coordination of transcription/warmup/unload, validation/queue/waiting-timeout no-mutation rules, safe failure/cancellation cleanup, corrupt-context rejection, completed-state/idempotent management replies, request-state isolation, and observable reclamation (REQ-008–REQ-011, REQ-013, REQ-018). Automatic/manual warmup loads only weights without synthetic inference. Invalid modes/configuration must not silently change behavior.
- Authentication/authorization and issuance/administration of access.
- A possible lightweight admin web interface for access management and observing operation; this remains a proposed future direction.
- Prometheus operational metrics.

Future work is required to be recorded, not implemented in V1. Later specification/design must resolve integration/fork changes and refine remaining contracts before implementation, preserving these retained obligations unless the user changes them. No schedules, estimates, task order, or confirmed V1 UI/profile are implied.

Acceptance: all deferred memory modes and management/selection semantics, likely upstream-fork investigation, access/admin/metrics directions, and their certainty are explicitly retained. Deleting them, treating proxying as sufficient proof, or advertising them as delivered in V1 is excluded.

### REQ-023: Planned profile contracts

Retain the confirmed planning outputs under this specification; specification authoring does not create them:

| Artifact | Required coverage |
| --- | --- |
| `core/contracts.yaml` | Docker/Helm configuration parity, fixed startup model, artifact sources/cache, resident-only validation/defaults, both image identities and deployment controls, repository/distribution/version identities and release states: REQ-001–REQ-008, REQ-015–REQ-017, REQ-024–REQ-031 |
| `contracts/openapi.yaml` | Transcription/health operations, typed field/status/error contracts, fixed-model validation, queue/deadline behavior, private backend boundary, no V1 authorization, and explicitly absent management routes: REQ-009–REQ-014, REQ-016, REQ-019, REQ-029–REQ-030 |
| `quality/scenarios.yaml` | Both required GPU environments, CUDA architecture/image compatibility, cache/offline/integrity, resident reuse/cleanup, no per-request reload, concurrency/cancellation, readiness/recovery/rollout, privacy, measured performance, clean contributor setup, agent discovery, untrusted CI, multi-channel publication and release gating/recovery: REQ-002, REQ-004–REQ-007, REQ-011–REQ-020, REQ-024–REQ-031 |

The required future directions in REQ-022 remain scoped as future work; these outputs implement only the agreed V1 contract.

Acceptance: outputs have behavioral requirement sources and preserve the V1/future boundary. The specification introduces no implementation-local symbols/classes, task packets, handoff, executable solution, estimates, or unauthorized planning.

## Project initialization and public distribution

### REQ-024: Reproducible open-source project initialization

The release includes a usable local Git repository and the public GitHub repository `swimmwatch/openwhispr-whisper-server`. A fresh contributor checkout can resolve the pinned Go/tooling dependencies, run documented checks and build both images without a GPU or maintainer credentials. Generated output, model weights, user audio, local agent history and credentials are excluded from source/build inputs; required third-party license material remains included. Remote setup verifies the selected owner and repository rather than overwriting an unrelated repository.

The project's own code and Helm chart use Apache-2.0. Distribution preserves the actual licenses/notices and applicable redistribution obligations of the packaged upstream, decoder, CUDA/base images and other dependencies; the project license does not relabel them. Public community material includes a license, contribution rules, security reporting and support policy, code of conduct, change history, third-party attribution, issue/PR templates, operator/developer documentation and a final README with tested installation examples. Sensitive reports have a usable private reporting route; contact details cannot be invented. Repository protections, required CI checks, dependency-update configuration and package visibility are configured and read back.

Acceptance: a clean checkout executes the documented contributor checks without local personal paths/secrets; Git/status and public repository settings are verified. Community files are complete, coherent and discoverable, license/notices match actual distributed components, and README commands match tested artifacts. A placeholder README, undisclosed license gap, unavailable private security route or unverified public/package visibility cannot be reported as completion.

### REQ-025: Portable Codex project support

The project supports Codex with a case-correct root `AGENTS.md`, one authoritative set of project rules and discoverable project-local GPU acceptance and release verification skills. Rules describe the agreed Go, Docker and Helm architecture, actual build/test commands, specification/planning/execution boundaries, applicable source lookup, resource/privacy constraints and release checks. Agent support is portable between contributor machines and does not depend on the author's home directory or globally modifying a user's settings.

The project does not vendor the external engineer-agent-workflows plugin. External developer plugins remain separately installed operator tooling, outside project setup acceptance. A workflow invoked from such a plugin still respects its own manual/MCP prerequisites; project instructions do not provide a substitute or bypass. Only relevant project settings and skills are configured; available and unavailable optional tools are distinguished. Credentials, transcripts, personal memory and machine-specific trust/provider/model preferences do not enter tracked configuration. Project settings do not silently disable sandbox/approval controls or authorize publication/deployment. Private Home Lab access remains under ignored local storage, with explicit target selection and read-only preparation; it is not required in a contributor checkout.

Acceptance: a fresh Codex project session identifies the intended instruction and local-skill sources, explains the Go/Docker/Helm boundaries and exposes usable project check commands. On the operator's workstation it resolves the private runbook and performs its explicit-target read-only connection check without disclosing private data. A malformed config, stale/absolute personal skill path, missing local prerequisite or duplicated conflicting rules is detected. Installation instructions without demonstrated discovery are incomplete agent-setup evidence; Codex and external developer plugins are not dependencies of the service or ordinary contributor CI.

### REQ-026: Continuous integration and controlled artifact delivery

GitHub Actions checks contributor changes and release candidates. Required coverage includes Go formatting/static checks/tests and material race/lifecycle cases; Go source security analysis and vulnerability checks; planning/product-contract validation; Dockerfile/workflow checks; clean GPU-free builds of both images; Helm values/render/schema/compatibility/package checks; Dockerfile and Helm configuration security analysis, including rendered chart misconfigurations; dependency/image security and license evidence; and documentation/settings checks when affected. Tool/action/base/source identities are pinned and updated through reviewed changes. Build cache may accelerate work but cannot replace a clean release build or hide missing source/dependencies.

Untrusted pull requests run without publication credentials, write-capable release jobs, cluster access or privileged self-hosted runners. Release permissions and registry credentials are isolated to trusted publication jobs with minimum necessary rights. Secrets cannot appear in layers, reports or logs. Builds record source/upstream/base/toolchain identities, image digests, chart checksums and software-component/provenance evidence. Failed, cancelled, unavailable or skipped required jobs block release completion; tests are corrected only when demonstrably wrong, not weakened to make a release pass.

CD covers artifact publication and GitHub releases. Home Lab installation and both real GPU acceptance suites remain separately authorized/manual execution with recorded evidence under REQ-020. GPU-free CI, static manifests and successful registry upload do not count as GPU acceptance.

Acceptance: a fork PR can run intended non-secret checks but cannot publish or obtain release/cluster credentials; a trusted candidate builds both images and packages the chart with traceable reports. Known Go source security findings and Dockerfile/Helm misconfigurations are detected by the required security analysis and block release under the agreed security gate. An intentionally failed required check prevents release publication/promotion. Required GPU evidence remains independently visible, and no automatic Home Lab deployment is configured.

### REQ-027: Multiple public image and chart channels

Both API and backend Linux amd64 images are publicly available in GHCR under GitHub owner `swimmwatch` and Docker Hub namespace `swimmwatch`. The single Helm chart is publicly available through GHCR OCI and the classic Helm repository at `https://swimmwatch.github.io/openwhispr-whisper-server/`. Exact image/chart package names and immutable identities form one release inventory. Namespace ownership and write access are verified during setup rather than assumed from a GitHub login.

Each release publishes the same selected image manifests to both image channels and the same packaged chart bytes to both chart channels. OCI manifest digests and chart archive checksums are recorded as distinct identities; an OCI chart manifest digest is not the archive SHA256. Helm chart/package versions use SemVer; image aliases and chart version/appVersion mapping are documented. Pre-release aliases cannot advance stable/default image aliases or be advertised as stable. The classic chart index preserves previously published versions and their checksums.

Consumers can select either image registry through existing Helm image overrides and install an exact chart version from either chart channel. Publication records partial outcomes, verifies anonymous pull/read-back from every required channel and preserves existing versions. A retry reuses verified immutable artifacts; different bytes under an already published version are rejected, requiring a new candidate version. A failed channel does not produce a completed release or silently advance stable aliases.

Acceptance: unauthenticated pulls from both image registries and Helm pulls from OCI and classic endpoints yield the expected identities and equivalent configuration. The classic index still serves an older version after a new publish. Injected channel failure/version collision remains visibly incomplete; retry recovers without overwriting old artifacts or marking a partial release complete.

### REQ-028: Pre-release verification before stable release

The first stable release is preceded by a versioned pre-release candidate. Candidate validation covers the full required CI build/check set, every publication channel, usable pulled artifacts, and both target GPU acceptance suites. When a build/check fails, implementation or release configuration is fixed and the relevant checks rerun until every required check passes. A source/runtime/build-input change invalidates affected prior evidence. Already published candidate versions are not overwritten; changed artifacts get a new candidate version.

Stable publication requires successful, current evidence for the selected candidate/artifact identities, finished public documentation and an explicit trusted release action. GitHub's pre-release flag alone is not that evidence. Validated executable image digests are promoted without an unnoticed rebuild. A stable chart has its own stable SemVer package identity, so version/alias metadata changes are revalidated and its new archive is checked in both channels; changed rendered behavior or executable inputs requires renewed affected acceptance. The release inventory distinguishes candidate build source, final release source, chart identity and promoted image identities without mislabeling provenance.

Missing credentials, publication/access failure, failed or missing required CI/GPU evidence and drift keep the candidate or stable release incomplete. Recovery preserves prior stable versions and model cache. Stable aliases and the public latest-release designation are finalized only after required stable-channel read-back succeeds; cross-registry writes are not claimed atomic. Deployment remains manual.

Acceptance: a successful candidate can be installed and transcribe on both GPUs, while an injected build failure, partial publication, stale source or missing GPU report prevents stable completion. Fixes produce a new candidate when artifacts change, with corrected checks/evidence. Stable image digests equal the accepted candidate digests, the stable chart/version checks pass, public documentation is complete and every required stable channel is readable before the release is reported finished.

## Go service foundation

### REQ-029: Structured diagnostics and request correlation

Both Go binaries emit structured JSON logs to stdout from startup onward, with validated configurable severity. Startup, readiness, recovery and shutdown events identify the component and safe stage/outcome. API completion logs include the HTTP method, matched route, response status, duration, request ID and safe error code when applicable. User content, raw URLs/query strings, headers, uploaded filenames, model-source URLs, configuration dumps and raw native/decoder errors are excluded at every level. Panic recovery emits a safe failure without leaking the panic value or stack into public responses/content logs.

Every API-produced response, including health, validation, rate-limit, queue, unknown-route and internal-error responses, includes X-Request-ID. A single caller-supplied ID matching [A-Za-z0-9][A-Za-z0-9._-]{0,127} is preserved; absent, duplicated or invalid IDs are replaced with a generated ID. Correlation is diagnostic, not authentication or deduplication. The same accepted ID accompanies owned application context, queue/execution logs and the private supervisor request/recovery diagnostics; the native C++ server requires no modification. Pooled HTTP framework contexts are never retained by workers.

Acceptance: a supplied valid ID survives successful and failed requests through API/queue/supervisor; invalid IDs cannot inject log/header content. Concurrent requests retain distinct correlation. JSON log fixtures parse and reflect actual response status, including recovered failures. Unique audio/prompt/transcript/error markers and unsafe metadata are absent from logs at every supported level.

### REQ-030: Per-IP admission rate limit with replaceable storage

The public transcription route has a configurable per-IP token-bucket rate limit before upload reading, audio decoding or queue admission. Health probes bypass this limiter. Denial returns 429 with rate_limit_exceeded and a positive Retry-After delay; queue_full remains a distinct 429 cause. A denied request performs no decoder, queue or backend work. The limit counts transcription attempts reaching the route, including requests later rejected by field validation; request ID never selects a quota.

V1 stores limiter state in memory independently in each API Pod, with idle-entry expiration. Restart resets quotas, multiple Pods have separate quotas, and clients behind one address share a quota; no cluster-wide limit is promised. Use a replaceable store interface so Redis or another backend can be added later. External store implementation and distributed semantics are deferred; selecting an unsupported storage backend fails startup. Storage errors fail closed with a safe 503 rate_limit_unavailable, never false admission or a fabricated quota-exceeded response.

By default the client address comes from the direct network peer and forwarded headers are ignored. Behind an operator-configured proxy, client IP may be extracted from X-Forwarded-For only across explicitly trusted proxy CIDRs, walking the chain from the trusted peer. No implicit trust of all private/loopback/link-local addresses is permitted. Canonical IPv4/IPv6 addresses form keys; untrusted headers cannot evade a quota. Forwarded/real-IP headers do not change identity outside the selected contract. Docker/Helm expose the same validated rate, burst, idle expiration, storage and proxy settings. Proposed configurable defaults are enabled, one request per second, burst four and 300-second idle expiration; these are policy assumptions, not throughput measurements.

Acceptance: one IP exhausts its quota while another remains admitted; quota refills and idle state expires. Spoofed headers and malformed proxy chains do not select an arbitrary identity. Health stays responsive under saturation, and denied uploads are not read. Tests exercise storage replacement/error behavior and separate Pod/restart semantics. Repeated high-cardinality traffic followed by expiration/cleanup does not leave accumulating stale entries; the in-memory store is not claimed as a complete network flood defense.

### REQ-031: Maintained Go foundation and dependency boundaries

The project follows documented Go module, naming, formatting, error and context conventions. Commands assemble dependencies explicitly; internal implementation packages remain private. Domain rules and transcription application logic do not depend on Echo, configuration-loading libraries, concrete HTTP clients, filesystem/process adapters or future limiter storage. Interfaces belong at actual consuming boundaries; no generic dependency-injection/repository framework or speculative persistence is required. Rate limiting remains an HTTP admission concern and cannot replace GPU queue/resource bounds.

Domain and application failures have a protocol-independent typed classification and preserve an underlying cause where needed. Service logic neither creates nor consumes HTTP status codes, headers, Echo errors or public JSON error envelopes. Transport and dependency adapters convert their failures into this internal classification before returning through application ports. At the HTTP boundary, handlers and their shared error adapter map internal failures to the existing status, cause code and safe response format under REQ-013; transport-only failures stay in the HTTP adapter. Wrapping an error cannot lose its classification, and matching must not depend on error-message text. Unknown errors become safe internal-error responses without exposing their cause. V1 implements only HTTP error adaptation; another protocol can add its own adapter without changing service error contracts, but no additional protocol implementation is required.

Use maintained compatible packages for HTTP middleware, typed configuration loading and reusable validation, and the standard library where sufficient. Pin module versions/checksums, record licenses and vulnerability checks, and avoid implementing existing logging/limiter/validation machinery. Thin integration and strict-input enforcement may be needed: library defaults cannot weaken CLI > environment > JSON > defaults, collection replacement, duplicate/unknown-key rejection, deferred-setting rejection or fail-before-listening validation. Request validation combines reusable field checks with model/audio semantic checks; validation tags do not replace bounded multipart decoding.

Acceptance: documented package boundaries match actual imports; a deliberate forbidden framework/adapter import into domain/application is detected. A fresh checkout resolves/verifies dependencies and passes formatting, vet, focused behavior/race checks and existing security gates. Invalid settings fail before a listener/native process starts, and fixture checks distinguish precedence, duplicate/unknown fields, strict types and collection replacement. Selected dependency versions and compatibility are verified during initialization.

Acceptance: service tests exercise typed and wrapped failures without HTTP/Echo dependencies. HTTP adapter tests verify the existing error registry, safe unknown-error fallback and correlation headers without serializing inner errors directly. The same internal failure retains its classification across direct and wrapped application calls; HTTP status, public messages and response serialization are added only at the HTTP boundary. Disconnect/deadline/shutdown handling preserves REQ-011 and cannot fabricate a response after connection loss or successful inference from a failure.

