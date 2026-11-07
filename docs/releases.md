# Build, security and release automation

Release qualification requires successful trusted GitHub CI on the selected
committed inputs. See the
[current handoff](specs/gpu-whisper-server/tasks/handoff.md). Local fixtures and
cached dirty-checkout builds do not qualify a release or GPU acceptance.

## Contributor CI

`.github/workflows/ci.yml` runs only on pull requests, without path filters,
secrets, write tokens or GPU runners. Pushing or merging to `main` does not start
another qualification CI run, and there is no manual CI trigger. The separate
`Docker layer cache` workflow draft is intended to refresh shared image layers on
relevant `main` pushes or manual dispatch on `main`; it does not qualify or publish
a release. PR CI includes `source`, `chart`, `build-api`,
`build-backend`, image runtime/license checks and the aggregate `Security Check`.
The aggregate rejects failed, cancelled, skipped and missing dependencies.

Release publication starts on a published GitHub Release or manual dispatch with
one existing `version_tag`. Source qualification follows the merged PR and its
latest CI run, which must have succeeded. The release workflow builds its own cold RC images;
PR image caches and artifacts are not release inputs.

Source checks reuse module verification, formatting, vet, race and behavior tests.
Chart checks use native Helm 3/4 linting, rendering and packaging, with tests for
configuration propagation, pairing, storage, isolation and resource ownership.
The custom Python Kubernetes-schema walker is retired. Runtime checks
reuse P05's actual image/metadata/license/Compose harness. Separate cold builds
use exclusive GPU-free BuildKit builders without imported layer or compiler
caches for release candidates and retain actual SBOM/provenance in the selected OCI indexes. Normal
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
timeouts. Runner labels, repeated timeouts and literal action SHAs use native
scalar YAML anchors within each workflow; action revisions remain immutable.

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
| Image layer cache (approved target; migration pending) | Native GHCR registry backend (`type=registry`, export `mode=max`); separate per-role packages, tag `main-linux-amd64-v1`, trusted `main` writers only. |
| Image artifact compression | Keep `compression-level: 0` for already compressed OCI archives. |
| `SEGMENT_DOWNLOAD_TIMEOUT_MINS` | Five-minute cache segment timeout; required build/scan checks still run. |

