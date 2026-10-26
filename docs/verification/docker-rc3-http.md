# Published rc.3 server HTTP/GPU verification

**Selected server-only stage: PASS** (2026-10-26, Europe/Moscow).
The user explicitly requested direct HTTP tests and excluded Desktop/Electron.
No desktop application was executed in this stage. This report
is **not complete P07/P08 qualification** and does not change rc.3's release state.

## Actual immutable target

- Candidate: `v0.1.0-rc.3`, source `a47647049ad69ae154e327d8d2536d650eaa4dee`.
- API index: `sha256:6cd42d7046d27ea12974b1f6a4d746f381549258da81b273d90ee9a62c0accd2`.
- Backend index: `sha256:d9e94866fcd015c8c2091e5c6d79afeec01432b110aaeb23bb6d14586453de1e`.
- Both GHCR and Docker Hub references were used through the canonical Compose
  target with `--no-build --pull never`, not local rebuilds or a CA-derived image.
  Their anonymous pulls and public artifact identities were already verified in
  [R01](<release-rc3.md>).
- GPU: RTX 5070 Ti Laptop, driver 595.99.02, 12227 MiB; Docker 29.9.2 and
  Compose 5.6.1. Other GPU consumers were present and left untouched: these are
  measured test conditions, not an isolated performance benchmark or numeric SLO.
- Model: public multilingual `base`, read-only prepared file,
  SHA256 `60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe`.
  Audio: pinned public JFK WAV and recorded derivatives, not personal recordings.

## Execution and results

| Actual check | Result |
| --- | --- |
| Published resident baseline, 60 sequential requests and unchanged native worker | PASS, 16.177s, 60 successes / 0 errors |
| GHCR HTTP matrix: 10 formats, 7 options, 34 invalid/raw request cases | PASS, all 51 cases |
| Input/file/duration/prompt bounds, recording isolation and prompt reset | PASS |
| FIFO, overflow, queued cancellation and queued operation deadline | PASS |
| Enabled limiter, active cancel/deadline, API/backend death, native crash/recovery, restart and down/up retention | PASS, eight lifecycle cases |
| Active API SIGTERM drain and SIGKILL recovery | PASS, both modes |
| Model-file observation and unavailable-GPU fail-closed case | PASS |
| Genuine OpenAI SDK JSON/text HTTP requests, no desktop transport | PASS, both cases |
| Longest input, measured three blocks of 20, correlated CUDA activity/model-I/O observations and released resources | PASS |
| Docker Hub selected references: HTTP matrix plus lifecycle/recovery recipes | PASS, 51 cases plus eight lifecycle cases |

The selected GHCR server slices completed in 94.92s; SDK HTTP cases in 3.700s;
resident measurements in 20.153s. The focused Docker Hub run completed in 51.04s.
All commands returned exit 0. The reduced 3s operation-deadline case returned
HTTP 408 `operation_timeout` in 3.516s without a transport error, followed by
recovery. Reduced fixtures are not presented as default timeout measurements.
Ordinary requests require recognizable public speech; the temperature-1 boundary
case checks protocol/nonempty content, not guaranteed lexical accuracy.

### Measured observations, not acceptance thresholds

The independent measured 60-request workload had 60 successes and no errors:
public median **98.371ms**, p95 **425.488ms**; queue median 0.345ms, private HTTP
median 88.500ms and observed API-overhead median 9.190ms. These include observation
overhead and the stated shared-GPU conditions. Initialization-to-CUDA-ready was
573.320ms, **not pure weight-file loading**. Fourteen native-process GPU samples
recorded peak SM activity 32% and framebuffer 557842432 bytes. Scoped recurring
model-path operations: zero.

This is not a new calibrated idle/retention/failure-memory-envelope proof. Do not
infer that every possible leak is excluded from snapshots or successful requests.

## Reproducible harness and binding

The previous planned full-suite selectors were absent. The increment adds actual
published entries while preserving strict local-only receipt/constructor guards.
Selection JSON references the R01 inventory and verified public fixture paths,
with an explicit `ghcr.io` or `docker.io` registry; it never relabels a local build
receipt as published evidence.

Each selected scenario records candidate source/build/runtime/index identities,
selection/inventory/configuration/audio hashes, effective settings and actual
container/reference/engine-image IDs. Engine image IDs are not mislabeled as
universal OCI config digests. Report scope is `published_candidate_partial_acceptance`
and qualification remains `not_established`.

The implementation passed behavioral RED/GREEN, focused tests, `go vet`, gofmt
and the complete acceptance-package race suite (144.135s) before real execution.
The harness was tested as an uncommitted increment based on `29ea5fc`; this
follow-up records that increment. Exact tested tracked patch and new source
snapshots are retained in the private
[harness snapshot](<../../.local/p07/rc3-preflight/harness/tracked.patch>).
The published rc.3 source/artifacts were not changed.

Actual server selector, with Go 1.27.3 explicitly selected in PATH and private
`P07_PUBLISHED_SELECTION`, `P07_GPU_TESTS=1`, `P07_LOCAL_TESTS` empty:

```sh
go test ./tests/acceptance -run '^TestResidentSuite$/(public_contract|input_bounds_and_isolation|fifo_and_queued_cancellation|queued_deadline|lifecycle|active_api_shutdown|model_observer|gpu_unavailable)$' -count=1 -v -timeout=45m
go test ./tests/acceptance -run '^TestClientCompatibility$/^official-openai-(json|text)$' -count=1 -v -timeout=12m
go test ./tests/acceptance -run '^TestResidentMeasurements$' -count=1 -v -timeout=38m
# With the separate Docker Hub selection, no duplicate performance benchmark:
go test ./tests/acceptance -run '^TestResidentSuite$/(public_contract|lifecycle)$' -count=1 -v -timeout=25m
```

All workload commands ran through `rtk proxy` under one test-owned GPU lock, with
private logs and bounded cleanup. No test-owned acceptance containers remained;
operator containers/processes and model caches were preserved.

## Boundaries and evidence

Full P07 remains unchecked: unchanged-candidate counted HTTPS/cache/tiny-model
recipes, configured idle/retention/failure-memory envelopes, remaining reload/raw
output/overlap sensitivity mutations, concurrent measurements and feasible
standalone native comparison are not completed by these slices. Desktop
interoperability is excluded by the user's current scope, not reported as passed.
No P08/Kubernetes/stable invocation or candidate publication followed.

Private evidence: [baseline](<../../.local/p07/rc3-preflight/published-baseline.log>),
[GHCR server cases](<../../.local/p07/rc3-preflight/server-http.log>),
[SDK HTTP](<../../.local/p07/rc3-preflight/sdk-http.log>),
[measurement run](<../../.local/p07/rc3-preflight/resident-measurements.log>),
[measurement JSON](<../../.local/p07/run/published-981387209/resident-measurements.json>),
[Docker Hub cases](<../../.local/p07/rc3-preflight/dockerhub-http.log>),
[candidate binding](<../../.local/p07/run/published-981387209/candidate-binding-1117576114.json>),
[CPU/implementation receipt](<../../.local/p07/rc3-preflight/published-wiring-result.md>).
