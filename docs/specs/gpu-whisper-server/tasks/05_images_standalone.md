# P05 — Two images and private standalone Docker operation

Outcome: actual GPU-free builds containing the agreed binaries and unchanged CUDA server, with reusable Docker Compose deployment and example inputs.
Requirements: REQ-001, REQ-002, REQ-015, REQ-019, REQ-024, REQ-026.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete after the user-authorized cgroup CPU-quota correction (2026-10-23). The regression reproduced the defect before the fix; all ten resource cases, both rebuilt images and the full race/image/standalone suite passed afterward. Current identities and evidence are in the CPU-quota correction below; earlier image identities are historical. Real GPU, hosted CI/security and publication remain separate work.

Prerequisites: P01–P04 checks pass; Linux amd64 builder with Docker/BuildKit and authorized dependency/source network access. Read core/contracts.yaml runtime/deployment/configuration.transient_storage/delivery.pipeline; quality/scenarios.yaml Q-PACKAGING/Q-PRIVACY/Q-RELEASEBUILD; spec.md:27, :35, :182, :224. Re-verify the pinned upstream/FFmpeg sources and base-image identities before packaging.

## Scope and ownership

Allowed: containers/api.Dockerfile, containers/backend.Dockerfile, refinement of I01's .dockerignore, deploy/compose.yaml, deploy/compose.env.example, narrowly scoped image/composition contract tests under tests/integration/** and non-sensitive build/license identity evidence. Publication and CI/release workflow implementation belong to C02/R01/R02; GPU acceptance and cluster deployment are separate packets.

Build the Go/Echo API and Go supervisor at the core baseline. API image contains the selected FFmpeg 9.0.2 release; backend image contains the supervisor plus unchanged pinned whisper-server compiled with CUDA 12.8. Use source-compatible CUDA architecture list from core and disable GGML_NATIVE/native discovery. Do not patch native source or copy upstream's narrower 75/80/86/90 list. Choose matching NVIDIA CUDA builder/runtime images, pin their real registry digests and required dependencies/CMake/source checksums; record immutable build identities for both images. Signed/source identity verification must be real, not a made-up checksum. Toolchain drift that changes GPU coverage returns to planning before release.

Multi-stage runtime includes only needed executable/libraries/not source credentials. Preserve licensing/provenance for packaged upstream/FFmpeg. Both processes use non-root UID, read-only root, writable configured cache/disposable /tmp, bounded diagnostics and PID/signal ownership. API receives no GPU. Native port stays loopback and only supervisor private port is available on the backend network. Backend decoder dependencies mean native WAV support; FFmpeg conversion is owned by the API, not --convert.

Check actual dependency/base redistribution obligations (including selected CUDA image and FFmpeg build options); update THIRD_PARTY_NOTICES.md/LICENSE material without applying Apache-2.0 to dependencies. Add truthful OCI source/revision/license metadata. Verify .dockerignore excludes .git, agent state/config/secrets, local audio/model weights and output while retaining build inputs/license files. C02 generates SBOM/provenance and release clean-build evidence; no mutable latest base or fabricated digest. Q-RELEASEBUILD also consumes this packet's checks.

Compose deploys separate API/backend containers, public 8080 only, a dedicated private network, model cache/prepared-file mounts and backend GPU selection. Prepared files read-only, transient volumes disposable/bounded; operator cache remains after container removal. Changing model/config recreates the pair with the new settings, never requiring an image rebuild.

### Reusable Compose inputs

Provide one canonical stack and a non-secret environment example. Its supported
inputs select both image references, local public-port binding, exactly one GPU,
and the existing application configuration/cache/prepared-model paths. Reference
the core settings schema rather than introducing a second model/configuration
format. Keep downloaded-cache and prepared-file recipes expressible through these
inputs and the same service definition; do not create parallel stack definitions.
Published-image inputs are rendered here but are not pulled before R01. Local
builds use the same stack with explicit local image identities.

