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
| `cache-tools` | Restore/save checksum-verified downloads by runner, job and manifest identity | After `ci-config`; no inputs |
| `cache-trivy-db` | Restore/save only the daily vulnerability database | After `ci-config`; no inputs |
| `setup-trivy` | Install the pinned native scanner and immediately verify its executable hash | After Python setup; required `destination`, `cache` is a `true` or `false` string |

Only the source-check job installs Python developer dependencies. Other jobs use
`dev-dependencies: 'false'`; protected publication also uses `cache: 'false'` for
Python and the scanner executable. Download archives and vulnerability databases
retain their separate existing cache policies. Restored downloads and executables
are revalidated; scanner reports and credentials are never dependency cache inputs.

Keep callers responsible for job permissions, release environments, artifact
identities and publication order. Keep native build, authentication, upload and
deployment actions visible at their workflow ownership boundaries.

When changing an action, run the [local lint and tooling checks](../../CONTRIBUTING.md#github-actions-linting).
Use actionlint and ShellCheck for workflow/action syntax. Project tests exercise
the invoked build, artifact and release tools rather than parse workflow files.
Local checks do not execute hosted GitHub actions or qualify a release.
