# Container build parameters

The API and backend Dockerfiles declare pinned defaults through `ARG`. Build-time
versions, package pins and source checksums are not added to the container's
environment. Runtime `ENV` is reserved for the loader/tool settings that actually
need it. Docker documents the distinction and stage scope in
[build variables](https://docs.docker.com/build/building/variables/).

The approved default inventory is [build-inputs.json](../containers/build-inputs.json).
Defaults preserve the existing Linux amd64 toolchain, CUDA architecture coverage,
decoder/license profile, non-root identity and runtime paths.

## Argument groups

| Purpose | API | Backend |
| --- | --- | --- |
| Go compiler/base | `GO_VERSION`, `GO_BUILD_IMAGE` | Same |
| Runtime/build bases | `UBUNTU_IMAGE` | `CUDA_TOOLKIT_VERSION`, `CUDA_BUILD_IMAGE`, `CUDA_RUNTIME_IMAGE` |
| Native source | `FFMPEG_VERSION`, `FFMPEG_TAG`, `FFMPEG_COMMIT`, `FFMPEG_SHA256` | `WHISPER_REVISION`, `WHISPER_COMMIT`, `WHISPER_SHA256`, `WHISPER_SERVER_BLOB`, `WHISPER_SERVER_SHA256` |
| CMake/CUDA inputs | Not applicable | `CMAKE_VERSION`, `CMAKE_SHA256`, `CUDA_ARCHITECTURES`, `CUDA_EULA_SHA256` |
| Shared package pins | `CURL_VERSION`, `CA_CERTIFICATES_VERSION`, `CCACHE_VERSION` | Same |
| Build packages | `BUILD_ESSENTIAL_VERSION`, `PKG_CONFIG_VERSION`, `NASM_VERSION` | Supplied by pinned CUDA development base |
| Runtime security packages | Shared pins above | Also `OPENSSL_VERSION`, `GNUPG_VERSION`, `GCC_RUNTIME_VERSION` |
| Network budgets | `APT_RETRIES`, `FETCH_CONNECT_TIMEOUT`, `FETCH_MAX_TIME`, `FETCH_RETRIES` | Same |
| Compilation budgets | `BUILD_JOBS`, `BUILD_MEMORY_RESERVE_KIB`, `BUILD_MEMORY_PER_JOB_KIB` | Same |
| Runtime identity | `RUNTIME_UID`, `RUNTIME_GID` | Same |
| OCI labels | `SOURCE_REVISION`, `APP_VERSION`, `IMAGE_SOURCE`, `IMAGE_TITLE`, `IMAGE_DESCRIPTION`, `IMAGE_AUTHORS`, `IMAGE_VENDOR`, `IMAGE_URL`, `IMAGE_DOCUMENTATION`, `IMAGE_LICENSES` | Same; role-specific title, description and license defaults |

Global arguments are explicitly consumed in the stages that use them. Later
metadata/label arguments stay late to preserve earlier build caches. The backend
uses `CUDA_TOOLKIT_VERSION` to avoid collision with the NVIDIA base image's
existing `CUDA_VERSION` environment variable.

## Resource tuning

```sh
docker buildx build \
  --file containers/api.Dockerfile \
  --build-arg BUILD_JOBS=4 \
  --build-arg FETCH_RETRIES=5 \
  --tag whisper-api:local \
  --load .

docker buildx build \
  --file containers/backend.Dockerfile \
  --build-arg BUILD_JOBS=2 \
  --tag whisper-backend:local \
  --load .
```

Without `BUILD_JOBS`, the shared [job calculator](../containers/scripts/build-jobs.sh)
uses available CPUs, the readable cgroup CPU/memory limits and available host RAM.
It reserves 2 GiB and budgets 512 MiB per FFmpeg job or 2 GiB per CUDA job.
The automatic result is at least one job. An explicit positive `BUILD_JOBS`
overrides automatic sizing; choose it for the build host's actual memory budget.
Memory arguments use KiB. No helper or compiler is copied into the runtime image.

Go module/build caches and native ccache mounts retain their existing default
namespaces. Package indexes and archives are removed within the instructions
that create them. Readable heredoc blocks keep related work in the same layers;
multi-stage builds and `COPY --link` remain in use.

## Version and identity changes

Version overrides must include the corresponding immutable image digests and
source checksums. Changing a tag while retaining an old digest selects the old
image content; it does not upgrade it. The Go build explicitly checks its actual
compiler against `GO_VERSION`. Source archives, native server bytes and CUDA
license material are verified against the supplied SHA256 values.

`build.json` records effective build arguments. The copied `build-inputs.json`
remains the approved default inventory. Custom version/base/architecture builds
require an updated reviewed inventory and qualification before release; they
must not be represented as the approved defaults.

UID/GID defaults stay `10001:10001`; zero and malformed values are rejected.
Custom identities also require matching deployment security contexts and volume
permissions. Ports, health endpoints, exec entrypoints, native GPU requirements
and the decoder/license profile remain explicit service contracts.

## Registry metadata

Both images use the standard keys from [OCI Image Specification 1.1.1](https://github.com/opencontainers/image-spec/blob/v1.1.1/annotations.md).
The Dockerfiles provide source, revision, version, title, description, authors,
vendor, project URL, documentation URL and the complete redistributed-license
expression. API and backend titles, descriptions and licenses identify their
different contents. Defaults remain overridable through the arguments above.
The authors/vendor default identifies this project's contributors, without
inventing a personal contact address or claiming NVIDIA/upstream authorship.

CI uses SHA-pinned `docker/metadata-action` to add the real build creation time
in RFC 3339 format and fill the same metadata in image-config labels, runtime
manifest annotations and OCI-index annotations. One `IMAGE_METADATA` input feeds
both Action outputs; `docker/build-push-action` applies them to the qualified
export before SBOM/provenance verification. The source revision is the exact
checked-out CI SHA, including the tested merge revision for pull requests.
Documentation links point to that immutable source revision.

CI image versions are `0.0.0-dev+sha.<full-source-sha>`. Release tags and Helm
versions continue to come from the reviewed release inventory. Candidate/stable
promotion copies the accepted OCI bytes without rebuilding or rewriting image
metadata: the embedded version/date/revision describe the original build, while
registry tags identify the promoted release. No `latest` tag is published by the
metadata Action. OCI metadata is recorded in `image.json`; inspection rejects
conflicting values between image labels and corresponding annotations.

The same OCI bytes carry these fields into GHCR and Docker Hub. In particular,
GHCR uses the source link and index-level description/license annotations for
[package information](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry#labelling-container-images).
Registry web interfaces choose which fields to display. Docker Hub's repository
overview/README and account access/visibility are separate registry settings;
image labels do not configure them. No registry write is needed to inspect local
metadata.

Plain Dockerfile builds retain the static config labels. Supply a real creation
time with Docker's native `--label` when building locally:

```sh
docker buildx build \
  --file containers/api.Dockerfile \
  --build-arg SOURCE_REVISION="$(git rev-parse HEAD)" \
  --build-arg APP_VERSION=0.0.0-dev \
  --label "org.opencontainers.image.created=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --tag whisper-api:local \
  --load .

docker image inspect whisper-api:local --format '{{json .Config.Labels}}'

# Set IMAGE_REF to a published reference from the reviewed release inventory.
docker buildx imagetools inspect --raw "$IMAGE_REF"
```

The raw registry index exposes annotations without downloading CUDA layers.
The CI OCI export is authoritative for the full index and attestations;
`docker load`/local image inspection may expose only the runtime image config.
