# Operating Resident V1

This guide describes the checked-in service and deployment configuration. It is
not a new GPU acceptance report. Published artifacts, current source and historical
verification runs are different identities; do not transfer a result between them.

## Prerequisites and evidence

- Linux amd64; Docker Engine with Buildx and Compose for standalone operation.
- A compatible NVIDIA driver and container GPU integration. Only the backend gets
  a GPU; the API decodes audio with its packaged FFmpeg. CPU fallback cannot satisfy
  readiness. Build both images without GPU injection.
- Sufficient GPU/host memory for the selected model, persistent cache capacity,
  and disk-backed temporary storage. Compiled architecture coverage is not proof
  that every GPU/model combination works.
- For Kubernetes: existing NVIDIA device integration, an enforcing CNI, compatible
  storage, and an Ingress controller/TLS Secret only if Ingress is enabled. The
  chart does not install infrastructure or provide authentication.

[Build inputs](<../containers/build-inputs.json>) pin the executable sources and
CUDA 12.8.1 bases. Architecture coverage is defined by the
[core contract](<specs/gpu-whisper-server/core/contracts.yaml>) and applied in the
[backend Dockerfile](<../containers/backend.Dockerfile>); do not narrow it to the
build host. The current evidence must be read with these limits:

| Recorded environment | Evidence and limits |
| --- | --- |
| Docker, RTX 5070 Ti Laptop GPU | [rc.5 report](<verification/docker-rc5-p07-server.md>): driver 595.99.02, Docker 29.9.2, Compose 5.6.1; functional CUDA/SDK/lifecycle/cache checks, with accepted memory and speech/performance limitations. Not rc.6 qualification. |
| Kubernetes, RTX 2060 | [Development history](<verification/homelab-development.md>) and [rc.6 report](<verification/release-rc6.md>): real transcription and selected recovery/network/cache checks, but incomplete memory and continuous replacement/non-overlap proof. Not a complete P08 pass. |
| Helm compatibility | [Chart tests](<../tests/integration/chart_test.go>) use Helm 3.22.0/4.3.0 and a declared Kubernetes render matrix. Rendering is not cluster, CNI, storage or GPU acceptance. |

Performance figures in those reports describe their specific inputs, model,
parameters, samples and failures. There is no throughput, latency or quality SLO.
Security reviews/scans for rc.6 have expired; its images lack the working-tree
short-audio mitigation. The [v1.0.0 exception](<verification/release-1.0.0-notes.md>)
discloses these risks rather than converting missing checks into passes. Review the
[security policy](<../SECURITY.md>) before selecting an artifact.

## Prepare the canonical Compose stack

Run examples from the repository root. They start an evaluation service when you
choose to execute them; reading this guide does not authorize changes to an existing
deployment. Use only synthetic or public fixtures. Keep existing local files and
operator model/cache data intact.

For a **new** evaluation directory:

```sh
mkdir -p .local/compose/prepared
cp deploy/compose.env.example .local/compose/compose.env
```

The [example environment](<../deploy/compose.env.example>) selects the canonical
[JSON defaults](<../internal/platform/config/defaults.json>). Edit your local copy
to choose both images, a GPU and optional host port. Keep the loopback bind unless
you have separately provided network access control. In the same shell, define:

```sh
dc() {
  docker compose --env-file .local/compose/compose.env -f deploy/compose.yaml "$@"
}
```

This uses the canonical [Compose stack](<../deploy/compose.yaml>) and its
`whisper-server` project name. Use one consistent, unique `--project-name` in the
function if another stack already uses that name; project identity also selects
its named volumes.

Important distinctions:

- `--env-file` supplies **Compose interpolation**, not arbitrary container environment.
  Exported shell values can override those inputs. Do not pass the interpolation
  file as a service `env_file`: several of its keys are not application settings.
- Both services read the same mounted JSON via `--config`. Relative bind paths in
  the example resolve from the Compose file's directory, not the env-file directory.
- JSON `api.port`, `backend.port`, and `api.backendUrl` must agree with the Compose
  container-port/probe variables. The host publication port is independent.
- UID/GID `10001:10001` needs readable configuration/prepared files and traversable
  parent directories. Existing cache volumes must be writable by that identity.
  Do not solve permission failures with privileged/root containers.
- `/models` is the canonical persistent backend cache mount; changing JSON
  `storage.cacheDir` alone does not remap it. A different cache root needs an
  operator-owned Compose volume override with matching UID/GID 10001 permissions;
  this guide does not supply that custom recipe. Helm mounts its configured cache
  root independently. `/prepared` is a read-only host bind.
  Separate named `/tmp` volumes contain disposable work. Compose does not impose
  filesystem quotas on these volumes; application bounds are not disk quotas.
- Only the API port is published. The supervisor is on the Compose bridge and
  native HTTP is backend loopback. Host/Docker administrators and containers granted
  access to that bridge remain trusted. The bridge permits model-download egress.

## Select images

### Local source builds

