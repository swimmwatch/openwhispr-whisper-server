# Internal composite actions

Run local actions after checkout. Native actions remain pinned to reviewed commit
identities in [tools/versions.yaml](../../tools/versions.yaml). Shared runtime and
cache settings come from `ci-config`; Python and uv versions remain in the project
manifests rather than action inputs.

| Action | Responsibility | Inputs / prerequisites |
| --- | --- | --- |
| `ci-config` | Export tool pins and shared CI settings; invoke runner parallelism | Checkout with project manifests |
| `runner-parallelism` | Derive Go, installer and image-build worker budgets from `nproc` | Linux runner; no inputs |
| `setup-python` | Install pinned uv/Python and sync locked dependencies | After `ci-config`; `cache` and `dev-dependencies` are `true` or `false` strings |
| `build-image` | Build and verify one OCI export with native Docker Actions and preserved SBOM/provenance | After `ci-config` and Python setup; `role`, `source-sha`, optional `version`, `cold`, `cache-access` (default `read`) |
| `cache-tools` | Restore/save checksum-verified downloads by runner, job and manifest identity | After `ci-config`; no inputs |
| `cache-trivy-db` | Restore/save only the daily vulnerability database | After `ci-config`; no inputs |
| `setup-trivy` | Install the pinned native scanner and immediately verify its executable hash | After Python setup; required `destination`, `cache` is a `true` or `false` string |

Only the source-check job installs Python developer dependencies. Other jobs use
`dev-dependencies: 'false'`; protected publication also uses `cache: 'false'` for
Python and the scanner executable. Download archives and vulnerability databases
retain their separate existing cache policies. Restored downloads and executables
are revalidated; scanner reports and credentials are never dependency cache inputs.

## Registry image cache

The local implementation replaces the earlier GitHub Actions (`type=gha`)
shared-cache draft with BuildKit's native GHCR registry cache (`type=registry`, `mode=max`).
Use separate `<cache-prefix>-api` and `<cache-prefix>-backend` packages with the
`main-linux-amd64-v1` tag, never release image repositories. PRs import anonymously
and never export or log in. Only trusted `main` push/manual cache writers receive
`packages: write` and use the existing SHA-pinned `docker/login-action`. Writers
stage cache output and promote the shared reference only after OCI digest and
SBOM/provenance verification; a failed build/verification must not advance it.
Keep the existing pinned official Actions and toolchains; add no JavaScript/Docker
Action or cache provider.

The helper/workflow wiring and cache-source artifact recording are implemented
locally; this does not establish that GHCR packages exist or a hosted build used
them. Cold builds force cache access to `none`. The cache-only workflow publishes
no release images and does not duplicate qualification CI. Go modules now occupy
normal dependency layers in both Dockerfiles; compiler/ccache mounts remain
builder-local and are not exported by the remote cache. A local fresh-worker
fixture confirmed dependency-layer reuse with an empty compiler mount and offline
recompilation after a source change. This does not qualify full production images
or the complete uploaded registry attestation graph.

The [maintenance workflow](../workflows/image-cache-maintenance.yml) runs weekly
in read-only dry-run mode. Only an explicit manual `apply` enables package deletion:
keep the main digest and newest three versions; expire staging-only versions after
seven days and untagged versions after fourteen days, at most 100 per run.
It shares the writer lock, never targets release packages and has not run remotely.

The [cold audit](../workflows/image-cold-audit.yml) runs weekly or manually on
`main`, with fresh cold builders, actual runtime/chart checks and scanner evidence.
It neither publishes release images nor replaces the current release cold gate.

Release builds still use `cold: 'true'` and the inventory cold-build gate. The
user-selected future trusted cached RC path remains inactive pending the formal
policy update through the currently unavailable workflow MCP. Public package
visibility, intermediate-layer disclosure review, repository writer permissions,
anonymous fork pulls, a main seed and two independent PR checks plus RC checks
remain unverified. See [cache policy](../../docs/releases.md#ci-performance-and-cache-policy).

Keep callers responsible for job permissions, release environments, artifact
identities and publication order. Keep native build, authentication, upload and
deployment actions visible at their workflow ownership boundaries.

When changing an action, run the [local lint and tooling checks](../../CONTRIBUTING.md#github-actions-linting).
Use actionlint and ShellCheck for workflow/action syntax. Project tests exercise
the invoked build, artifact and release tools rather than parse workflow files.
Local checks do not execute hosted GitHub actions or qualify a release.
