# Shared acceptance helpers — P07 / future P08

This package is in progress. **It is not a complete GPU acceptance harness or a release qualification command.** P07 and P08 require separate real-target reports; neither helper tests nor a successful public transcript satisfies the entire packet.

## Implemented slices

- `PublicClient`: one bounded multipart request, unchanged ordered/duplicate form fields for negative workloads, no redirect replay or application credentials, full public response-consumption timing, JSON/text handling and safe diagnostics. Reply serialization excludes transcript and raw server error code; callers must not log in-memory reply content.
- `MeasurePublicSteadyState`: explicitly declared 30-or-more requests at concurrency one; successes and errors remain visible. Cancellation returns partial observations and an error. It does not retry failed inference, insert readiness waits between steady samples or invent backend timing.
- `SummarizeMeasurements`: successful-request median/nearest-rank p95, measured backend/audio ratios, separately observed startup/model load, resource counts/peaks, and API overhead only when matching queue/backend observations actually exist. Unknown is `nil`, not zero. It does not enforce a performance SLO or certify a target.
- `DockerTarget`: the unchanged canonical `deploy/compose.yaml`, explicit project/config/prepared paths and immutable candidate image references; bounded serialized operations, actual inspection and privacy-safe process names. Failed startup cleanup is bounded. Ordinary teardown retains model-cache volumes and prepared files. Raw returned logs and mount/endpoint identities are private observations, not public report fields.
- `Observe`: bounded actual private health, native identities, API/supervisor/native RSS/FD and raw scratch counters. Unknown facts remain nil with errors; a known empty ownership lock is counted independently. Snapshots are serial, not atomic; process identities/paths remain private.
- `CorrelateTimings`: isolated sequential public samples matched to actual queue/private HTTP events by validated request IDs and completion order. Missing/contradictory facts fail rather than invent timing; native initialization-to-ready is not pure weight-file load time.
- `TestDockerGPUResidentBaseline`: explicit real-GPU opt-in baseline using pinned candidate inputs and the identified public eleven-second JFK fixture. It verifies actual public transcription, execution boundaries and sixty repeated requests with native process continuity. It is not the complete resident, client, resource or lifecycle suite.

## Local helper validation

Use the actual Go 1.27.3 compiler selected by the contributor setup. An older Go shim with `GOTOOLCHAIN=local` is not equivalent.

```sh
GOTOOLCHAIN=local GOWORK=off P07_GPU_TESTS= P07_LOCAL_TESTS= go test -race ./tests/acceptance -count=1
GOTOOLCHAIN=local GOWORK=off go vet ./tests/acceptance
```

The explicit GPU baseline is skipped in this helper-only run. That skip is not a passed GPU check. A `go test -run` selector matching no tests may also exit successfully; it is never qualification evidence.

## Separate local defect verification (not qualification)

The user's explicit local-fix stage may run `TestLocal*` on locally built source,
without qualifying published artifacts. It never falls back from candidate mode.
`NewLocalDockerTarget` accepts only named immutable
`whisper-p07-local-api@sha256:…` / `whisper-p07-local-backend@sha256:…` references;
`NewDockerTarget` still rejects them. Mutable tags and bare IDs are not targets.

Supply a private bounded build receipt (`scope: local_validation_not_release`,
`registry_push: false`, `release_qualified: false`) with actual API/backend image
references/engine IDs, runtime fingerprint, verified public base/tiny model files
and fixture directory. Build/source changes invalidate earlier final results.
The public JFK derivatives must include all advertised formats, distinct-recording
controls and packaged-decoder-verified 900s/901s lossless fixtures. No personal
recording or operator model cache is selected.

```sh
P07_LOCAL_TESTS=1 \
P07_LOCAL_RECEIPT=/absolute/private/local-build/receipt.json \
P07_LOCAL_SOURCE_BIND_IP=DOCKER_HOST_BRIDGE_ADDRESS \
P07_LOCAL_CA_BACKEND_IMAGE=whisper-p07-local-backend@sha256:FIXTURE_DIGEST \
GOTOOLCHAIN=local GOWORK=off \
go test ./tests/acceptance -run '^TestLocal' -count=1 -v -timeout=45m
```

