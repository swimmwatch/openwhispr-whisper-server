# P05 — Two images and private standalone Docker operation

Outcome: actual GPU-free builds containing the agreed binaries and unchanged CUDA server, with reusable Docker Compose deployment and example inputs.
Requirements: REQ-001, REQ-002, REQ-015, REQ-019, REQ-024, REQ-026.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Require explicit invocation, usable Git worktree and passing dependencies. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Recheck guidance/.codegraph, exact packet requirements through MCP and pinned source before work/completion. Run from project root with rtk. Tests/checks below are to create; no implementation tests exist yet. Write/run discriminating RED before behavior, implement to GREEN, PROVE absence/wiring or assertions not reached by RED with a reversible mutation, then restore.

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
rtk proxy docker compose -f deploy/compose.yaml config --quiet
rtk proxy go test ./tests/integration -run 'TestImageContract|TestStandaloneWiring|TestComposeConfiguration' -count=1 -v
```

Run builds in a clean builder without GPU injection; record no GPU requirement. Inspect compiled CUDA artifacts/compile flags for every declared architecture; build success on a GPU machine alone is insufficient. Use a controlled non-GPU unavailable-startup run to verify truthful readiness and safe exit; actual success CUDA transcription is P07.

PROVE by substituting an omitted architecture, root UID, leaked build marker/credential-shaped fixture or an exposed native port, then restore and show owning checks fail/pass. No real secrets in images/test fixtures. A broken dependency/image contract stays incomplete; fix implementation rather than weakening checks.
## Completion

Run focused checks, appropriate full Go tests/vet and final pinned specification freshness check. Record exact results and mutations; required unavailable/failed checks keep this packet incomplete. Update this packet, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop. No automatic following packet, review, commit or publication.