Go modules/build outputs use the pinned setup-go cache with `go.sum` as input.
Python uses setup-uv's dependency cache, keyed by `uv.lock`, `pyproject.toml` and
`.python-version`; publisher jobs disable this cache. Python jobs synchronize with
`uv sync --locked --no-build` before executing commands with `uv run --no-sync`.
The lock verifies dependency identities, and module verification still runs.
See the pinned [Go](https://raw.githubusercontent.com/actions/setup-go/b7ad1dad31e06c5925ef5d2fc7ad053ef454303e/README.md#caching-dependency-files-and-build-outputs)
and [uv cache guidance](https://docs.astral.sh/uv/guides/integration/github/#caching).

Tool caches contain downloaded tool archives, keyed by OS/architecture/job
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

### Shared GHCR build cache

The local implementation replaces the earlier shared GitHub Actions (`type=gha`)
cache draft in place, rather than adding a second provider. It uses BuildKit's
native registry cache (`type=registry`, export `mode=max`) in separate
`ghcr.io/<owner>/<cache-prefix>-api` and `ghcr.io/<owner>/<cache-prefix>-backend`
packages, both tagged `main-linux-amd64-v1`. These are cache packages, not release
image repositories. Stable role/platform references allow reuse across PRs;
BuildKit invalidates layers from their instructions and inputs, not a source-SHA
cache namespace. `mode=max` includes intermediate FFmpeg/CUDA stages.

The trust boundary is:

- PR jobs import public cache anonymously, without login, package write permission
  or cache export. Untrusted PR output never updates shared cache references.
- Only the cache-only workflow on trusted `main` pushes or manual dispatch on
  `main` may write. Only writer jobs receive `packages: write` and authenticate
  using the existing SHA-pinned `docker/login-action` and `GITHUB_TOKEN`.
- Writers stage cache output, verify the exported OCI digest and actual
  SBOM/provenance, then promote the shared cache reference. Failed build or OCI
  verification must leave that reference unchanged. Cache refreshes stay
  serialized without cancelling an in-progress seed.
- Keep the existing pinned official Actions and toolchains; introduce no new
  JavaScript/Docker Action or cache provider. Cache refresh is not release image
  publication and replaces no source, runtime, security or release gate.

Go module downloads now occupy normal dependency layers in both Dockerfiles,
separate from application source, so those layers can be exported. Go compiler and
ccache mounts remain ephemeral builder-local state: `RUN --mount=type=cache`
contents are not exported/restored by the registry cache. A changed compilation
step may therefore compile again even after a successful layer-cache import.
See [registry cache](https://docs.docker.com/build/cache/backends/registry/),
[cache modes](https://docs.docker.com/build/cache/backends/#cache-mode) and
[cache mounts](https://docs.docker.com/build/ci/github-actions/cache/#cache-mounts).

### Local state and remote prerequisites

The local helper/workflow wiring and recording of selected immutable cache sources
are implemented. Local lint/unit checks do not establish GHCR package creation,
a successful hosted seed, anonymous access, measured CUDA-build speedup or
security/release qualification. A minimal local fixture on fresh pinned BuildKit
workers confirmed intermediate dependency-cache reuse, offline recompilation after
a source change with an empty compiler mount, actual OCI archive SPDX/SLSA, and
native cache-manifest promotion with digest/byte readback. It did not qualify the
complete uploaded registry blob/attestation graph or full API/backend images.
Local Docker 29.9.3 differed from the CI 29.9.2 pin; Buildx/BuildKit matched.

The [maintenance workflow](../.github/workflows/image-cache-maintenance.yml)
schedules a read-only dry-run weekly (Monday, 04:23 UTC). Manual `apply` defaults
false; only explicit manual apply gets package write permission. Keep the current
main digest and newest three versions per package. Only known staging-only
versions older than seven days and untagged versions older than fourteen days
are eligible, using the update timestamp conservatively, at most 100 per run.
Unknown/release tags, missing main or malformed metadata fail closed. The native
GitHub CLI reads both full lists and revalidates candidates before deletion.
The workflow shares the writer lock and never targets release packages. GitHub
requires package admin access as well as token package-write permission. REST
cannot atomically delete conditionally on tags: external writers must not overlap,
and a failed apply can leave earlier eligible versions deleted. Review a fresh
dry-run after failure rather than retrying a stale plan. No remote retention ran.

The [cold-audit workflow](../.github/workflows/image-cold-audit.yml) runs weekly
(Monday, 03:23 UTC) or manually on `main`. It uses fresh builders without cache
imports/exports, then actual runtime, chart and scanner checks. Its diagnostic
artifacts are not selected release images, and a failed audit remains failed.

Before relying on the remote cache, the operator must review intermediate-layer
contents for public disclosure, create/configure public GHCR cache packages and
repository writer access, and verify anonymous access from a fork PR. Then retain
evidence from a successful `main` seed, two independent PR cache imports and RC
checks under the active cold policy. Check logs for the selected registry cache
manifest and `CACHED` native/decoder compilation layers; workflow success alone
does not prove a hit. The first seed, eviction or changed native inputs may require
a full build. Cached PR execution still exports/verifies fresh OCI metadata and
runs all normal runtime/security gates. None of these remote checks has run for
this migration.

Release candidates currently remain exclusive and cold:
[release.yml](../.github/workflows/release.yml) passes `cold: 'true'`, and release
inventory validation requires cold image builds. There
is no retained builder state or imported/exported layer/compiler cache; the local
build CLI also rejects cache arguments with `--cold`. The user selected a future
trusted cached RC path, but activation remains pending the formal policy update
through the currently unavailable workflow MCP. This documentation does not
change that policy or enable cached RC qualification.

Earlier local-cache validation recorded 100 tooling tests and actual BuildKit
cache export/import with a cached COPY layer and preserved OCI SBOM/provenance.
Installing five pinned tool/schema sets took 6.383s with network downloads and
0.515s from verified cache on the same machine. These historical local results
are not current migration test results, GHCR evidence or full CUDA-build timings;
they must not be reattributed to this change.

Current local validation: Ruff format/lint, mypy (43 source files), lock checks,
Hadolint, actionlint discovery of all six workflows and composite ShellCheck passed.
The tooling suite ran 198 tests: 191 passed, six production publisher/Helm artifact
fixtures were not enabled, and one pre-existing Trivy 0.75.0 JSON-suppression gap
remains an expected failure. Sixteen official-SDK transport fixtures passed.
An initial fixture failed with a registry-startup connection reset before builders;
the bounded second execution passed and owned-resource cleanup was confirmed.
An initial tooling run also failed because the test command supplied a nonexistent
Helm path; installing the checksum-verified pinned Helm into task-local tools and
rerunning the complete suite resolved that environment error. These local results
are not hosted cache-hit evidence, full production image qualification or authority
to publish or enable cached RC builds.

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
rtk proxy uv run --locked python tools/install_ci.py --destination .local/ci/bin --tools actionlint buildx govulncheck gosec hadolint trivy helm3 helm4
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

## Tag-driven release workflow

The workflow follows the same trigger and metadata/build structure as the
[cloakbrowser-mcp release workflow](https://github.com/swimmwatch/cloakbrowser-mcp/blob/main/.github/workflows/release.yml).
It runs on `release: published` or manual dispatch with an existing `version_tag`.
Supported versions are `vX.Y.Z-rc.N` and `vX.Y.Z`, with positive RC numbers.
The former `inventory`, `ci-run-id` and `stage` workflow inputs are replaced by
`version_tag`; mode and source qualification are resolved automatically.

The jobs are:

1. `metadata` resolves the tag to a commit in `main`, finds the PR that merged
   that exact commit, and verifies its latest CI run and required jobs. It records
   both the tested PR head and the resulting merge commit. This relies on the
   required up-to-date PR checks on `main`; bypass merges are not qualified.
2. `build` runs the API/backend matrix in parallel for an RC. The shared
   `build-image` Action uses isolated Buildx builders, a cold build, OCI exports,
   SHA-pinned Docker Actions, and real SBOM/provenance. Stable skips rebuilding.
3. `chart` packages the version from the tag and renders that same archive.
   Both default image tags in the packaged chart use that release version;
   contributor defaults in the source chart remain unchanged.
4. `image-runtime` verifies the newly built RC images using the same reusable
   runtime/license/Compose harness as PR CI. Stable reuses the accepted images.
5. `publish` selects the exact artifacts and performs fresh security checks
   before registry authentication. It copies the accepted OCI indexes unchanged
   to GHCR and Docker Hub, publishes the chart to Helm OCI, verifies anonymous
   read-back, and saves complete or partial channel state.
6. `pages` preserves previous chart versions, publishes the same archive in the
   Helm repository, and verifies its public index and archive bytes.
7. `aliases` runs only for stable, after all channels pass. It preserves the
   existing durable snapshot/rollback behavior before advancing semantic aliases.
8. `github-release` verifies the final state and attaches the chart and public
   release inventory, with native `sha256sum` checksums. Existing assets are
   downloaded and compared before a retry;
   different bytes are never overwritten.

Manual dispatch avoids exposing a GitHub Release before the channels are ready:

```sh
gh workflow run release.yml --ref main -f version_tag=v0.1.0-rc.1
```

The tag must already exist and identify a merged PR commit in `main`. This command
uses the release tooling from the tagged source, so create the tag after this
workflow and its helpers have been merged. The command
is an example, not evidence that a tag, build or release has been created.
When using the `published` event, the GitHub Release is already public before
workflow execution. Create stable GitHub Releases only after P07/P08 acceptance;
failed qualification prevents channel writes but does not unpublish the release.
The final asset step uses `GITHUB_TOKEN`, so creating the GitHub Release there
does not recursively trigger another release workflow, according to
[GitHub's workflow trigger rules](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow).

RC source and version come from the tag. `source.py` records the actual PR CI
reference; `inventory.py` assembles identities from the built images and chart.
The chart OCI digest is recorded after verified publication, separately from the
archive SHA256. No placeholder OCI digest can qualify completed publication.

Stable requires a reviewed, non-secret `docs/releases/X.Y.Z.json` in the tagged
source. It contains the published RC inventory, `candidate_run_id`, accepted image
digests, both successful GPU reports, and final documentation approval. The
workflow downloads that RC's exact image artifacts and candidate chart, verifies
runtime and rendered equivalence, builds the stable chart, and rescans the
selected images. It retains their original RC build provenance. GPU reports must
match the selected manifests and runtime identity. The RC artifacts are retained
for `CI_PUBLICATION_RETENTION_DAYS`; complete promotion before expiry. Restoring
expired artifacts from their immutable registry identities requires a separate
implementation; never rebuild them as a substitute for accepted GPU evidence.

Publisher configuration remains operator-owned:

- Protected `release` and `release-pages` environments and required reviewers.
- Repository-linked GHCR package permissions and public package visibility.
- Scoped `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` protected secrets.
- An existing `gh-pages` branch and public Pages site preserving prior archives.
- For stable, the reviewed acceptance inventory described above.

The global concurrency group serializes shared Helm index and alias writes;
active publication is never cancelled for another version. Python/uv versions,
worker budgets, tool checksums and cache settings remain centralized in the
existing project configuration. Registry authentication uses native Actions and
task-local credential directories, with unconditional cleanup.
Immutable-version collision checks use the task-local registry login, including
before a package exists. Completion and retries still require anonymous
read-back. New GHCR packages must be made public in their package settings
before that gate can pass; private access never qualifies publication.

Retry failed jobs in the same workflow run to reuse the exact built artifacts.
A full rebuild can produce different attestations or package bytes and cannot
overwrite an existing version; allocate another RC when inputs or bytes change.
Cross-registry publication is not atomic. A failed channel remains incomplete,
and previously published immutable versions are preserved.

Stable aliases advance only after all channels and acceptance gates pass. The
immutable `stable-alias-snapshot` artifact preserves previous identities across
runner loss. A failure restores existing aliases where possible; previously absent
aliases remain explicit partial state because deleting a shared manifest could
remove immutable tags. Such a failure cannot qualify stable completion.

This workflow publishes artifacts only. It does not deploy Kubernetes or perform
GPU acceptance, and no successful build or upload substitutes for P07/P08.