These produce unpublished local images and require no build GPU:

```sh
docker buildx build --platform linux/amd64 -f containers/api.Dockerfile \
  --tag whisper-api:local --load .
docker buildx build --platform linux/amd64 -f containers/backend.Dockerfile \
  --tag whisper-backend:local --load .
export WHISPER_API_IMAGE=whisper-api:local
export WHISPER_BACKEND_IMAGE=whisper-backend:local
```

See [container build parameters](<container-builds.md>) for resource tuning and
identity rules. Local builds do not inherit qualification from published images.
Continue with [start and check](<#start-and-check>).

### Recorded rc.6 images: GHCR or Docker Hub

These exact image indexes are recorded in the [rc.6 report](<verification/release-rc6.md>).
They are reproducible evaluation inputs, **not a security-cleared recommendation**.
Read its expired-security and unpublished-mitigation warnings first. Choose one
registry; the report records the same indexes in both:

```sh
REGISTRY=ghcr.io
# Alternative: REGISTRY=docker.io
export WHISPER_API_IMAGE="$REGISTRY/swimmwatch/openwhispr-whisper-server-api@sha256:c309f5dfa0b3f03857cb4ac3077dd39ed1b947fd1f5217637eae19316a6ab636"
export WHISPER_BACKEND_IMAGE="$REGISTRY/swimmwatch/openwhispr-whisper-server-backend@sha256:45957d1a1c171557399ee313c4cdf6f385801104cd932c644bfe71788c027467"
dc config --quiet
dc pull
```

Never mix the API from one inventory with an unrelated backend. To select another
release, use its actual immutable inventory rather than guessing a tag or using
`latest`. Publication/read-back is distinct from runtime qualification. Continue
with the common startup sequence below; no rebuild is needed.

## Start and check

After preparation and **one** image-selection recipe:

```sh
dc config --quiet
dc up -d
dc ps
curl --fail --silent --show-error --max-time 2 http://127.0.0.1:8080/health/live

# Linux timeout bounds waiting; adjust the URL if you changed the host port.
timeout 2500s sh -c 'until curl --fail --silent --max-time 2 http://127.0.0.1:8080/health/ready; do sleep 2; done'
```

If the wait fails, stop here and inspect diagnostics. This wait is a convenience
for the default startup budgets, not a performance target. A healthy API process
can be unready while the model downloads, loads or recovers. Compose healthchecks
can also show unhealthy during a long first provisioning; container startup alone
is not success. `config --quiet` validates Compose, not the mounted application's
JSON or actual mount permissions.

Once readiness returns `{"status":"ready"}`, save a short synthetic or public
speech recording as `sample.wav` and call the **public** endpoint:

```sh
curl --fail-with-body --max-time 620 http://127.0.0.1:8080/v1/audio/transcriptions \
  -F 'file=@sample.wav' -F model=base -F language=auto -F temperature=0 \
  -F response_format=json
```

Success returns `{"text":"..."}`; `response_format=text` returns plain UTF-8 text.
The example client timeout is not the server's queue/operation deadline. Do not add
blind inference retries: cancellation or a lost reply may follow actual execution.
For a changed model, use its configured served ID. The
[OpenAPI contract](<specs/gpu-whisper-server/contracts/openapi.yaml>) owns all fields,
status codes, payload/duration limits and error envelopes. Supported audio families
include WAV, MP3, MP4, MPEG, MPGA, M4A, OGG, FLAC and WebM, including Opus; content is
decoded rather than trusted by filename. Duplicate/unknown fields and `stream=true`
are rejected. Public `/load`, `/inference`, model management and metrics are absent.

## Configuration, prepared files and offline cache

Make a local JSON copy before editing defaults; point `WHISPER_CONFIG_FILE` at its
absolute path, or at a path relative to the Compose file. Both containers must use
the same intended settings. Application precedence is explicit CLI > environment >
JSON > defaults; collections replace. Every source is validated, so an invalid lower
layer is not repaired by an override. See [configuration bindings](<../internal/platform/config/settings.go>)
and the [core contract](<specs/gpu-whisper-server/core/contracts.yaml>).

`models.catalog` declares artifacts; `models.downloadSet` selects artifacts to
prepare; `models.served` selects exactly one model for requests. Download-only models
are not served. A different request model ID never loads or switches models.

### Prepared model, with no implicit download

Place an operator-verified Whisper GGML model in the prepared directory. For the
canonical `base` artifact, name it `base.bin` and verify the
[canonical SHA256/size](<../internal/platform/config/defaults.json>). The file and
its parents must be readable by UID 10001. Do not copy private files into the image.
Use this complete minimal JSON as your local application configuration:

```json
{
  "models": {
    "catalog": [{
      "id": "base",
      "source": {
        "kind": "prepared",
        "path": "/prepared/base.bin",
        "sha256": "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe",
        "expected_bytes": 147951465
      }
    }],
    "served": "base",
    "downloadSet": []
  }
}
```

Other settings retain defaults. Select the local configuration in the interpolation
file (or an exported `WHISPER_CONFIG_FILE`), then use the same image-selection and
[start/check](<#start-and-check>) sequence. For an existing stack, use the recreation
sequence below. Missing/corrupt prepared files fail provisioning; there is no fallback
download. Prepared files are never modified or deleted by request cleanup.

### Previously populated cache, offline restart

First complete an online start with the default download configuration. Keep the
same project name, named model-cache volume and model source/checksum. To require
cache-only operation, use a local JSON file containing:

```json
{"models":{"downloadSet":[]}}
```

The unchanged default catalog/served ID still selects `base`, but missing or corrupt
cache now fails rather than downloading. Recreate both containers with this config,
then repeat readiness and public transcription. Existing images must already be
available when offline. A successful request alone does not prove zero source
traffic; that stronger claim requires the owning acceptance suite's source-boundary
observations. Do not delete the cache to troubleshoot a startup failure.

## Restart, change and stop

For unchanged configuration, `dc restart` restarts the pair. For changes to image,
JSON, environment or model, stop and recreate **both** components:

```sh
dc config --quiet
dc stop
dc up -d --force-recreate
```

Repeat readiness and a public request. A failed replacement stays unready rather
than serving a different model. Restore the previous configuration and image
identities, then repeat this same controlled replacement to recover.

Do not reduce the stop grace below shutdown + cleanup + 5 seconds (default 45s).
The API rejects new work and cancels waiting work on shutdown, with bounded active
drain. Uncertain active cancellation requires native stop/reap before replacement;
normal successful requests keep the same resident model. Model reload may take
longer than request cleanup, during which readiness remains false.

For routine teardown:

```sh
dc down
```

No `--volumes`, cache deletion or prepared-file removal is part of routine teardown.
Named work volumes may also persist; startup clears only service-owned abandoned
work. Intentional cache disposal is a separate operator decision after identifying
the exact owned volume, its users and backups. See Docker's
[restart](https://github.com/docker/compose/blob/v5.6.1/docs/reference/compose_restart.md),
[recreation](https://github.com/docker/compose/blob/v5.6.1/docs/reference/compose_up.md)
and [volume-retention](https://github.com/docker/compose/blob/v5.6.1/docs/reference/compose_down.md)
semantics.

## Diagnose without exposing content

```sh
dc ps
dc logs --tail 100 api backend
curl --include --max-time 2 http://127.0.0.1:8080/health/ready
```

Review logs locally before sharing. Go logs are structured JSON with safe event/error
codes and `X-Request-ID` correlation; raw native/decoder output is not a public log
stream. Do not upload configuration dumps, credentials, private paths, client audio,
prompts or transcripts. The service stores no transcript database, but Resident V1
permits bounded latest-result/native working state in RAM/VRAM; immediate secure
zeroization is not promised.

| Symptom | Next check |
| --- | --- |
| `configuration_rejected` / process exits | Unknown/duplicate settings, model references, strict types and port agreement; do not relax validation. |
| `provisioning_failed` | Mounted-file readability, expected checksum/size, cache write access and HTTPS egress when downloads are selected. |
| `gpu_unavailable`, recovery, or backend unavailable | Selected device, driver/runtime compatibility, model fit and backend lifecycle; do not accept CPU results as a fix. |
| `429 rate_limit_exceeded` | Wait at least `Retry-After`; this is per-IP admission, before upload/queue work. |
| `429 queue_full` | Waiting capacity was exhausted; work was not admitted. No refill time is promised. |
| `503 rate_limit_unavailable` | Limiter storage failed closed; this is not a quota denial. |
| `408 operation_timeout` | Queue plus execution budget elapsed; queued work never ran, active work may have begun. |

The in-memory rate limiter uses per-process token buckets, not a distributed quota.
Restart resets it; each API replica has its own store and clients behind one IP
share a bucket. Its idle cleanup is not a hard active-IP cap or arbitrary flood protection.
The default uses the direct peer address. Trust `X-Forwarded-For` only with explicit
operator-confirmed proxy CIDRs and a proxy that sanitizes forwarded headers; TLS
and forwarded-IP configuration are not authentication.

## Kubernetes, upgrades and removal

Use the [chart guide](<../charts/whisper-server/README.md>) for exact-version chart
selection, explicit target flags, image overrides, paired resources and retained
claims. Both StatefulSets replace without surge, so a one-GPU update has downtime.
Protocol/model-fingerprint/generation checks reject incompatible pairs; they are
not authentication, artifact verification or continuous GPU non-overlap evidence.

A failed unready StatefulSet revision can require operator intervention after
rollback. First inspect the exact owned Pods and their revisions; do not force-delete
processes or assume API object disappearance proves native/GPU work stopped. Keep
previous image/configuration identities and valid cache. Chart uninstall retains
cache claims and operator-provided storage; it does not authorize PVC/namespace
removal. Cluster installation, acceptance workloads and storage disposal remain
separate operator actions.