The cache test's separately identified backend derivative changes only
`SSL_CERT_FILE=/prepared/p07-source-ca.pem` and a fixture label. A generated CA
public certificate uses the existing read-only prepared bind; its private key
stays in the test process. The source fixture binds only the explicitly selected
Docker host bridge address. HTTP and rejected TLS attempts are counted, including
under the deny trap. Do not disable HTTPS validation, modify host trust or add a
Compose override. Other scenarios restore/use the original backend image.

These opt-in tests exercise public/client, bounds/FIFO, reduced/default lifecycle,
fixed-model/cache and independently observed resident paths. They do not pull,
publish or promote a candidate. Current failures and unrun assertions stay visible
in the [local checkpoint](../../docs/verification/docker-local-pre-release.md).
They retain their separate local-only receipt/target guards. They do not become
published-candidate evidence merely because the same case helpers are reusable.

## Real candidate baseline

After explicit P07 authorization, verified image pulls and private fixture preparation, supply absolute paths to the selected public release inventory and fixture:

```sh
P07_GPU_TESTS=1 \
P07_RELEASE_INVENTORY=/path/to/selected/channel-state.json \
P07_AUDIO_FIXTURE=/path/to/public/jfk.wav \
P07_AUDIO_SHA256=59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e \
GOTOOLCHAIN=local GOWORK=off \
go test ./tests/acceptance -run '^TestDockerGPUResidentBaseline$' -count=1 -v -timeout=38m
```

This command starts only an isolated local canonical Compose project, not Kubernetes. It selects exact images from the inventory; it does not build/publish artifacts. Evidence/configuration remains under ignored `.local/p07/run/`. Owned containers/networks are removed; named model caches are retained. Deleting retained data is not automatic cleanup.

The initial rc.2 run **failed** repeated requests with transient `not_ready` despite successful individual transcription and healthy backend responses. Do not add delays/retries, change the candidate bytes under its published version or call the packet complete. See the owning P07 packet and handoff for current recovery status.

## Published candidate slices (partial P07, not qualification)

`TestResidentSuite`, `TestClientCompatibility` and `TestResidentMeasurements` are
real entry points. They reuse the same assertions as the local checks but select
`NewDockerTarget`, never `NewLocalDockerTarget` or a relabeled local build receipt.
No image build, registry pull, dependency installation or client environment repair
is performed by the target adapter. The existing client helper still requires its
already prepared locked SDK/Electron prerequisites.

Prepare a private selection (directory 0700, file 0600) with absolute paths:

```json
{
  "release_inventory": "/absolute/private/selected/channel-state.json",
  "registry": "ghcr.io",
  "base_file": "/absolute/private/public-models/base.bin",
  "fixture_directory": "/absolute/private/public-jfk-derivatives"
}
```

`registry` may instead be `docker.io`. The inventory must provide the candidate
version, source SHA, build/runtime identities and both immutable image indexes.
`tiny_file` is optional preparation metadata; no published tiny/cache recipe is
implemented by this increment. Local scope/image/CA override fields are rejected.
Base model copying checks the canonical checksum/size; the JFK WAV checksum is
checked before target startup. Derivative cases keep their own pinned checks.
Do not supply a stale Docker bridge or the local source-CA derivative to this mode.

Put the **actual Go 1.27.3 bin directory** first in PATH; verify its version with
`GOTOOLCHAIN=local go version`. Then, only after explicit target authorization and
exclusive GPU ownership, run implemented server slices sequentially:

```sh
rtk proxy env P07_GPU_TESTS=1 P07_LOCAL_TESTS= \
  P07_PUBLISHED_SELECTION=/absolute/private/published-selection.json \
  GOTOOLCHAIN=local GOWORK=off GOCACHE="$PWD/.local/go-cache" \
  go test ./tests/acceptance \
    -run '^TestResidentSuite$/(baseline|public_contract|input_bounds_and_isolation|fifo_and_queued_cancellation|queued_deadline|lifecycle|active_api_shutdown|model_observer|gpu_unavailable)$' \
    -count=1 -v -timeout=45m
```

Use `-run '^TestClientCompatibility$'` for the genuine SDK JSON/text and OpenWhispr
transport group, or `-run '^TestResidentMeasurements$'` for the existing 900-second
calibration/three-block/model-I/O/GPU-observed steady measurement slice. These are
**partial results**, not a full P07 pass. For a server-only scope without Desktop,
select `-run '^TestClientCompatibility$/^official-openai-(json|text)$'` to run only
the genuine SDK HTTP requests; the Electron subtest is not invoked. The server
`TestResidentSuite/public_contract` group never calls the client group. A subtest
selector can deliberately omit required groups; that omission must remain visible
in the acceptance report.

An unfiltered opt-in `TestResidentSuite` fails its explicit
`required_unimplemented_gates` subtest. In particular, counted HTTPS/cache/model
recipes on the unchanged published backend are not implemented: the local CA
image derivative cannot qualify the original candidate. Configured idle and
retention/failure memory envelopes, remaining reload/raw-output/overlap PROVE
mutations and concurrent performance/direct-native comparison remain outstanding.
The selected server stage exercised Docker Hub public/lifecycle recipes separately;
see the [published rc.3 report](../../docs/verification/docker-rc3-http.md).
The full client gate still requires its authentic transport. User-selected
server-only selectors do not invoke Desktop or claim that gate.

Private `candidate-binding-*.json` receipts are emitted at startup, configuration
recreation and final cleanup alongside each slice's reports under `.local/p07/run/`.
They bind the R01 source/build/runtime/index identities and inventory/selection
checksums to actual selected-reference image inspection, container engine image IDs,
container IDs, mounted configuration bytes/effective settings and audio/model
identities. Engine image IDs are **not** universally OCI config digests. Receipts
always retain `qualification: not_established`; they contain no transcript/audio
content and do not advance any release inventory. Canonical Compose, no-build/no-pull,
bounded cleanup and cache retention remain unchanged.

## Official client transport probe

The locked dev environment contains `openai==2.11.0`. The [SDK probe](openai_client.py)
uses that real client with automatic retries disabled, a finite end-to-end Linux
signal deadline, no environment proxy/redirect replay, and only the bounded,
checksum-verified public JFK fixture. It emits transcript length/hash, not content.

```sh
uv run --locked python tests/acceptance/openai_client.py \
  --base-url http://127.0.0.1:PORT/v1 \
  --audio /path/to/public/jfk.wav --response-format json
```

Use `--response-format text` for the second supported representation. The target
must be the explicitly authorized live candidate or separately authorized local
fix target; local calls do not qualify a published candidate. `TestClientCompatibility`
now runs only the required genuine SDK ten-format JSON/text matrix under the
Approved REQ-012 amendment. Desktop/Electron execution is cancelled as a V1 gate,
not passed; SDK success does not establish actual OpenWhispr application
interoperability. A handwritten POST is not a replacement for SDK evidence.
Local disposable HTTP fault-server tests establish failure/privacy behavior only.
The published rc.5 server report owns actual candidate outcomes.

