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
They are not the still-planned full P07/P08 qualification entry points.

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
fix target; local calls do not qualify a published candidate. These calls passed
on recorded local images, not yet against an accepted GPU candidate. Local disposable HTTP fault-server tests check
client failure/privacy behavior only. OpenWhispr must additionally run its genuine
supported upload transport; a handwritten POST is not a replacement. Its prepared
scoped Electron probe is private task tooling, not full UI acceptance evidence.

## Still required

Complete the owning packet's format/client compatibility, independent CUDA inference activity, model-file/RSS/VRAM/FD/transient-resource observations, calibrated repeated/failure envelopes, queue/deadline/disconnect/privacy cases, fixed/second/invalid model cases, offline/prepared/cache paths, restart/reconfiguration/signal/retention recipes and independent performance report. Actual clients must run their supported transports, not a handwritten compatible request substituted for them.

P08 must reuse completed shared assertions on its own independently authorized target, with a separate test-task workflow and Kubernetes/CNI/upgrade/rollback/storage evidence. No Kubernetes runner or cluster deployment is authorized by this package's existence.
