# Code review checklist

Use this for an authorized review of changed behavior. It is not a second
architecture specification, defaults table, or claim of runtime qualification.

## Scope and authority

- [ ] Read the affected contracts and callers before judging the change:
  [project rules](<../AGENTS.md>), [specification](<specs/gpu-whisper-server/spec.md>),
  [core contracts](<specs/gpu-whisper-server/core/contracts.yaml>) and
  [OpenAPI](<specs/gpu-whisper-server/contracts/openapi.yaml>). Resolve a conflict
  in its owning contract; an upstream example does not override project policy.
- [ ] Preserve Resident V1, unchanged native source, two images and separate
  ordinal-paired API/backend Pods. Do not introduce deferred modes, a shared
  backend pool, speculative layers, or new dependencies during cleanup.
- [ ] Check current pins, not an assumed latest release: [go.mod](<../go.mod>)
  owns Go/framework versions, [tool identities](<../tools/versions.yaml>) record
  distributions, and [chart inputs](<../tests/integration/testdata/chart-inputs.json>)
  pin both Helm lines. The generic Kubernetes matrix remains owned by
  [REQ-002](<specs/gpu-whisper-server/spec.md#req-002-platforms-architectures-and-required-gpu-targets>).
  A recorded pin is not a fresh security or compatibility certification; any
  version change needs its own reviewed evidence.

## Go

Apply [G01–G06](<../AGENTS.md#go-architecture>) and the
[HTTP status rule](<../AGENTS.md#http-status-constants>).

- [ ] Trace constructor wiring and imports: commands own startup/shutdown;
  domain/application stay independent of transport and configuration loaders.
  Interfaces serve actual consumers, not hypothetical mocks. Prefer a synchronous
  call when the caller can own concurrency. [Go review guidance][go-review]
- [ ] Propagate request context first through blocking calls. Call derived cancel
  functions on every terminal path; do not reset the operation budget at each
  layer or retain a pooled Echo context. Identify who stops and joins each
  goroutine. Calling `CancelFunc` signals cancellation; it does not wait for work
  to stop. [Context contract][go-context]
- [ ] Trace subprocess ownership through start failure, cancellation, pipe drain,
  kill and reap. `CommandContext` kills its command by default, not an entire
  descendant process group, and leaves `WaitDelay` unset. Preserve bounded native
  group shutdown before replacement; a closed HTTP connection is not GPU-stop
  evidence. Do not interpolate request input into a shell. [Process contract][go-exec]
- [ ] Preserve typed failure classification through wrapping with `errors.Is/As`.
  The HTTP adapter alone selects safe messages/status/headers; unknown failures
  retain the safe fallback. Check committed/disconnected responses are not written
  twice. Neither `%w` nor `%v` makes raw error text safe to expose. [Errors][go-errors]
- [ ] Exercise invalid configuration at each source, including a bad value later
  overridden by another source. Preserve precedence, collection replacement,
  unknown/duplicate/deferred-field rejection and fail-before-start behavior.
  Reuse the canonical loader and typed options, not a second default mapping.
- [ ] Check actual resource boundaries before allocation: upload framing, decoded
  samples, staging/queue capacity, response bytes and temporary files. Test cleanup
  after rejection, cancellation and shutdown without deleting valid model cache
  or prepared files. File extension and MIME type are not content validation.
- [ ] Verify correlation and privacy on success and failure: validated request ID,
  safe final status, no raw native/decoder output or user content. Admission must
  precede body work, store errors fail closed, trusted-proxy rules resist spoofing,
  and probes bypass both the limiter and inference queue.
- [ ] Cover FIFO, zero waiting capacity, queued versus active cancellation, process
  loss, mixed generations and shutdown with observable assertions. Synchronize
  tests on events rather than arbitrary sleeps; ensure assertions can detect the
  original failure, not merely a substitute implementation. [Race limits][go-race]

## Kubernetes and Helm

Apply [H01–H06](<../AGENTS.md#helm-and-kubernetes-architecture>) and relevant
[D01–D06](<../AGENTS.md#docker-and-compose-architecture>).

- [ ] Keep the minimal application chart, stable selectors, chart-prefixed helpers,
  typed values and quoted string metadata. Review both image identities and the
  packaged contents; do not import an unrelated chart scaffold. [Chart format][helm-charts]
- [ ] Validate merged values with the existing
  [values schema](<../charts/whisper-server/values.schema.json>) and template
  cross-field checks. Exercise defaults, overrides and invalid inputs under both
  pinned Helm lines without skipping schema validation. Feed serialized service
  configuration into the real Go loader to check Docker/Helm parity. Helm validates
  final `.Values`; this does not replace per-source Go validation. [Schema scope][helm-charts]
- [ ] Trace API ordinal to its dedicated backend Service and policy. Check both
  Pod-template checksums and protocol/fingerprint/generation rejection during mixed
  revisions. Two StatefulSets do not provide an atomic pair-wide rollout.
  [Identity and updates][k8s-statefulset]
- [ ] Keep matching positive GPU requests/limits on backend only; check CPU/RAM
  sizing against measured workload and scheduling constraints. Do not confuse CPU
  throttling, memory out-of-memory termination and insufficient-GPU Pending state.
  No CPU fallback or new resource defaults follow from generic examples.
  [GPU scheduling][k8s-gpu]; [resource semantics][k8s-resources]
- [ ] Derive startup and termination budgets from the contract and configured
  provisioning/load/shutdown/cleanup limits. Readiness failure removes serving
  eligibility; liveness failure can restart the container. Verify healthy long
  inference does not fail liveness, and probes neither load nor transcribe.
  [Probe semantics][k8s-probes]
- [ ] Preserve ordered, no-surge replacement and explicit single-GPU downtime.
  Review recovery from a replacement that never becomes Ready: reverting a
  StatefulSet template can leave the bad Pod blocking rollback. Require a bounded,
  authorized recovery procedure and runtime evidence, not a promise that rollback
  alone repairs it. Do not force-delete a live GPU owner. [Rollback caveat][k8s-statefulset]
- [ ] Inspect all applicable NetworkPolicies, not just the new one: allowed traffic
  is additive. Check same-namespace paired access, unintended peers/ports, and any
  egress restrictions' effect on DNS and approved downloads. An enforcing network
  plugin and positive/negative connection tests are required; a rendered policy
  proves neither isolation nor authentication. [NetworkPolicy semantics][k8s-network]
- [ ] Verify non-root execution, read-only root, dropped capabilities, no privilege
  escalation or needless API token/RBAC, and real mounted-volume permissions.
  Do not solve permission failures with root/privileged workarounds. GPU drivers,
  network, storage and ingress controllers remain operator prerequisites.
  [Application security guidance][k8s-security]
- [ ] Preserve `Retain/Retain` cache ownership and read-only prepared files. Check
  access modes against replicas and placement; upgrades/uninstall must not delete
  operator claims. Keep bounded disk-backed transient storage separate from cache;
  application admission/cleanup still enforce resource bounds. [Storage lifecycle][k8s-statefulset]

## Verification and reporting

- [ ] Use the existing [contributor checks](<../CONTRIBUTING.md>) and
  [build/security/chart checks](<releases.md#contributor-ci>): formatting, vet,
  affected behavior tests, relevant race checks, native Helm lint/render/package
  checks and affected security gates. Do not add source-string tests, custom
  repository-file checkers or retired schema walkers.
- [ ] Record exact tools, inputs and actual outcomes, including opt-in tests that
  skipped. Go race detection needs a supported environment and only observes
  executed paths; a passing run cannot prove all schedules safe. [Race detector][go-race]
- [ ] Separate rendering from API-server admission, CNI enforcement, volume
  permissions and real CUDA/lifecycle evidence. Check actual target versions
  against the [Helm 3][helm3-skew] / [Helm 4][helm4-skew] support policies; local
  `--kube-version` rendering does not qualify a cluster or either required GPU.
- [ ] Report findings by severity with location, trigger, consequence and evidence.
  State unverified behavior and failed/unavailable required checks explicitly.
  Review does not authorize deployment, publication, dependency upgrades or
  destructive recovery.

## Primary references

Checked **2026-11-07 (Europe/Moscow)** using Context7 resolve/query and official
primary pages. Go API references match the repository's Go pin. Context7 did not
expose the pinned Kubernetes/Helm versions, so their official documentation was
used for semantics, not patch-specific qualification.

- [Go Code Review Comments][go-review]: consumer-owned interfaces, contexts,
  goroutine lifetimes and synchronous APIs.
- Go 1.27.3 [context][go-context] and [os/exec][go-exec]: cancellation, process and
  wait/pipe ownership.
- [Working with Errors in Go 1.13][go-errors] (2019-10-17): wrapped classification
  and the distinction between exposing a cause and exposing its text.
- [Go Data Race Detector][go-race]: runtime coverage and environment requirements.
- Kubernetes [StatefulSets][k8s-statefulset], [probes][k8s-probes],
  [GPU scheduling][k8s-gpu] and [resource management][k8s-resources]: identity,
  retention, rollout limitations and resource/probe behavior.
- Kubernetes [NetworkPolicies][k8s-network] and [application security checklist][k8s-security]:
  additive rules, plugin prerequisites and least privilege.
- Helm [chart/schema reference][helm-charts] and [Helm 3][helm3-skew] /
  [Helm 4][helm4-skew] version support policies. The Helm 3 site marks its docs as
  no longer actively maintained. The [Helm 4 Charts page][helm4-charts] warns that
  it is not yet fully updated for Helm 4. Neither limitation changes project pins;
  use pinned-tool checks and actual target evidence before claiming compatibility.

[go-review]: https://go.dev/wiki/CodeReviewComments
[go-context]: https://pkg.go.dev/context@go1.27.3
[go-exec]: https://pkg.go.dev/os/exec@go1.27.3
[go-errors]: https://go.dev/blog/go1.13-errors
[go-race]: https://go.dev/doc/articles/race_detector
[k8s-statefulset]: https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/
[k8s-probes]: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
[k8s-gpu]: https://kubernetes.io/docs/tasks/manage-gpus/scheduling-gpus/
[k8s-resources]: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
[k8s-network]: https://kubernetes.io/docs/concepts/services-networking/network-policies/
[k8s-security]: https://kubernetes.io/docs/concepts/security/application-security-checklist/
[helm-charts]: https://helm.sh/docs/v3/topics/charts/#schema-files
[helm3-skew]: https://helm.sh/docs/v3/topics/version_skew/
[helm4-skew]: https://helm.sh/docs/topics/version_skew/
[helm4-charts]: https://helm.sh/docs/topics/charts/