For the local recipe, bind public 8080 to loopback; explain deliberate external
binding in P09 because V1 has no authentication. Publish no supervisor/native
host port, allocate no GPU to the API, and preserve model-download egress where
needed. Use NVIDIA reservations with capabilities gpu and either one device_ids
entry or count 1, never both. Preserve non-root/read-only execution and ensure
host/named-volume permissions work without granting root to the service.

Compose healthchecks use actual bounded probes, not synthetic inference. Startup
ordering does not replace public /health/ready or safe dependency-loss handling.
Both stop_grace_period values must cover configured shutdown + cleanup + 5 seconds
(at least 45 seconds at baseline); ordinary down preserves model cache. Record
tested Docker Engine/Compose plugin versions; do not advertise untested versions.
P07 tests the actual lifecycle and P09 writes final runnable instructions.

## Check-first acceptance

Create TestImageContract/TestStandaloneWiring before Dockerfiles/composition. Run their missing-artifact RED, then PROVE any structural assertion that RED did not exercise. Check GPU-free builds, actual binary/dependency identities, image UID/root/mount/listener/GPU metadata and private networking rather than just Dockerfile text.

Create TestComposeConfiguration before the Compose inputs. Resolve the exact
configuration through the real Compose CLI for local images, both registry
overrides, a prepared-file mount and populated-cache mode. Assert two services,
both selected image references, correct configuration injection, only the public
API binding, backend-only GPU, read-only prepared files, persistent model cache,
bounded transient mounts and adequate stop grace. Test paths from the documented
working directory; a .env interpolation value alone is not application injection.
Rendering is structural evidence only; offline source-access and real GPU success
remain P07 checks. Pin published references only after real release identities exist.

Planned commands:

```bash
rtk proxy docker build --platform linux/amd64 -f containers/api.Dockerfile -t whisper-api:local .
rtk proxy docker build --platform linux/amd64 -f containers/backend.Dockerfile -t whisper-backend:local .
rtk proxy docker compose --env-file deploy/compose.env.example -f deploy/compose.yaml config --quiet
rtk proxy env P05_IMAGE_TESTS=1 go test ./tests/integration -count=1 -v
```

Run builds in a clean builder without GPU injection; record no GPU requirement. Inspect compiled CUDA artifacts/compile flags for every declared architecture; build success on a GPU machine alone is insufficient. Use a controlled non-GPU unavailable-startup run to verify truthful readiness and safe exit; actual success CUDA transcription is P07.

PROVE by substituting an omitted architecture, root UID, leaked build marker/credential-shaped fixture or an exposed native port, then restore and show owning checks fail/pass. No real secrets in images/test fixtures. A broken dependency/image contract stays incomplete; fix implementation rather than weakening checks.
## Completion

Run focused checks, appropriate full Go tests/vet and final pinned specification freshness check. Record exact results and mutations; required unavailable/failed checks keep this packet incomplete. Update this packet, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop. No automatic following packet, review, commit or publication.

## Technical proposal

Build exactly two Linux amd64 multi-stage images: containers/api.Dockerfile contains cmd/api plus the pinned FFmpeg decoder; containers/backend.Dockerfile contains cmd/backend, the unchanged pinned whisper-server and required CUDA libraries. Bind ../core/contracts.yaml runtime/api/backend/decoder, configuration/transient_storage, deployment/security and delivery/images/pipeline. Preserve Go 1.27.3, CUDA 12.8, FFmpeg n9.0.2, pinned native revision and every declared CUDA architecture; disable native host detection and require a GPU-free build. Do not adopt an upstream sample's narrower coverage or move conversion into native --convert.

