# I03 — Portable Codex project setup

Outcome: Codex discovers portable project rules and two local skills, explains Go/Docker/Helm architecture boundaries and resolves verified private Home Lab access on the operator's workstation.
Requirements: REQ-025.

## Inputs and execution boundary

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: complete under the user's 2026-10-11 Codex-only project scope. External plugin copying and second-client acceptance are cancelled, not passed. See handoff.md and docs/agent-setup.md for actual evidence. The REQ-025 amendment has fresh independent review and checkpointed Approved state. Each packet requires its own explicit invocation; no later packet, commit, publication or deployment is authorized.

All commands below run from the project root and use rtk. Named scripts/tests are **to create**, unless explicitly identified as existing. Write/run the discriminating check before the change; record relevant RED, correct implementation to GREEN, and PROVE newly written absence/wiring assertions with one reversible mutation when RED did not exercise them. A missing file/tool alone is not semantic RED. Restore all probes/mutations before final checks.

Prerequisites: I01 complete and usable Git. Read spec.md REQ-025; core/contracts.yaml delivery.agent_support; quality/scenarios.yaml Q-AGENTS. Actual Codex discovery, policy and private-access results are recorded in docs/agent-setup.md; client version alone is not acceptance.

## Scope and ownership

Allowed: root AGENTS.md, useful reviewed .codex/config.toml when needed,
.agents/skills/gpu-acceptance/**, .agents/skills/release-verification/**,
tools/check_agents.py, docs/agent-setup.md, private .local/homelab preparation
and this packet's plan/checklist/handoff updates. The explicit scope correction
also reconciles REQ-025 and its delivery.agent_support/Q-AGENTS contracts before
renewed approval. No external plugin snapshot, other-client adapter, personal/global
configuration, tracked credentials/history or automatic publication authority.

U-AGENTS owns the canonical AGENTS.md and two portable local skills. Record
actual build/test/architecture entry points, packet boundaries, workflow-MCP
ownership, synchronous request_user_input preference and RTK availability without
claiming unavailable tools exist. CodeGraph-first applies only to an existing
index; I03 does not create one. No empty/speculative client settings are required.

## Architecture policy for AGENTS.md

I03 writes concise, actionable rules for all three surfaces into the case-correct
root AGENTS.md. Link authoritative sections of [plan.md](plan.md),
[Go foundation](foundation_01_go_service.md), [packaging](05_images_standalone.md)
and [chart](06_helm_chart.md); do not duplicate mutable defaults, image identities,
configuration schemas or error-mapping tables. The following rule intents are
the acceptance checklist, not a requirement to copy their wording verbatim.
Record project choices separately from language/platform guidance. No runtime
artifact, dependency change or packet execution is part of I03.

### Go

- **G01. Package boundaries:** cmd/api and cmd/backend assemble dependencies and
  own startup/shutdown. Domain owns plain types/invariants; application owns use
  cases and consumed ports; adapters own HTTP, files, decoding and subprocesses;
  platform owns configuration/logging setup. Domain/application must not import
  concrete adapters, platform loaders or Echo. This inward dependency direction
  is the project's Clean Architecture choice; cmd/internal is Go's server-layout
  guidance. Create packages only when actual behavior needs them.
- **G02. Dependency ownership:** define narrow interfaces at the consuming
  application boundary and inject concrete implementations through constructors.
  Map validated startup configuration to typed options in command assembly. Reuse
  the agreed Echo store interface rather than inventing a parallel one. No generic
  repositories, service locators, DI/code-generation framework or interfaces
  created solely for mocking.
- **G03. Error boundary:** preserve domain Failure/FailureKind, Cause/Unwrap and
  errors.Is/As classification. HTTP status, envelope, headers and safe public
  messages belong exclusively to the HTTP adapter. Never classify by error text,
  serialize Cause or expose raw Error() output. Preserve the existing unknown
  failure fallback and response ownership; ordinary failures do not use panic.
- **G04. Context and resources:** propagate context.Context as the first argument
  of request-scoped/blocking operations; never retain pooled Echo Context in
  queued work. Every goroutine/subprocess has an owner, cancellation and a bounded
  stop/wait path. Closing a transport is not proof that GPU work stopped. Keep
  FIFO, upload/decoder limits, validated request IDs, private logging and trusted
  client-IP/rate-limit rules tied to their existing contracts.
- **G05. Configuration and packages:** use the selected standard library and
  approved Echo/Koanf/validator versions. Strictly validate every configuration
  source before startup, preserve precedence and fail on unknown/deferred fields.
  Do not replace existing packages, duplicate defaults or add hot reload/new
  storage/protocol behavior under a refactor. New dependencies or behavior changes
  require the appropriate separately authorized planning/specification work.
- **G06. Verification:** use gofmt, go vet and behavior-focused package tests;
  use race checks for relevant concurrency paths in the supported test environment.
  F01 owns go list based import-boundary enforcement; later packets own actual
  adapter/process wiring. Test observable failures and resource ownership, not
  just implementation shape; standard Go tools do not prove architectural or
  GPU correctness by themselves.

### Docker and Compose

- **D01. Image boundaries:** keep separate API and backend multi-stage builds.
  The backend supervisor owns the unchanged native child process; the API owns
  decoding. Runtime stages contain only required binaries/libraries and license
  material. Preserve compatible CUDA/FFmpeg runtime dependencies rather than
  forcing scratch/Alpine or copying a whole development tree.
- **D02. Reproducible GPU coverage:** preserve Linux amd64, pinned source/toolchain
  identities and core's CUDA architecture policy with native detection disabled.
  Build without GPU injection. Do not copy the narrower CUDA architecture list
  or newer CUDA version from an upstream example. GPU access and a positive native
  health response alone are insufficient runtime evidence; CPU fallback must not
  satisfy readiness.
- **D03. Build inputs:** verify actual base-image digests and source checksums;
  order stable module inputs before frequently changed source for build caching.
  Keep .dockerignore aligned with required source/license inputs while excluding
  personal state, secrets, audio/models and outputs. No secrets in ARG/ENV/COPY;
  use BuildKit secret mounts only if a legitimate build input requires them.
  Pins receive reviewed updates; a cached build does not replace release checks.
- **D04. Runtime permissions and storage:** use explicit non-root execution,
  read-only root filesystems and only the contract's bounded writable mounts.
  Verify UID/GID permissions rather than adding root/privileged workarounds.
  Prepared model files stay read-only, model cache persists, and disposable audio
  work stays separate. Routine cleanup must not remove operator-owned cache.
- **D05. Lifecycle:** use exec-form entry points so Go processes receive signals;
  the supervisor must stop and reap its native process group before replacement.
  Grace periods follow configured shutdown/cleanup budgets. Probes are bounded
  and distinguish liveness from readiness; no inference-based warmup/healthcheck
  and no claim that container startup establishes model readiness.
- **D06. Compose wiring:** maintain one canonical stack and non-secret example
  inputs. Publish only the API, with loopback in local recipes; keep the supervisor
  private and native HTTP on backend loopback. Allocate exactly one GPU only to
  backend, with capabilities gpu and either count or device_ids. Preserve required
  download egress. Distinguish .env interpolation from application injection;
  changed configuration recreates the pair. P05 owns config/build checks and
  P07 owns actual lifecycle/GPU evidence.

### Helm and Kubernetes

- **H01. Chart structure:** keep the minimal charts/whisper-server application
  chart, apiVersion v2, SemVer chart version and quoted appVersion. Split templates
  by resource, use two-space indentation and chart-prefixed helpers in _helpers.tpl.
  Use documented camelCase values with explicit types and recommended resource
  labels. Do not introduce a library-chart dependency just to reuse a few helpers.
- **H02. Stable pairing:** retain separate API/backend StatefulSets, matching
  ordinals, per-backend Service selection and per-pair NetworkPolicy. Selector
  labels must stay stable across chart/image version changes; never include a
  changing version/checksum in a selector. Only the public API Service/optional
  Ingress exposes client traffic. No shared backend pool or same-Pod redesign.
- **H03. GPU and rollout:** request the agreed GPU resource only in backend with
  the contract's matching request/limit values. Retain ordered, no-surge replacement
  and explicit single-GPU downtime; never permit old/new native GPU overlap or
  CPU fallback. Host drivers, GPU device plugin/operator, CNI, storage and Ingress
  controller are operator prerequisites, not resources provisioned by this chart.
- **H04. Values and parity:** values.schema.json enforces supported keys, types
  and bounds; template failures enforce cross-field constraints the schema cannot
  express. Serialize the existing canonical configuration and verify Docker/Helm
  parity instead of creating another configuration API/default table. Keep two
  image references and release-inventory identities; no invented tags/digests.
- **H05. Health and updates:** keep distinct startup/readiness/liveness probes
  and provisioning budgets. Healthy long inference must not trigger liveness
  restart. Configuration changes update both controllers' Pod-template checksums;
  supervisor protocol/fingerprint/generation checks guard mixed revisions.
  Termination grace follows configured shutdown plus cleanup, not a template's
  arbitrary default.
- **H06. Permissions, cache and evidence:** retain non-root/read-only execution,
  bounded transient storage, read-only prepared files and Retain/Retain cache
  ownership. Existing claims are never deleted by the chart. No unnecessary API
  token mount, RBAC, privileged settings or broadened network policy. Policy needs
  an enforcing CNI. P06 owns values/render/schema/package checks; actual GPU,
  networking, rollout/rollback and storage behavior require P08, not rendered YAML.

### Research references and transfer decisions

Research checked on 2026-10-11 by three requested GPT-6 Luna scouts; the planning
owner validated primary sources and compatibility with the Approved contracts.
The requested model is recorded without claiming measured savings or independently
verified inference identity. References inform rules; they are not copied code,
new dependencies or authoritative sources for this service's behavior.

| Source | Accepted use | Limit on transfer |
| --- | --- | --- |
| [Go module layout](https://go.dev/doc/modules/layout), [Code Review Comments](https://go.dev/wiki/CodeReviewComments), [error wrapping](https://go.dev/blog/go1.13-errors) | Server cmd/internal layout; consumer-owned interfaces, context/lifecycle and error-chain conventions | Clean Architecture layers and the Failure/HTTP mapping remain project decisions; keep the agreed Cause/Unwrap |
| [Prometheus contributing guide](https://github.com/prometheus/prometheus/blob/main/CONTRIBUTING.md) | A real server uses cmd and documented build/test commands | Do not copy its dependencies, package tree, tools or entrypoint size; it is a reference, not a template standard |
| [Community project-layout](https://github.com/golang-standards/project-layout) | Evaluate common directory examples against official Go guidance | Its own README disclaims official Go-standard status; do not scaffold its full directory tree |
| [Docker build guidance](https://docs.docker.com/build/building/best-practices/), [build secrets](https://docs.docker.com/build/building/secrets/), [exec entry points](https://docs.docker.com/reference/build-checks/json-args-recommended/), [Compose GPUs](https://docs.docker.com/compose/how-tos/gpu-support/) | Multi-stage inputs, reviewed pins, secret handling, signals and GPU reservations | Preserve this project's approved versions, runtime libraries, supervision and download egress |
| [whisper.cpp CUDA Dockerfile](https://github.com/ggml-org/whisper.cpp/blob/master/.devops/main-cuda.Dockerfile) | Concrete CUDA builder/runtime example | Do not copy its mutable bases, narrower architecture list, whole-tree runtime copy or shell wrapper; the pinned OpenWhispr source remains authoritative |
| [Helm templates](https://helm.sh/docs/chart_best_practices/templates/), [values](https://helm.sh/docs/chart_best_practices/values/), [labels](https://helm.sh/docs/chart_best_practices/labels/), [schema](https://helm.sh/docs/topics/charts/#schema-files) | Scoped helpers, typed values, schema checks and consistent labels | Helpers/labels are conventions; stable selectors, pairing and configuration parity follow this project |
| [kube-prometheus-stack deployment](https://github.com/prometheus-community/helm-charts/blob/main/charts/kube-prometheus-stack/templates/prometheus-operator/deployment.yaml) | Real named helpers and separated selector/resource labels | Do not copy its Deployment, operator, CRDs, dependencies, permissions or values surface |
| [Kubernetes GPUs](https://kubernetes.io/docs/tasks/manage-gpus/scheduling-gpus/), [probes](https://kubernetes.io/docs/concepts/workloads/pods/probes/), [NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/) | Confirm resource/probe semantics and CNI enforcement prerequisite | The generic API allows options beyond our contract; preserve agreed settings and P06's existing version matrix, not an assumed Home Lab version |

## Local Home Lab preparation

The user explicitly requested reusable access to the educational Home Lab test
cluster, including necessary local credentials. I03 owns initial collection and
read-only access verification on this workstation; P08 owns refresh and actual
test-release operations. This preparation does not authorize a test deployment,
cluster provisioning, RBAC changes or unrelated workload operations.

Use .local/homelab as the canonical private location:

| Local artifact | Content and responsibility |
| --- | --- |
| .local/homelab/README.md | Purpose, confirmed cluster/context identity, network/VPN/SSH prerequisites if needed, login/re-login procedure, required auth plugins/tools, exact local file references and explicit kubectl/Helm commands |
| .local/homelab/cluster.yaml | Operator-local inventory: Kubernetes distribution/server and client/Helm versions; nodes/OS/container runtime; RTX 2060, driver/VRAM and allocatable/available GPU; device plugin/runtime class; selectors/taints; CNI policy support; StorageClasses/access modes/claim ownership; existing Ingress/TLS prerequisites when applicable; confirmed test namespace/release or explicitly pending choices |
| .local/homelab/kubeconfig | Dedicated configuration for the confirmed cluster/context with usable local authentication; do not rewrite the operator's global kubeconfig/current context |
| .local/homelab/access/ | Only credentials, certificates/keys or login-provider material actually required by the selected authentication method and not already carried by kubeconfig; no speculative credential copies |

Discover facts from user-provided access and existing local configuration before
asking for missing connection details. Do not infer the test target from whichever
context happens to be current. Capture access scope, credential renewal/expiry
where applicable, observed timestamps/source evidence and freshness status;
explicitly mark unknown/unverified inventory fields rather than inventing values.
Read-only discovery must not create a GPU probe workload or install infrastructure.
Credentials necessary for login may be stored locally as requested; runbook prose
references those files instead of reproducing passwords/tokens/key contents.

Use directory permissions 0700 and credential-file permissions 0600, with
same-user agent access. Verify .gitignore and .dockerignore exclude the directory;
do not copy it to chart packages, images, shared agent config, CI artifacts or
public documentation. Agents may read the local runbook/inventory as task input;
do not print raw credential values in tool output or logs. Keep complete private
inventory and connection evidence locally. Public instructions contain only the generic path,
required field descriptions and collection/reuse procedure. Inspect configuration
without dumping raw kubeconfig/secrets; never weaken TLS verification to obtain
a successful connection.

Record kubectl commands with explicit --kubeconfig and --context, and Helm with
--kubeconfig and --kube-context; scoped operations also name the namespace/release.
Verify authentication/API connectivity, read-only inventory access and relevant
permissions through auth can-i without changing RBAC. Record a tested login flow
and safe success/failure evidence; do not call a copied kubeconfig verified access.
Missing login material keeps this local preparation incomplete; ask only for the
information the agent cannot discover. Unknown GPU/storage prerequisites remain
explicit P08 prerequisites, not an I03 permission to change the cluster.

AGENTS.md and the gpu-acceptance skill must point to .local/homelab/README.md,
describe explicit target selection/freshness checks and preserve P08's operation
scope. Do not embed actual addresses, node/context names, accounts or credentials
in tracked rules. Ordinary builds, CI and portable agent-discovery checks
do not require .local/homelab; a fresh contributor checkout can lack private access.
Separately verify on this workstation that Codex resolves the local runbook
without disclosing its private content and can perform the recorded read-only
connection check. I03 completion records both portable discovery and this user's
local preparation; credentials are not completion-report attachments.

Source semantics checked on 2026-10-11: [Kubernetes context/kubeconfig selection](https://kubernetes.io/docs/tasks/access-application-cluster/configure-access-multiple-clusters/),
[kubectl auth can-i](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_auth/kubectl_auth_can-i/)
and [Helm context/kubeconfig options](https://helm.sh/docs/helm/helm/).
Actual private inventory and read-only access evidence are now under .local/homelab;
refresh them before the separately authorized P08 runtime suite. No extra tracked script or production cluster-client
dependency is introduced; one-off collection helpers belong under .local/.

## Project skills and developer tools

Canonical gpu-acceptance and release-verification directories live in
.agents/skills. Codex must actually discover and load their metadata/sources.
P07/P08/C02 harnesses remain planned and unavailable; these skills grant no
deployment, inference or publication authority and invent no runnable commands.

The user selected project files for Codex through Request User Input on
2026-10-11. I03 does not copy engineer-agent-workflows, install another client or
require an external plugin release. Those earlier gates are cancelled by the
scope change, not made green. Explicitly invoked operator-installed workflows
still require their own matching manual/MCP; missing prerequisites block that
workflow. Optional Context7/GitHub tools and RTK availability are explicit and
are not service/build dependencies. No global controls or credentials are changed.

## Checks, lifecycle and gates

Reusable check: `rtk proxy python3 tools/check_agents.py` from the project root.
Python 3.11+ is a developer prerequisite. Validate canonical policy references,
local skill metadata/source containment, useful reviewed Codex MCP config shapes,
obvious personal paths/credential material and absence of unsupported adapters or
external snapshots. Private Home Lab files are not needed by this structural
check. Unsupported controls, malformed settings or non-portable paths fail it.

RED: a populated fixture demonstrated the old client requirement contradicted
the explicit new scope. GREEN: the current full project structural check passes.
PROVE: disposable fixtures must reject malformed metadata/config, an escaping
skill source, a personal path, an unsupported adapter and a plugin snapshot;
restore/remove each fixture. Do not break real project files or copy private data.

Fresh native Codex acceptance must identify automatically discovered AGENTS.md
and both local skills before manual lookup, load their actual sources, run the
structural checker and explain these cases with actual rules and owning packets:

| Case | Required explanation | Runtime enforcement owner |
| --- | --- | --- |
| HTTP status in a use case / application imports Echo | Consuming application ports, protocol-neutral failure and HTTP mapper boundary | F01, P02-P04 |
| Root runtime / missing CUDA architecture / native HTTP host port | Permissions, contract GPU coverage and private native transport | P05, P07 |
| Shared backend Service / changing selector / cache-deleting teardown | Ordinal pairing, stable selectors and retained cache ownership | P06, P08 |

The same fresh client resolves the private runbook and executes only its recorded
explicit-target read-only API connection command with captured/suppressed output.
No namespace/release is selected. Record actual client version, command exits,
safe source references and answers in docs/agent-setup.md/handoff.md; remove raw
private transcripts. Native session success alone is not architecture/access proof.
Keep runtime symbols and harnesses explicitly planned until implemented.

Normal client configuration/discovery stays enabled. Do not bypass permissions,
switch global settings or add a sandbox override to force acceptance. A required
failed/unavailable check leaves I03 incomplete. Native discovery, policy
comprehension and private read-only access must all pass; no next-packet tests,
runtime artifact, GPU workload or deployment is executed to finish agent setup.

## Completion

Record actual commands, results, identities and relevant RED/GREEN/PROVE evidence. Recheck pinned specification freshness. Mark only this packet's checkbox in [todo.md](todo.md) after every required check passes and update current [handoff.md](handoff.md). A failed/unavailable required check leaves it incomplete. Preserve unrelated files and stop after this packet; no automatic next packet, review, commit, publication or deployment.
