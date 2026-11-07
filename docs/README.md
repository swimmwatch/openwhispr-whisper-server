# Documentation

Start with the [project overview](<../README.md>). Resident V1 implements one fixed
CUDA-resident model behind a bounded synchronous HTTP API; publication and a
successful transcript do not establish complete qualification.

## Use and operate

- [Operations](<operations.md>) — Docker Compose, both image registries, prepared
  files/offline cache, configuration, readiness, recovery and retained storage.
- [Helm chart guide](<../charts/whisper-server/README.md>) — paired StatefulSets,
  exact-version chart selection, prerequisites, values and lifecycle commands.
- [Container builds](<container-builds.md>) — pinned inputs, GPU-free builds,
  resource tuning, image metadata and provenance.
- [OpenAPI](<specs/gpu-whisper-server/contracts/openapi.yaml>) — public fields,
  response/error schemas, health and explicitly absent routes.
- [Core contracts](<specs/gpu-whisper-server/core/contracts.yaml>) — configuration,
  model/artifact boundaries, deployment and delivery requirements.

## Develop and review

- [Contributing](<../CONTRIBUTING.md>) — setup, Go/Python/CI checks, contribution
  rules and release conventions.
- [Code review checklist](<code-review.md>) — Go and Kubernetes/Helm checks grounded
  in project contracts and dated primary sources; not runtime qualification.
- [Agent setup](<agent-setup.md>) and [project instructions](<../AGENTS.md>) — local
  skills, tooling availability and authorization boundaries.
- [Build/security/release automation](<releases.md>) — CI, scanners, immutable
  artifact delivery, channel read-back and qualification gates.

### Service ownership

[API assembly](<../cmd/api/main.go>) and [backend assembly](<../cmd/backend/main.go>)
load settings, inject dependencies and own shutdown. The API owns upload validation,
FFmpeg normalization and admission/FIFO; the backend owns artifact provisioning,
one native process and exceptional stop/reload. Successful requests reuse the
resident context. See the [audio adapter](<../internal/adapter/audio/README.md>)
and [backend adapter](<../internal/adapter/backend/README.md>) for implementation
boundaries, not release acceptance claims.

Domain types and the transcription application do not depend on Echo or concrete
adapters. Ports belong to their consumers. Shared strict configuration and safe
logging are platform concerns; [configuration bindings](<../internal/platform/config/settings.go>)
and [configuration details](<../internal/platform/config/README.md>) explain their
actual representation. Module versions are owned by [go.mod](<../go.mod>), not this
navigation page.

[Failure](<../internal/domain/failure.go>) is protocol-neutral and retains its cause
through `Unwrap`. For example, wrapping a `domain.Failure` with `Kind: domain.UnknownModel`
and `Field: "model"` using `fmt.Errorf` and `%w` still allows `errors.As` to find it.
The [HTTP mapper](<../internal/adapter/httpapi/errors.go>) emits HTTP 404 with
`model_not_found` and the allowed `model` parameter, never the wrapped cause.
Unknown failures map to a fixed HTTP 500 `internal_error`. New protocols would need
their own adapter; only HTTP is implemented.

## Evidence, not promises

| Read | Interpretation |
| --- | --- |
| [Local repository audit — 2026-11-07](<verification/audit-2026-11-07.md>) | Source fixes and recorded local checks; consult its current verification status for pending results. Not release/GPU qualification or repair of published artifacts. |
| [Security review — 2026-11-07](<verification/security-audit-2026-11-07.md>) | Scoped local remediation, advisory/profile evidence and unresolved coverage/freshness gaps. Not a complete security-gate pass or a repair of rc.6 images. |
| [rc.6 publication and partial GPU runs](<verification/release-rc6.md>) | Exact artifact identities, historical channel results, failed/stale security evidence and incomplete Kubernetes qualification. |
| [v1.0.0 release-only exceptions](<verification/release-1.0.0-notes.md>) | Explicitly disclosed GPU, memory, security, documentation and unpublished-mitigation gaps; not a full acceptance pass. |
| [rc.5 Docker verification](<verification/docker-rc5-p07-server.md>) | A different candidate, closed with accepted memory/speech/performance limitations. |
| [Kubernetes development history](<verification/homelab-development.md>) | Development observations, not published-candidate qualification. |
| [Acceptance harness guide](<../tests/acceptance/README.md>) | Opt-in prerequisites and partial versus full checks; CPU helper tests are not GPU proof. |

Historical reports preserve unsuccessful runs and may refer to ignored private
receipts unavailable in a contributor checkout. Their old “next action” statements
are not new execution authority. Read the report's final disposition and exact
artifact identity rather than treating all historical entries as current state.
These documents do not report the current availability of an operator's cluster.

## Scope and community

The [specification](<specs/gpu-whisper-server/spec.md>) defines requirements;
[future work](<specs/gpu-whisper-server/spec.md#req-022-required-development-after-v1>)
retains deferred memory modes, lifecycle/model controls, access management and
metrics. The [plan](<specs/gpu-whisper-server/tasks/plan.md>),
[packet checklist](<specs/gpu-whisper-server/tasks/todo.md>) and
[handoff](<specs/gpu-whisper-server/tasks/handoff.md>) retain execution history.
Documentation improvements do not close P09 or replace its unavailable formal
workflow/freshness and runtime/reader acceptance checks.

See [support](<../SUPPORT.md>), [private security reporting](<../SECURITY.md>),
[contribution rules](<../CONTRIBUTING.md>), [code of conduct](<../CODE_OF_CONDUCT.md>),
[license](<../LICENSE>) and [third-party notices](<../THIRD_PARTY_NOTICES.md>).
