# Build, security and release automation

C02 has passed local checks and remains incomplete pending actual trusted GitHub
CI on the current committed inputs. See the
[current handoff](specs/gpu-whisper-server/tasks/handoff.md). Local fixtures and
cached dirty-checkout builds do not qualify a release or GPU acceptance.

## Contributor CI

`.github/workflows/ci.yml` runs every PR/main push without path filters, secrets,
write tokens or GPU runners. It includes `source`, `chart`, `build-api`,
`build-backend`, image runtime/license checks and the aggregate `Security Check`.
The aggregate rejects failed, cancelled, skipped and missing dependencies.

Source checks reuse real module/format/vet/race/import/contracts/agent tests.
Chart checks reuse P06's Helm 3/4 and Kubernetes API-schema matrix. Runtime checks
reuse P05's actual image/metadata/license/Compose harness. Separate cold builds
use exclusive GPU-free BuildKit builders without imported layer or compiler
caches and retain actual SBOM/provenance in the selected OCI indexes. Normal
Dockerfile caches remain available to ordinary developer builds. Untrusted PR
cache is never a trusted release input.

`tools/versions.yaml` pins executable/module checksums, action revisions,
BuildKit, its SBOM generator and the disposable registry. `tools/install_ci.py`
installs only into its explicit destination. The uv 0.13.5 project uses pinned
CPython 3.14.9, including secure archive extraction; Go/scanner execution uses
Go 1.27.3. Tools are developer/CI tooling,
not service dependencies. No global agent settings are changed.

Native CI operations use reviewed, SHA-pinned publisher Actions:

Both workflows load shared configuration through
[ci-config](../.github/actions/ci-config/action.yml) immediately after checkout.
Python comes from `.python-version`; the native setup-uv Action reads the exact
uv requirement from `pyproject.toml` using `version-file`. Buildx, BuildKit,
SBOM-generator and Trivy pins come from `tools/versions.yaml`. No workflow copies
these version numbers. The shared Action also owns CPU-based worker budgets,
dependency-cache paths/namespaces, artifact retention/compression and download
timeouts. Workflow/job/step environment overrides of shared settings are rejected
by the policy checks. Runner labels, repeated timeouts and literal action SHAs
use native scalar YAML anchors within each workflow; action revisions remain
immutable and checked against the reviewed inventory.

| Operation | Action |
| --- | --- |
| Buildx lifecycle and binary cache | `docker/setup-buildx-action` |
| Matching OCI labels and manifest/index annotations | `docker/metadata-action` |
| Parallel image matrix, OCI export, attestations and layer caches | `docker/build-push-action` |
| Scoped registry authentication and automatic logout | `docker/login-action` |
| Pinned Trivy installation and binary cache | `aquasecurity/setup-trivy` |
| Current-source secret scan, JSON output in private runner storage | `aquasecurity/trivy-action` |

Python retains project policy: validate exported OCI bytes against the Action's
digest, sanitize secret reports, check coverage/freshness/exceptions, select
immutable release inputs and recover partial publication. It does not orchestrate
the image build in CI. The standalone build CLI remains available locally.
Go tests, Ruff, mypy and Helm's two-version matrix use their native commands;
adding an Action around each command would not add concurrency or caching.