Use glibc-compatible Linux runtime bases, not scratch/Alpine; resolve official compatible builder/runtime tags to verified immutable digests during execution, record source checksums and actual linked libraries/licenses before building. This is identity resolution within fixed baselines, not authorization to upgrade versions. If a baseline is unavailable or incompatible, stop and return that choice to planning/specification. No new service module or inference library. Keep dependency caching before source COPY, explicit non-root UID/GID, exec entrypoints, read-only root and only contract-approved writable mounts. Bundle needed license/notices; inspect .dockerignore and OCI metadata against real built contents.

One canonical deploy/compose.yaml and non-secret deploy/compose.env.example configure both image identities, existing canonical application config files, cache/prepared mounts, loopback public API port and exactly one backend GPU. Choose count:1 by default or one device_ids entry, mutually exclusive; capabilities:[gpu] is mandatory. A private service network keeps supervisor unexposed; native HTTP stays backend-loopback; permit required download egress. Runtime app config is mounted/injected explicitly, not assumed from Compose interpolation.

Retain operator-owned model cache across down/recreate, mount prepared files read-only, keep bounded disposable /tmp separate, and verify UID permissions without root workarounds. Both services stop within configured shutdown+cleanup+margin; default at least 45 seconds. Use executable bounded probe commands/healthchecks, never warmup inference; container running alone is not readiness. Config changes recreate the pair; image rebuild is not needed for model changes.

P05 owns integration test harnesses under tests/integration for actual image contents/binary versions/libraries/architectures, non-root/permissions/signals, absence of secrets and private listeners. Resolve Compose for local builds and both registry overrides, prepared/offline selections and writable/transient mounts. Run both real builds without GPU, real Compose config and controlled unavailable-GPU startup; rendering cannot pass CUDA acceptance. Test-first checks plus reversible missing-architecture/root/exposed-port/leaked-marker mutations. Q-PACKAGING/Q-PRIVACY/Q-RELEASEBUILD apply; C02 adds full scans, R01 publishes and P07 proves actual GPU runtime. No publication or cluster mutation in P05.

## Technical agreement

Selected proposal: `p05-images-compose-v1`. Packet: `P05`.