For memory evidence, API RSS now uses the precise PID 1 `smaps_rollup` resident
total, with missing/invalid data rejected; no approximate `status/VmRSS` fallback
is used. This changes the observation source, not envelope formulas, conditioning
budgets, success/failure workloads or retention checks. Each new run independently
calibrates its bounds; earlier approximate-counter bounds are not reused. All owned-process aggregate RSS and backend breakdown Anonymous now use precise
rollup counters too; only the independently labeled File counter remains
approximate `status/RssFile`. No PSS or RSS-minus-anonymous substitution is used.
The v3 memory entry requires **both** `P07_API_GC_TRACE=1` and
`P07_SUPERVISOR_GC_TRACE=1`; missing either fails before observation/calibration.
Actual flags/images/start identities are independently verified. These fixed
profiles enable numeric diagnostics, never GC tuning or forced collection.
An independent premeasurement completion barrier plus three comparable natural
cycles freezes each Go role's marked-live interval. The same bounded collector
checks measured and terminal phases; precise API RSS remains reported and its
three-request-block accumulation check remains active. Only the API absolute
completed-RSS rejection is replaced by its independently frozen heap proof.
Pending, stale, forced, replaced-process or missing proof cannot qualify a run.

If necessary, at most 4096 sequential GETs of the existing `/health/live` per
observation phase obtain ordinary collections under the unchanged caller deadline.
The same procedure is used for calibration and measurement; actual calls/errors
and timing are retained. These are not extra inference requests. No budget is
reset and the 120-inference conditioning cap is unchanged. Numeric logs use
bounded inclusive `--since/--until` windows; supplementary artifacts never reread
accumulated history. Scope transitions require proven retirement and independent
pre-work calibration of a new Go process, not refitting from failed work.
Whole-MiB quantization does not prove every sub-resolution retained copy.

Run functional/SDK slices and independent correlated performance without these
GC profiles. Diagnostic memory timing does not establish queue/backend/API
components; use the separate uninstrumented performance entry and owning report.
Individual private reports retain `qualification: not_established`; only the
owning packet can establish overall completion from all actual required evidence.

## Partial P08 public and SDK workloads

`TestP08SharedHTTPPartial` uses the same public-contract and genuine SDK cases as
P07 against an already authorized, parent-owned Kubernetes pair and loopback
forward. It does not install or replace the release. Its private mode-0600 JSON
selection contains `resource_target` (absolute path to the existing private P08
resource selection with namespace and Pod UIDs), `api_image_id`, and
`backend_image_id` (actual runtime-reported image IDs). The selection and target
are private and must not be copied into reports.

Set `P08_SHARED_HTTP=1`, `P08_SHARED_HTTP_SELECTION` to that selection,
`P08_SHARED_BACKEND_PORT` to the selected supervisor port, and the existing
`P08_DEVELOPMENT_API_URL`, `P08_DEVELOPMENT_FIXTURES`,
`P08_DEVELOPMENT_EVIDENCE`, and `P08_DEVELOPMENT_MODEL` inputs. The API URL must
be loopback; fixtures and an existing mode-0700 evidence directory must be
absolute. Do not set this opt-in without an authorized target and test window.
With those private inputs already set, the SDK-only selector is:

```sh
rtk proxy env P08_SHARED_HTTP=1 go test ./tests/acceptance \
  -run '^TestP08SharedHTTPPartial$/^client_compatibility$' -count=1 -v -timeout=35m
```

This invokes the existing locked Python SDK JSON/text codec matrix; a CPU fake
runner test is not SDK or GPU evidence.

At each workload boundary the test checks selected process/Pod/image identities
and correlates a unique loopback health request to the selected API Pod log.
These are boundary samples, not continuous identity or network isolation proof.
A selected or partial run does not establish CUDA activity, memory, lifecycle,
full shared-suite execution, or P08 qualification.

## Still required

Complete the owning packet's format/client compatibility, independent CUDA inference activity, model-file/RSS/VRAM/FD/transient-resource observations, calibrated repeated/failure envelopes, queue/deadline/disconnect/privacy cases, fixed/second/invalid model cases, offline/prepared/cache paths, restart/reconfiguration/signal/retention recipes and independent performance report. Actual clients must run their supported transports, not a handwritten compatible request substituted for them.

P08 must reuse completed shared assertions on its own independently authorized target, with a separate test-task workflow and Kubernetes/CNI/upgrade/rollback/storage evidence. No Kubernetes runner or cluster deployment is authorized by this package's existence.