Image metadata fields, local inspection and immutable build-version semantics
are documented in [Container build parameters](container-builds.md#registry-metadata).
The protected publisher preserves the accepted labels and annotations by copying
the OCI index unchanged; registry tags and repository overview settings remain
separate from embedded build metadata.

## CI performance and cache policy

Both image roles share one matrix (`max-parallel: 2`, `fail-fast: false`), keeping
the qualification names `build-api` and `build-backend`. Image/runtime tests and
security scans run independently after their inputs arrive. The final
`Security Check` always checks source, chart, the complete build matrix, runtime
tests and scans; failed/skipped/cancelled dependencies cannot become success.
Only superseded PR runs are cancelled. Protected publication stays serialized
without cancellation.

| Parameter | Value and purpose |
| --- | --- |
| `GOMAXPROCS`, `GOFLAGS=-p=N` | `N=nproc`; Go runtime and package build concurrency follow available runner CPUs. |
| `go test -parallel N -count=1` | Allow concurrent tests where explicitly supported; execute tests every run. |
| `go test -vet=off` | Avoid duplicate vet work after the separately required `go vet ./...` check. |
| `CI_TOOL_JOBS=N`, installer `--jobs` | Concurrent checksum-verified tool downloads/installation, capped by the number of tools. |
| `CI_BUILD_PARALLELISM=N` | Pass available CPU count to setup-buildx's `buildkitd-config-inline`; local builds also accept `--parallelism`. |
| Native `BUILD_JOBS` | Keep existing automatic CPU/quota/RAM sizing; do not override it with an unsafe CPU-only count. |
| PR layer cache | Local OCI cache, `mode=max,compression=zstd,compression-level=3`; rotate to avoid accumulating old blobs. |
| Image artifact compression | Keep `compression-level: 0` for already compressed OCI archives. |
| `SEGMENT_DOWNLOAD_TIMEOUT_MINS` | Five-minute cache segment timeout; required build/scan checks still run. |

Go modules/build outputs use the pinned setup-go cache with `go.sum` as input.
Python uses setup-uv's dependency cache, keyed by `uv.lock`, `pyproject.toml` and
`.python-version`; publisher jobs disable this cache. Python jobs synchronize with
`uv sync --locked --no-build` before executing commands with `uv run --no-sync`.
The lock verifies dependency identities, and module verification still runs.
See the pinned [Go](https://raw.githubusercontent.com/actions/setup-go/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/README.md#caching-dependency-files-and-build-outputs)
and [uv cache guidance](https://docs.astral.sh/uv/guides/integration/github/#caching).

Tool caches contain downloaded archives/schemas, keyed by OS/architecture/job
and the tool manifests. Every restored input is verified against SHA256 before
extraction/use. Helm 3/4 install directly to separate names so parallel extraction
cannot overwrite a shared `helm` executable. Trivy persists only `db/`, with
daily keys and normal upstream refresh enabled. Vulnerability scans independently
require schema 2, content updated within 24 hours and an unexpired `NextUpdate`,
even if Trivy exits zero. This also rejects an expired DB downloaded in the last
hour; see the pinned [Trivy update logic](https://raw.githubusercontent.com/aquasecurity/trivy/v0.75.0/pkg/db/db.go).
Reports, secret matches, scanner
result caches and credentials are never exported as dependency caches.

The Trivy installer Action separately caches its pinned executable in CI.
Every restored executable is checked against its reviewed binary SHA256 before
execution. Protected publisher jobs disable that binary cache. The source scan
Action disables its own report/database cache and writes raw JSON only under a
private runner directory; the following policy step emits counts, and an
unconditional cleanup removes raw data. Aggregate secret/image scans retain the
private-output wrapper because their report redaction and input bindings are
project requirements.

Layer caches are PR-only, scoped by role, tool/build/module inputs and source
revision. Main builds use exclusive builders, `no-cache: true` and no retained
builder state, importing/exporting no layer or compiler cache. The local build CLI rejects cache arguments together with
`--cold`. This preserves trusted release qualification. External layer caches do
not persist `RUN --mount=type=cache` compiler/module contents; unchanged build
layers can still be reused. See [Docker local caches](https://docs.docker.com/build/cache/backends/local/),
[BuildKit parallelism](https://docs.docker.com/build/buildkit/configure/#max-parallelism)
and the pinned [cache action/scoping rules](https://github.com/actions/cache/tree/cdf6c1fa76f9f475f3d7449005a359c84ca0f306).

Local validation passed 100 tooling tests and actual BuildKit cache export/import
with a cached COPY layer and preserved OCI SBOM/provenance. Installing five
pinned tool/schema sets took 6.383s with network downloads and 0.515s from the
verified cache on the same machine. These are local installer measurements;
GitHub cache transfer, full CUDA compilation and end-to-end CI latency were not
benchmarked. Current remote qualification remains incomplete.

## Helm chart metadata

`charts/whisper-server/Chart.yaml` is the chart metadata source for Helm 3 and 4.
It supplies the chart name/description, separate chart/application versions,
keywords, project home/source links and the maintainer's public profile.
Annotations add the chart's `Apache-2.0` SPDX license, documentation link and
Artifact Hub category/documentation/source/support links. The chart license
describes project-owned chart files; the separate API/backend images retain
their own complete redistributed-license expressions.

Helm preserves these fields in the packaged chart, `helm show chart`, the
repository `index.yaml` and the OCI configuration object. Native `helm push`
generates OCI title, description, chart version, project/source URLs, authors
and publication time, and forwards custom chart annotations to the manifest.
License and documentation are therefore readable from the registry manifest
without downloading images or installing the chart. Custom metadata stays
under `annotations`; unknown top-level Chart.yaml fields are not used.

The publisher uses the reviewed inventory's actual chart/application versions.
Native Helm derives version/date annotations for the selected package rather
than maintaining a second copy in Chart.yaml. Artifact Hub annotations describe
the package; repository registration and publication are separate operations.

```sh
helm show chart charts/whisper-server
helm package charts/whisper-server --destination .local/chart-metadata
helm show chart .local/chart-metadata/whisper-server-0.0.0-dev.tgz

# Set CHART_REF and CHART_VERSION from a published, reviewed release inventory.
helm show chart "$CHART_REF" --version "$CHART_VERSION"
```

References: [Helm chart metadata](https://helm.sh/docs/topics/charts/#the-chartyaml-file),
[Helm OCI registries](https://helm.sh/docs/topics/registries/), and
[Artifact Hub annotations](https://artifacthub.io/docs/topics/annotations/helm/).

## Local commands

See [container build parameters](container-builds.md) for pinned Docker ARG defaults,
resource budgets and safe version overrides.

Install the [pinned uv project](../CONTRIBUTING.md#python-tooling) with
`uv python install` and `uv sync --locked` from the repository root.
Put the selected tool directory and Go 1.27.3 on this shell's PATH. Example paths
below are ignored; never stage raw reports or credentials.

```bash
rtk proxy uv run --locked python tools/install_ci.py --destination .local/ci/bin --tools actionlint buildx govulncheck gosec hadolint trivy helm3 helm4 kubernetes-schemas
rtk proxy uv run --locked python tools/release/artifacts.py build --role api --cold --source-sha <source-sha> --output .local/ci/artifacts/api
rtk proxy uv run --locked python tools/release/artifacts.py build --role backend --cold --source-sha <source-sha> --output .local/ci/artifacts/backend
rtk proxy uv run --locked python tools/release/artifacts.py chart --helm .local/ci/bin/helm3 --source-sha <source-sha> --output .local/ci/artifacts/chart
rtk proxy uv run --locked python tools/release/artifacts.py unpack --artifacts .local/ci/artifacts
rtk proxy uv run --locked python tools/security/run.py --artifacts .local/ci/artifacts --bin .local/ci/bin --output .local/ci/security.json
rtk proxy uv run --locked python tools/security/check.py --stage complete --inventory .local/ci/security.json
```

Existing P05/P06 packet commands remain authoritative for runtime/chart tests.
Local builds with uncommitted changes do not prove that the labeled Git revision
contains those bytes. `artifacts.py` verifies blob sizes/hashes, Linux amd64,
source labels and actual SPDX/provenance predicates. Unnamed OCI exports can
have empty in-toto subjects: their checked attestation-manifest reference binds
them to the exact runtime descriptor. Transferred chart archives, extracted
bytes, values and rendered manifests are also verified. Chart archive SHA256 is
distinct from chart OCI manifest digest.

## Security evidence

`tools/security/run.py` captures raw output in private temporary files. Public
inventories retain counts, validated advisory identifiers, package/remediation
versions and input/exception identities. They omit secret matches, snippets and
raw diagnostics. Current-source secret scans use the native Action and the same
private-state exclusions and safe report evaluator as the local CLI. Actual unsafe Go/Docker/Kubernetes and fixed Go advisory fixtures test
scanner denial; disposable registry/index fixtures test publisher behavior.

The aggregate denies reachable govulncheck findings even with JSON exit zero,
unresolved gosec/Hadolint findings, all secrets and HIGH/CRITICAL Trivy findings,
including unfixed entries. Lower severities remain visible. Executable and
retrieved database/rule metadata bind report identity; refresh time is separate
from advisory database content-modification time. No scanner failure, missing
matrix entry, stale evidence or unsupported target becomes a clean report.

`tools/security/exceptions.json` contains exact rule/file false-positive or
non-applicability decisions with evidence, owner and expiry. Seven entries cover
eight gosec findings. Two exact Echo Proxy advisory exceptions cover both
binaries. The linked-symbol guard checks both entrypoints and a positive Proxy
fixture; Echo remains at the contracted v5.4.0. No blanket exclusion was added.
Expired/broadened entries fail even a clean scan. Inline `#nosec`/Hadolint ignores
require the exact ledger. There is no risk-waiver or ignore-unfixed mode.

Custom native components require advisory reconciliation. Optional
`--component-advisories <file>` supplies a schema-version-1 review bound to
`build_input_identity`, with `components` for api/ffmpeg, backend/whisper.cpp and
backend/cuda. Each needs `owner`, `expires`, `rationale`, `sources` (HTTPS `url`
and content `sha256`) and `conclusion`: no_applicable_blocking_advisories,
unresolved or blocking. Sources are retrieved again and must retain their
reviewed hashes. This review never suppresses image findings. Missing, changed,
expired or unresolved evidence blocks the aggregate. The current review is
`tools/security/component-advisories.json`. Applicable HIGH/CRITICAL/UNKNOWN
findings cannot be declared clean. Two applicable FFmpeg MEDIUM findings and
one whisper.cpp MEDIUM finding remain in report counts. Actual image tests verify
excluded H264/DVD subtitle components and Nsight tooling are absent.

## Controlled publication and prerequisites

`.github/workflows/release.yml` is manual and main-only. Registry writes use the
protected `release` environment; Pages writes use `release-pages`. A serialized
concurrency group never cancels an active publisher. `verify_ci.py` validates the
actual trusted GitHub run/event/source/jobs and downloaded security inventory
before registry credentials are used.

Docker login Actions run after validation, use only task-local Docker configuration
and log out in their post steps. Helm OCI authentication retains its native CLI;
credential directories are also removed by the existing unconditional cleanup.

Operator setup, outside local packet execution:

- Protected release environments/reviewers; GHCR job-scoped package-write token.
- Scoped protected secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`, names only.
- Existing `gh-pages` branch and public Pages site, retaining previous archives.
- Reviewed non-secret `docs/releases/<version>.json` and its exact successful main
  CI run ID. No accepted real release inventory is supplied yet.

Release inventory is reviewed in a separate metadata commit. Its
`release_source_sha` names an already completed successful trusted main CI run.
Only that selected inventory file may differ in the approval checkout. Image
provenance retains `candidate_source_sha`. Runtime/chart changes require a new
source CI run.

Stable selection uses the accepted candidate's exact images with the current
stable chart. Five actual Helm renders verify metadata-only promotion. Stable
rescans the selected original images against the current chart, without rebuilding
or relabeling binaries. Current CI images may differ from the accepted selection.

`tools/release/check.py --prepare --stage candidate|stable --inventory <file>`
checks prerequisites without claiming pending channels succeeded. Without
`--prepare`, all six channels need successful read-back. Stable also requires
both actual GPU reports, final docs, accepted image identities and rendering
runtime equivalence.

`publish.py` copies selected OCI indexes unchanged across GHCR/Docker Hub,
rejects immutable-version collisions and verifies anonymous manifest reads. It
publishes the same chart archive to OCI and Pages, merges the old index and
preserves per-channel state atomically. A partial failure remains incomplete;
retry verifies existing bytes before writes. Task-local credentials are removed.

Pages uses explicit artifact upload/deployment and anonymous read-back before
alias finalization. Stable aliases (`major`, `major.minor`, `latest`) advance
only after all channels and qualification gates pass. A separate `aliases-prepare`
phase saves previous identities before any alias write. Its immutable
`stable-alias-snapshot` artifact survives runner loss and is restored by ID when
retrying the same run. The conservative rollback set covers writes lost before
final progress could be uploaded. Snapshot preparation/upload failure prevents
writes; mutable progress artifacts can be replaced, the original snapshot cannot.
See GitHub's [artifact API](https://docs.github.com/en/rest/actions/artifacts#list-workflow-run-artifacts)
and the pinned [upload](https://raw.githubusercontent.com/actions/upload-artifact/cf430e030ddbb5b0abf93d22962f4752f3646cd9/README.md)
and [download](https://raw.githubusercontent.com/actions/download-artifact/9000827ccba6bdab643e8b6fd33ac0654aef8333/README.md) actions.

Existing aliases are restored and verified after failure. Previously absent
aliases remain explicit partial state: manifest deletion could remove immutable
tags sharing the same manifest. Such failures cannot qualify stable completion.
Before starting a different workflow run, reconcile prior partial state; a fresh
run cannot infer another run's original aliases.

Disposable local acceptance covers registry copy, collisions, retry, archive/index
preservation, accepted-image selection, actual Helm equivalence and interrupted
alias publication. Actual protected remote execution and channel compatibility
remain unqualified. Do not present local checks as completed remote publication. R01/R02 own real publication/anonymous pulls;
P07/P08 own hardware acceptance. CI never deploys Home Lab.