- proposal_digest: `a62f3277128cee4446eb561cfb74f75c15c5f8f8221fc34c81e3a26ae11aa6d6`
- agreement_ref: `{id: 885e513d-7ef8-4025-94e9-e772d3b35472, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Verify Linux builder/tool/network prerequisites and immutable source/base/license identities within fixed baselines.
2. Write image/wiring contract checks, observe RED, then build both actual multi-stage images without GPU.
3. Add the one Compose stack and non-secret inputs; resolve every registry/cache/prepared/configuration case.
4. Inspect executable/library/CUDA/UID/mount/listener/secret properties and bounded unavailable-GPU startup.
5. Apply structural PROVE mutations, restore and rerun focused image/Compose checks; leave runtime CUDA to P07.

## Implementation evidence — 2026-10-23

Implemented `containers/api.Dockerfile`, `containers/backend.Dockerfile`, the
resolved identity inventory `containers/build-inputs.json`, and one canonical
`deploy/compose.yaml` with non-secret `deploy/compose.env.example`. Image and
standalone checks live in `tests/integration/packaging_test.go` and
`tests/integration/standalone_test.go`. `.dockerignore`, third-party notices and
the unreleased changelog were updated. Production Go code, dependency versions,
native source and public contracts were not changed by P05.

Both Linux amd64 images built without GPU injection. The API contains Go 1.27.3
and FFmpeg n9.0.2, built with shared LGPL libraries, GPL/nonfree/autodetection and
network support disabled. Its runtime retains exact corresponding source,
licenses and build metadata. The backend contains the Go supervisor and unchanged
pinned native server built with CUDA 12.8.1 and CMake 3.31.10. Official base-image
digests and downloaded source/license checksums are recorded in the build-input
inventory; the CMake archive also matches its publisher checksum. This is not a
claim of signature verification.

Actual compiled CUDA artifacts and saved CMake settings cover every contracted
target: `50-virtual;61-virtual;70-virtual;75-virtual;80-virtual;86-real;89-real;90-virtual;120a-real`,
with `GGML_NATIVE=OFF`. GPU-free library inspection permits only the host-driver
dependency `libcuda.so.1` to be unresolved; required toolkit/runtime libraries
resolve. Both images use UID/GID `10001:10001` and exec entrypoints.

Local image identities, not published release references:

| Artifact | Identity |
| --- | --- |
| `whisper-api:local` | `sha256:59becf0b9d16f8fcc40923d4db685df33079bc08e7412770b448f5b98a7f6c6c` |
| `whisper-backend:local` | `sha256:b52a0c0172b4db9d59a907e00f6ee9a3fb56823b49c2f2a09d440fbc578d13e1` |
| Build source-tree revision | `sha256:7c2e2e6de08b114262de03fee5faae7e2aecf2373723f221f51dee1462fbb000` |

The revision label identifies actual uncommitted build inputs, not a fabricated
Git commit. Image sizes are approximately 301 MB and 6.65 GB respectively.
Tested tooling: Docker Engine 29.9.2, Compose 5.6.1 and Buildx 0.38.1.

Compose resolves local, GHCR and Docker Hub image selections, prepared-file and
populated-cache settings, explicit application configuration mounts and changed
stop budgets. Only the API is published, on loopback; only the backend receives
one selected GPU. Prepared/configuration files are read-only. Model cache survives
container removal. Separate disk-backed scratch volumes use existing application
byte/count bounds; no filesystem quota or RAM-backed replacement is promised.
An actual Compose networking check uses a non-GPU helper in place of the native
GPU process: the API reaches the private backend and an unrelated bridge cannot.
This helper does not establish CUDA runtime correctness.

Final commands, run from the repository root using Go 1.27.3, passed:

```bash
rtk proxy env P05_IMAGE_TESTS=1 go test ./... -count=1
rtk proxy env P05_IMAGE_TESTS=1 go test -race ./... -count=1
rtk proxy go vet ./...
rtk proxy gofmt -l cmd internal tests/integration
rtk proxy python3 tools/check_go_architecture.py
rtk proxy python3 tools/check_contracts.py
rtk proxy python3 tools/check_agents.py
rtk proxy .local/c01/venv/bin/python -m unittest discover -s tests/tooling -v
rtk proxy docker compose --env-file deploy/compose.env.example -f deploy/compose.yaml config --quiet
```

The tooling suite passed all 20 tests using the pinned checksum-verified Trivy
0.75.0 executable under `.local/p05/bin`. Its historical database is not fresh
vulnerability evidence; full current scans belong to C02. Image checks inspect
actual compiler/module identities, libraries, source/license bytes and every
regular file in all image layers with a bounded streaming marker scan. Runtime
checks cover real 8 kHz WAV to 16 kHz PCM conversion, read-only/non-root startup,
bounded probes, SIGTERM, prepared/cache startup to the unavailable-GPU boundary
and cache retention.

All six isolated PROVE mutations failed at their intended assertions: root
UID/GID, a private layer marker, a marker beyond 1 MiB, omitted `89-real` CUDA
coverage, a runtime build-tool executable and published backend/native ports.
The large-marker case exposed and corrected the original scan's file-size gap.
Mutant images/definitions were removed; final qualifying images were unchanged.
Earlier build/network/link-check failures and configuration-test control
collisions were corrected before the final successful builds and full runs.
Test controls now use `P05_*`, outside the strict application's `WHISPER_*` prefix.

Private logs and identities remain in `.local/p05/evidence/`, including
`artifacts.json`, `source-tree.json`, `final-full-2.log`, `final-race-2.log`,
`final-vet-2.log`, `tooling-2.log` and `prove-summary.json`. Final specification
and exact P05 agreement checks remain pass/ready. Actual GPU transcription,
performance/memory evidence, Helm/Kubernetes, hosted CI/security qualification,
publication and final reader documentation remain with P06/C02/R01/P07/P08/P09/R02.

## Configured-port correction — 2026-10-23

The P05 review reproduced a valid `api.port=18080` listener with an obsolete
8080 Compose target/probe. The user explicitly selected P05 rework rather than
P06. `deploy/compose.yaml` now uses `WHISPER_API_CONTAINER_PORT` for its API
target/probe and `WHISPER_BACKEND_CONTAINER_PORT` for its private backend probe.
The example inputs explain that these must match `api.port` and `backend.port`
in the mounted JSON; `api.backendUrl` must address that backend listener.
`WHISPER_API_PORT` retains its existing independent host-publication meaning.
Compose interpolation does not modify or parse the application JSON. Default
8080/8081 wiring is unchanged; no configuration loader, wrapper or dependency
was added.

Test-first RED failed at the wrong API target for all three image selections and
at the actual missing 18080 publication. GREEN passed six Compose render cases,
including custom listeners and independent host port 28080, plus actual canonical
Compose network checks at 8080/8081 and 18080/18081. Each runtime case verifies
host-to-API `/health/live` returns 200, API-to-private-backend reachability and
denial from an unrelated network. The backend remains a non-GPU helper in this
network test; no CUDA inference is claimed. Existing standalone startup/cache
checks also passed.

Three isolated PROVE files restored the obsolete API target, API readiness URL
or backend health URL one at a time. The owning render assertions rejected all
three; temporary mutation files were removed. Evidence is retained privately in
`.local/p05/evidence/ports-{red,green,prove-api-target,prove-api-probe,prove-backend-probe}.log`.

Final rework checks passed from the repository root with the pinned Go 1.27.3:

```bash
rtk proxy go test ./... -count=1
rtk proxy env P05_IMAGE_TESTS=1 go test -race ./tests/integration -run '^(TestComposeConfiguration|TestStandalonePrivateNetwork|TestStandaloneWiring|TestStandaloneCachedStartup)$' -count=1 -v
rtk proxy go vet ./...
rtk proxy gofmt -l tests/integration
rtk proxy python3 tools/check_go_architecture.py
rtk proxy python3 tools/check_contracts.py
rtk proxy python3 tools/check_agents.py
rtk proxy docker compose --env-file deploy/compose.env.example -f deploy/compose.yaml config --quiet
```

Logs: `ports-full-go.log`, `ports-race.log` and `ports-vet.log` in the same private
evidence directory. The first architecture invocation lacked the pinned Go PATH
and failed to obtain the import graph; it passed with that environment restored.
The full Go run skips opt-in image execution; the focused race run explicitly
enables the affected runtime checks. All recorded image-build source hashes
remain unchanged, so prior image/build/license/CUDA evidence and local image IDs
are reused without rebuilding or rescanning identical artifacts. Frozen technical
commitments, public contracts, service code and image definitions are unchanged.

## Build-pipeline correction — 2026-10-23

Actual C02 Hadolint found five DL4006 findings in these Dockerfiles. The user
explicitly authorized this narrow P05 rework ("Давай исправляем"). Both images
now select `SHELL ["/bin/bash", "-euo", "pipefail", "-c"]` in all three stages.
No production Go, native source, dependency version, CUDA architecture or Compose
setting was changed. Hadolint 2.15.1 reports no findings, without suppressions.

Twelve real GPU-free Docker build probes cover all six stages before and after
the correction. A valid pipeline succeeds before each failing probe. The legacy
shell masks `false | true` with exit 0; every corrected stage rejects it with
exit 1. This verifies pipeline failure propagation rather than Dockerfile text
alone. The probes leave no production source mutation.

Both images rebuilt without GPU injection and retain the full contracted CUDA
architecture matrix. Current local identities supersede the earlier image table:

| Artifact | Identity |
| --- | --- |
| `whisper-api:local` | `sha256:07a85d092ffb1ac1f74e46528e050795fb66efeeef992d48c701cf3017c6da36` |
| `whisper-backend:local` | `sha256:588b193cf039bfabbc30a093f12b4a3d826920f8a50153f1aff03a349765a096` |
| Build source-tree revision | `sha256:80f6403cd90f1ecf751cdb7858f8d38afa37b6bf27af4c717864ffffedfeb02d` |

Both OCI revision labels match the source-tree identity. The 45 recorded build
inputs were rechecked unchanged. The first attached backend build ended with
SIGTERM/exit 143 after roughly 30 minutes, without a compiler error in its log;
the cause is unconfirmed. A bounded task-owned retry of the same command passed
in approximately 51 minutes. Original and retry logs are retained.

The complete `P05_IMAGE_TESTS=1 go test -race ./... -count=1` run passed with
Go 1.27.3 and the configured Python environment; integration checks took 338.275
seconds. It inspected both actual images' executables, licenses, libraries,
compiled CUDA coverage and layers, and reran Compose/private-network,
non-root/read-only startup, probe, shutdown and cache-retention checks. The
separate API image-contract race run also passed. `go vet ./...`, architecture,
contract and agent structural checks, and canonical Compose `config --quiet`
passed. Unchanged chart execution and real CUDA inference were not repeated;
runtime GPU qualification remains P07/P08.

Private evidence is `.local/p05/pipefail-fix/`: `artifacts.json`,
`source-tree.json`, image inspections, before/after Hadolint reports,
`pipeline-checks.json` and its twelve build logs, both image-build logs and the
retained interrupted attempt, `backend-worker-status.json`, `api-image-race.log`,
`final-race.log`, vet and structural-check logs. No release or complete Security
Check is claimed. C02's eight gosec findings and unfinished integration remain
outside this completed correction; C02 needs a separate invocation.

Final specification freshness returned `pass` with Approved/reviewed status;
the exact P05 agreement returned `ready` with no issues. Pins above are unchanged.
Results are saved privately in `final-freshness.json`. P05 is complete; no review,
commit, push, publication, deployment or automatic C02 resume was performed.

## Build optimization and comparison — 2026-10-23

The user explicitly requested faster cached and parallel builds of both images,
with measurements before and after. Changes are confined to the two Dockerfiles
and their documentation/evidence; Go/native sources, runtime versions, source/base
pins, all nine CUDA architectures and Compose are unchanged.

- Both images share version/platform-specific BuildKit Go module and compiler
  caches. Module verification still runs before compilation.
- Builder-only `ccache=4.9.1-1` caches FFmpeg C/C++ and native C/C++/CUDA objects
  in separate cache mounts, using compiler-content checks without relaxed cache
  correctness settings. Verified source downloads are separate from compilation,
  so changing job counts does not repeat downloads.
- Default `BUILD_JOBS` is automatic: CPU availability capped by estimated memory
  headroom, including a cgroup v2 memory limit when present. Reserve 2 GiB; budget
  512 MiB per FFmpeg job and 2 GiB per native job, with a minimum of one. On this
  host this selected 16 FFmpeg jobs and four native jobs, replacing two each.
  A positive integer `--build-arg BUILD_JOBS=4` overrides automatic selection;
  both actual builder stages reject zero before compilation.
- Runtime `COPY --link` layers stay reusable across earlier layer changes; OCI
  revision/version labels are applied last, preventing metadata-only changes
  from rerunning runtime package installation and artifact assembly.

The cache/layer mechanisms follow [Docker's cache guidance](https://docs.docker.com/build/cache/optimize/)
and [COPY --link reference](https://docs.docker.com/reference/dockerfile/#copy---link).
Compiler caching follows the [ccache 4.9.1 manual](https://ccache.dev/manual/4.9.1.html).
Cache mounts belong to the selected builder; a new/ephemeral builder starts with
empty mounts, and layer-cache export alone does not transfer their contents.
Hosted CI cache wiring remains C02. Cached measurements do not replace clean
release qualification in an isolated builder with empty compiler caches.

### Paired measurements

Same Linux amd64 host, 24 available CPUs, Docker Engine 29.9.2, Buildx 0.38.1 and
BuildKit 0.34.0; no GPU injection. Separate snapshots preserve the exact baseline
and optimized definitions. Images are built sequentially with `docker buildx
build --load`, including local export. No global cache was pruned. Each variant
starts with its ordinary layer cache populated. Go changes append a distinct
comment to the relevant command's `main.go` in the private snapshot only; the
application worktree is untouched. Metadata changes alter only `APP_VERSION`.
The table uses medians for three samples, except one metadata sample per image.

| Image | Scenario | Before, seconds | After, seconds | Samples |
| --- | --- | ---: | ---: | ---: |
| API | Unchanged inputs | 0.466 | 0.264 | 3 |
| Backend | Unchanged inputs | 0.265 | 0.365 | 3 |
| API | Changed Go input | 10.946 | 2.822 | 3 |
| Backend | Changed Go input | 21.933 | 3.672 | 3 |
| API | Version label only | 8.487 | 0.766 | 1 |
| Backend | Version label only | 24.744 | 1.618 | 1 |

The backend's unchanged case increased by 0.100 seconds; no speedup is claimed
for that case. Both unchanged builds remain below half a second. Changed-Go
builds are 3.88x/5.97x faster; metadata-only builds are 11.08x/15.29x faster on
this host. Subsecond differences and single metadata samples have limited
precision; these results are not universal performance guarantees.

Initial optimized builds populated new compiler caches: API 61.878 seconds,
backend 1565.812 seconds. They are excluded from the paired warm table. Historical
same-source compiler-step references were FFmpeg 203.1 seconds and native 3054.0
seconds at two jobs; new steps took 40.4 seconds at 16 jobs and 1537.4 seconds at
four jobs. Those older runs were not an isolated paired cold-build experiment;
network/resource conditions differed, so these are contextual observations.
No complete cold build in two fresh builders was compared.

Actual forced recompilation verifies compiler-cache reuse separately from layer
hits: FFmpeg reused 1558/1726 cacheable calls; native reused all 169/169 and
finished in 5.580 seconds (5.3-second build step), retaining four jobs. Initial
native counters were zero hits/169 misses; after the forced run cumulative
counters were 169 hits/169 misses. Percentages above use counter differences,
not the misleading cumulative 50% figure.

### Optimization artifacts and acceptance

These image identities precede the CPU-quota correction below and are historical.

| Artifact | Local identity |
| --- | --- |
| `whisper-api:local` | `sha256:e366d21ad71a41fad7a024b54711109b8dd0e500610468c43cb1d56f9273515b` |
| `whisper-backend:local` | `sha256:57b2e0924b823260cfb2ccf8aa6e27e0e3374a14a0e2c0363b3dca8533260f7f` |
| Build input revision | `sha256:d3f8a6a22d07cf318dfc2d616d3c7fedf92ef6b1e0307405404440464071ed82` |

Both OCI revision labels match the 45-file build input inventory; only the two
Dockerfiles differ from the preceding inventory. Final default-version builds
from the actual worktree passed. Both runtime images contain neither `ccache`
nor the compiler/module cache directories. Hadolint, architecture, contracts,
agent checks and canonical Compose configuration passed. The complete
`P05_IMAGE_TESTS=1 go test -race ./... -count=1` run passed with Go 1.27.3 and the
configured Python environment; integration checks took 333.158 seconds. Actual
binary/module/source/license/library/CUDA-architecture and layer inspections,
Compose/private-network, startup/shutdown, read-only/non-root execution and
cache-retention checks passed. Real CUDA inference and unchanged chart execution
were not repeated. Final source recheck confirmed all 45 inputs unchanged;
specification freshness returned Approved/reviewed `pass`, and the exact P05
agreement returned `ready` without issues. No P05 work remains. C02 is still
incomplete and needs a separate invocation; no review, commit, push, publication
or deployment was performed.

Private evidence is `.local/build-optimization/`: exact input snapshots,
`before-status.json`, `after-status.json`, per-sample logs, `comparison.json`,
compiler-cache probe logs/results, `invalid-jobs.json`, `artifacts.json`, image
inspections, `source-tree.json`, `source-recheck.json`,
`runtime-cache-isolation.json`, `final-race.log`, `final-freshness.json` and check logs.
Temporary benchmark orchestration stays ignored under `.local/`; no new build
script or service dependency was introduced into the repository.

## CPU-quota correction — 2026-10-23

The automatic job selector previously capped `nproc` by memory only; `nproc`
could exceed the builder's cgroup CPU quota. Both Dockerfiles now also read
cgroup v2 `cpu.max` and cap jobs at the quota divided by its period, rounded
up. The result remains bounded by CPU affinity and the existing memory budget.
An unlimited quota (`max`) adds no cap, and explicit positive `BUILD_JOBS`
continues to override automatic selection. Cache identities, dependency pins,
runtime settings, native source and CUDA architecture coverage are unchanged.

`tests/integration/build_parallelism_test.go` executes the actual selection
prefix from each Dockerfile in small real BuildKit builds with resource limits
and networking disabled, without compiling or downloading application sources.
Before the fix, the one-CPU regression failed with 11 API jobs and two backend
jobs. Afterward, all ten cases passed, covering five conditions per Dockerfile:

| Builder condition | Observed jobs in each Dockerfile |
| --- | ---: |
| One CPU quota | 1 |
| 1.5 CPU quota | 2 |
| Single-CPU affinity with unlimited quota | 1 |
| 256 MiB memory limit | 1 |
| Explicit `BUILD_JOBS=3` despite tighter resource limits | 3 |

Both Linux amd64 images rebuilt without GPU injection using retained compiler
caches: API 16.060 seconds, backend 5.680 seconds. These are correction rebuild
times, not a new cold-build or paired optimization benchmark.

| Artifact | Current local identity |
| --- | --- |
| `whisper-api:local` | `sha256:037e72282b5d6cf05db4c7581efd9f343adc0e9326c749cdc78f901ba70a5b5b` |
| `whisper-backend:local` | `sha256:ada72f8b7c291a48148e995f756efc92a0d1cc8cbcb7f009b1926af53eab8510` |
| Build input revision | `sha256:fdd671143ed6d92ad35dd83d17989fb408bc27c6069940b3228b337a41b0fd05` |

Both image revision labels match the 45-file build input inventory; only the two
Dockerfiles changed from the optimization inventory. Final source rechecks found
all 45 inputs and the separate regression-test hash unchanged. Hadolint, gofmt,
`go vet ./...`, architecture/contracts/agent checks and canonical Compose
configuration passed. The full `P05_IMAGE_TESTS=1 go test -race ./... -count=1`
run passed with Go 1.27.3 and the configured Python environment; its integration
package took 333.113 seconds. This includes actual image contents, isolation,
Compose networking, startup/shutdown and cache-retention checks. Real GPU
inference and chart execution were not repeated for this Dockerfile correction.
Final specification freshness returned Approved/reviewed `pass`; the exact P05
agreement returned `ready` without issues. P05 is complete; C02 remains incomplete
and requires a separate invocation.

Private evidence is `.local/p05/cpu-quota-fix/`: original Dockerfile snapshots,
`red.log`, `green.log`, `builds.json`, build logs, image inspections,
`artifacts.json`, `source-tree.json`, `source-recheck.json`, `final-race.log`,
`final-status.json`, `final-freshness.json` and the other check logs. No commit,
push, publication or deployment was performed.
