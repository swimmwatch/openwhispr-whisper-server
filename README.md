# OpenWhispr Whisper Server

A self-hosted, synchronous speech-to-text API for NVIDIA GPUs. A Go/Echo API
validates and decodes uploads, then calls a separate Go supervisor running the
unchanged [OpenWhispr/whisper.cpp](https://github.com/OpenWhispr/whisper.cpp) HTTP
server. Two container images and one Helm chart implement **Resident V1**:
one configured model stays loaded between requests.

> **Qualification is incomplete.** Recorded rc.6 publication and the release-only
> v1.0.0 exception are not full GPU, memory, security or documentation acceptance.
> Security evidence has expired, and the selected rc.6 images do not contain the
> working-tree short-audio mitigation. Read the
> [release limitations](<docs/verification/release-1.0.0-notes.md>) and
> [rc.6 results](<docs/verification/release-rc6.md>) before evaluating these artifacts.
> This README does not certify a production deployment or current security status.

**There is no built-in authentication.** Anyone who can reach the public API can
transcribe. Keep it on loopback or a restricted network; provide access control and
TLS outside the service before exposing it more widely.

## What it does

```text
Client → public API :8080 → private supervisor :8081 → native server 127.0.0.1:8082
         upload/FFmpeg      model cache + lifecycle     one CUDA-resident model
         one FIFO queue
```

- `POST /v1/audio/transcriptions`: multipart audio, with JSON or plain-text results.
- `GET /health/live` and `GET /health/ready`: independent process and serving health.
- One active inference per pair; bounded uploads, decoding, waiting work and deadlines.
- Pinned HTTPS model downloads or read-only prepared files; persistent verified cache.
- Separate API/backend containers; Kubernetes uses matching StatefulSet ordinals,
  backend-only GPU allocation and per-pair NetworkPolicy.

This is an OpenAI-compatible **transcription subset**, not the full OpenAI API.
Streaming, translation, model listing/switching, warmup/unload, CPU fallback,
authentication and metrics endpoints are not implemented. SDK checks do not prove
OpenWhispr desktop application interoperability. See the
[OpenAPI contract](<docs/specs/gpu-whisper-server/contracts/openapi.yaml>).

## Local evaluation with Docker Compose

Requirements: Linux amd64, Docker with Buildx/Compose, and an NVIDIA driver/container
runtime compatible with the pinned CUDA build. **Building needs no GPU; running the
backend does.** Review [runtime prerequisites and evidence](<docs/operations.md#prerequisites-and-evidence>)
first. These are source-build instructions, not a claim that this checkout has
passed either GPU qualification suite.

From the repository root, for a new local evaluation directory:

```sh
mkdir -p .local/compose/prepared
cp deploy/compose.env.example .local/compose/compose.env

docker buildx build --platform linux/amd64 -f containers/api.Dockerfile \
  -t whisper-api:local --load .
docker buildx build --platform linux/amd64 -f containers/backend.Dockerfile \
  -t whisper-backend:local --load .

docker compose --env-file .local/compose/compose.env -f deploy/compose.yaml config --quiet
docker compose --env-file .local/compose/compose.env -f deploy/compose.yaml up -d

curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/health/live
curl --include --max-time 2 http://127.0.0.1:8080/health/ready
```

The example selects GPU `0`, publishes only `127.0.0.1:8080`, and downloads the
pinned multilingual `base` model on first start. A running container or liveness
response does not establish readiness. Wait for readiness to return HTTP 200;
[the operator guide](<docs/operations.md#start-and-check>) gives a bounded wait and
failure diagnostics. Do not overwrite an existing local configuration blindly.

Save a short synthetic or explicitly public speech recording as `sample.wav`,
then submit it **only after readiness succeeds**:

```sh
curl --fail-with-body --max-time 620 http://127.0.0.1:8080/v1/audio/transcriptions \
  -F 'file=@sample.wav' -F model=base -F response_format=json
```

Success is `{"text":"..."}`. Use `response_format=text` for UTF-8 plain text.
The `model` must match the configured served ID; it never triggers a model switch.
Do not publish private recordings or transcripts when reporting results.

Stop without deleting models:

```sh
docker compose --env-file .local/compose/compose.env -f deploy/compose.yaml down
```

Do **not** add `--volumes`. See [operations](<docs/operations.md>) for immutable
GHCR/Docker Hub references, prepared/offline models, configuration changes and
recovery. The [chart guide](<charts/whisper-server/README.md>) covers Kubernetes.

## Configuration and lifecycle

The [canonical defaults](<internal/platform/config/defaults.json>) and
[core contract](<docs/specs/gpu-whisper-server/core/contracts.yaml>) are authoritative.
Explicit CLI settings override environment, then JSON configuration, then defaults;
arrays replace rather than append. Unknown/deferred settings fail startup.
Compose interpolation is not application environment injection.

Successful requests reuse the loaded model. Cancellation after native submission,
or uncertain native state, requires bounded stop/reap before reload; readiness is
false during recovery.
Resident mode retains model RAM/VRAM and may retain bounded native working state and
the latest result; it does not promise immediate secure erasure after each response.
API-owned request resources and disposable files have separate cleanup ownership.
Single-GPU upgrades have downtime; model-cache retention is not a backup strategy.

## Documentation and contributing

- [Documentation index](<docs/README.md>) — operator, developer and evidence navigation.
- [Contributing](<CONTRIBUTING.md>) — local checks and review process.
- [Support](<SUPPORT.md>), [private security reporting](<SECURITY.md>) and
  [code of conduct](<CODE_OF_CONDUCT.md>).
- [Changelog](<CHANGELOG.md>) and [release automation](<docs/releases.md>).

## Future work

These are deferred requirements or proposals, not delivered features:

- `ram-cache`: keep the last model's reusable RAM weights, release GPU allocations
  after processing; `on-demand`: release model RAM/VRAM while retaining disk files.
- Per-request model selection and policies, with safe release-before-replacement;
  FIFO-coordinated, idempotent warmup/unload and observed allocation reclamation.
  Warmup would load weights without synthetic inference. A native extension or fork
  remains a likely design investigation, not an existing proxy capability.
- Authentication/authorization and access administration; a proposed lightweight
  admin UI; Prometheus metrics. Distributed rate-limit storage is also not V1.

[REQ-022](<docs/specs/gpu-whisper-server/spec.md#req-022-required-development-after-v1>)
retains the detailed selection, memory and lifecycle semantics. No delivery date
is promised.

## License

Project-owned code and chart: [Apache-2.0](<LICENSE>). Packaged upstream, FFmpeg,
CUDA and other components retain their own licenses and redistribution obligations;
see [third-party notices](<THIRD_PARTY_NOTICES.md>).
