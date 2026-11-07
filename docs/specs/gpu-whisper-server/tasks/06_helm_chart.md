# P06 — Complete Helm chart with dedicated Pod pairs

Outcome: a validated chart deploying complete API/backend pairs with private boundaries and retained cache.
Requirements: REQ-001, REQ-002, REQ-003, REQ-006, REQ-013, REQ-014, REQ-016, REQ-017, REQ-019, REQ-023, REQ-027, REQ-029, REQ-030.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: complete (2026-10-23) for local chart rendering, API-schema validation, configuration/boundary checks and packaging. The exact agreement remains ready against the pinned Approved/reviewed specification. No cluster deployment, GPU/CNI/runtime acceptance or publication occurred; these remain in their owning packets.

Prerequisites: P05 checks pass. Read core/contracts.yaml deployment/configuration/delivery.chart, contracts/openapi.yaml health/private-boundary references; quality/scenarios.yaml Q-CONFIG/Q-CHART/Q-PAIRING/Q-ROLLOUT/Q-CONTRACTS/Q-OBSERVABILITY/Q-RATELIMIT; core observability/rate_limit. Source sections: spec.md:192 (chart), :204 (rollout), :224 (access); no existing chart is assumed.

## Scope and architecture

Maintenance update (2026-10-25): the custom Python Kubernetes-schema walker,
recursive YAML scalar assertions and schema downloads were retired at the user's
request. Native Helm linting and behavior tests remain. Schema-validation results
and commands recorded in the completion history below are historical evidence,
not current prerequisites. Use [release automation](../../../releases.md) for the
current tooling commands.

Allowed: charts/whisper-server/**, chart validation fixtures/checks in tests/integration/**, narrow canonical config serializers only when shared parity requires them. No cluster/GPU/CNI/ingress provisioning, deployment, publishing, autoscaling or global state.

Use two StatefulSets with matching replicas/ordinals and dedicated per-ordinal backend Services. API-n derives only backend-n from its own Pod name; selectors use statefulset.kubernetes.io/pod-name (avoid reliance on newer pod-index labels). Public Service balances APIs only. Native HTTP is loopback inside backend. Create per-pair NetworkPolicy allowing only API-n to backend-n's supervisor; default boundary excludes direct native/load access, non-paired Pods and other namespaces. Document CNI as required; rendered YAML does not prove enforcement.

Default one pair; backend alone gets matched positive nvidia.com/gpu request/limit default one. Expose separate image repository/tag/digest, CPU/RAM resources, selectors/affinity/tolerations/runtime class, model configuration/bounds, persistent cache/existing claim/prepared read-only files and size-limited disk-backed /tmp. Do not allocate GPU to API. Replicas require sufficient GPUs and storage access; insufficient GPU remains Pending, no CPU fallback.

Use retained backend volumeClaimTemplates by default (Retain/Retain); existing claims/operator files are never chart-deleted or changed. Public Service is ClusterIP; optional ingress disabled by default, only API exposed when enabled with host/class/annotations/TLS references. No authentication/controller/certificate provisioning.

ConfigMap content/checksum changes restart both controllers; canonical model/inference fingerprint excludes component-specific addresses. Independent rollout is guarded by P02/P04 protocol/generation validation. Ordered StatefulSet replacement avoids spare-GPU surge; downtime is explicit. Probe startup budgets include 1800 s default provisioning plus model-start allowance; liveness does not restart healthy long inference. Default termination grace 45 s validates >= shutdown + cleanup +5.

Values schema rejects unknown/deferred fields, duplicate/inconsistent IDs/sets, bad types/bounds and unmatched/zero GPU. Add semantic template failures where JSON Schema cannot express equality or derived constraints. Expose logging.level and all api.rateLimit/clientIP settings with core defaults/types; reject unsupported stores, invalid finite rates/bursts/TTL/trust combinations. For enabled Ingress, document explicit operator proxy CIDRs and header sanitation; never auto-trust all private ranges. Verify restart/checksum and Docker/Helm parity for these fields, probe limiter bypass and middleware error/header schema. One authoritative config serializer/default mapping; verify parity instead of copying unrelated defaults.

Keep chart name whisper-server, apiVersion v2, valid SemVer version and quoted appVersion. Add public home/sources/maintainer metadata and actual Apache-2.0 chart license. Use clear camelCase values, typed strings/numbers and descriptions beginning with each property's name; preserve existing canonical configuration parity. Add .helmignore excluding unrelated source/output/agent data and secrets, retaining templates/defaults/schema/license. Verify package contents and helm pull/show/template locally. Final chart README is P09; multi-channel publishing/index management is C02/R01/R02, not this packet.

## Checks before templates

Create TestChartMatrix/TestChartConfigurationParity/TestChartInvalidValues/TestChartBoundaries. Expected missing-chart RED must be followed by PROVE for structural assertions.

```bash
rtk proxy helm lint charts/whisper-server --strict
rtk proxy helm template whisper charts/whisper-server --kube-version 1.35.0
rtk proxy helm template whisper charts/whisper-server --kube-version 1.36.0
rtk proxy helm template whisper charts/whisper-server --kube-version 1.37.0
rtk proxy go test ./tests/integration -run 'TestChart' -count=1 -v
```

Harness runs exact recorded Helm 3.22/4.3 patches for generic render/API-schema validation and records official runtime version-skew constraints separately. Validate saved manifests using real Kubernetes schemas, not regex alone. Include default/overrides/two replicas/ingress on/off/existing and generated claims/prepared files, invalid GPU/unknown policies/bounds. Never imply generic rendering is live target acceptance.

PROVE by removing GPU from backend/adding it to API, routing both APIs to a common backend, widening policy, omitting retention or shortening grace; one mutation at a time must fail. Re-validate restored outputs and canonical config parity. Runtime NetworkPolicy, actual Home Lab version, one-GPU upgrade/rollback/uninstall are mandatory P08 checks and are not run here.
## Completion

Run focused checks, appropriate full Go tests/vet and final pinned specification freshness check. Record exact results and mutations; required unavailable/failed checks keep this packet incomplete. Update this packet, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md); preserve unrelated work and stop. No automatic following packet, review, commit or publication.

## Technical proposal

Implement one minimal apiVersion:v2 application chart at charts/whisper-server with SemVer version, quoted appVersion, chart-prefixed helpers, typed camelCase values and split resource templates. Bind ../core/contracts.yaml deployment, configuration, runtime, private_backend, observability/rate_limit, delivery/images/chart and ../contracts/openapi.yaml health/error boundaries. Keep two StatefulSets with matching API/backend ordinals and per-backend Services selecting statefulset.kubernetes.io/pod-name. API-n derives backend-n from its own identity; there is no shared inference pool or single-Pod redesign.

Render per-pair NetworkPolicy for only API-n -> backend-n supervisor access, with native listener loopback. Public ClusterIP and optional disabled-by-default Ingress expose API only; no RBAC, token automount, controller/cert/GPU/CNI installation. Backend alone gets positive matching nvidia.com/gpu requests/limits. Separate both image repository/tag/digest references and CPU/RAM/scheduling/runtimeClass options. Selectors exclude versions/checksums and stay stable across updates.

Use retained backend volumeClaimTemplates by default with Retain/Retain, or explicit operator claims/prepared read-only mounts. Existing claims/files are never deleted or adopted destructively. Validate replicas/storage/access modes. Each container has bounded disk-backed /tmp, non-root/read-only root, dropped capabilities and no privilege escalation or needless service-account token.

Serialize exactly the canonical config, not another defaults table. values.schema.json rejects unknown/deferred keys/types/bounds; template checks enforce remaining cross-field model/set/GPU/timeout/proxy relations. Preserve logging/rate/client-IP settings and unsupported-store rejection. Checksum both Pod templates for relevant configuration changes. Protocol/fingerprint/generation guards make mixed revisions unready. Ordered no-surge replacement and explicit one-GPU downtime remain; do not add an upgrade strategy needing a spare GPU.

Probes use independent bounded live/ready checks, startup budgets covering provisioning and native-load bounds and no inference. Derive termination grace from shutdown+cleanup+5 seconds, default 45. Include correct Helm metadata/license/ignore/package contents. No new public endpoint or chart dependencies/library chart.

P06 owns TestChartMatrix/ConfigurationParity/InvalidValues/Boundaries and saved-manifest schema checks in tests/integration. Use pinned verified Helm 3.22/4.3 patches and Kubernetes API schemas 1.35/1.36/1.37 for generic matrix; actual target/skew remains P08. Cover defaults, both image registries, two replicas, Ingress, generated/existing claims, prepared files and malformed keys/bounds. Test-first plus single mutations for GPU misassignment, shared pairing, policy widening, retention removal and short grace. Q-CONFIG/Q-CHART/Q-PAIRING/Q-ROLLOUT/Q-CONTRACTS/Q-OBSERVABILITY/Q-RATELIMIT apply. Rendering/package success is not cluster/GPU/CNI evidence; publication remains C02/R01/R02.

## Technical agreement

Selected proposal: `p06-ordinal-chart-v1`. Packet: `P06`.

- proposal_digest: `905295279131a4ada4ca21a0ee769c99d46a45c43579a2b3d76a56eec02420e1`
- agreement_ref: `{id: 885e513d-7ef8-4025-94e9-e772d3b35472, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Write chart/parity/invalid-values/boundary checks before templates and schema.
2. Implement canonical config/schema, two StatefulSets, paired Services/policies and retained storage.
3. Implement resources/security/probes/checksums/grace and optional API-only Ingress.
4. Run both Helm lines and generic Kubernetes schema/render/package matrix; apply required structural PROVE.
5. Validate restored saved manifests/parity/package contents and stop; leave live target enforcement to P08.

## Implementation evidence — 2026-10-23

Implemented `charts/whisper-server/` with application metadata, Apache-2.0 license,
Kubernetes schema attribution, typed closed values schema, split resource templates
and `.helmignore`. `tests/integration/chart_test.go` owns the acceptance harness;
`tests/integration/testdata/chart-inputs.json` pins the actual Helm distributions,
executable hashes and official Kubernetes API schema bytes. No application Go
code, module dependencies, public contracts, native source, image definitions or
frozen technical commitments changed. Unrelated work is preserved.

Two StatefulSets preserve matching API/backend ordinals. The API's exec command
derives its backend Service from the downward-provided Pod name; executable shell
argument checks cover both ordinals. Per-ordinal Services and same-namespace
NetworkPolicy allow only the paired API to the supervisor port. Selectorless
governing headless Services add no shared inference endpoints. Only the API has
a public ClusterIP Service and optional Ingress. Selectors exclude versions and
checksums. Only the backend receives matched positive NVIDIA GPU quantities.

The packaged `files/defaults.json` is a byte-checked distribution copy of
`internal/platform/config/defaults.json`, not another defaults authority.
One chart helper merges overrides and serializes canonical JSON; tests load it
through the actual Go configuration loader. Catalog/download-set replacement,
served-model selection, logging/rate/client-IP overrides, custom ports and both
Pod-template checksums are checked. Helm semantic checks cover model references,
GPU ordinals/counts, ports, proxy prefixes, rate TTL/refill, storage and timing.
Trusted-prefix fixtures are compared with the existing Go validator, including
IPv4/IPv6, normalized duplicates, mapped addresses and malformed prefixes.
Native scheduling schemas are derived from the pinned Kubernetes 1.35.0 API,
with adapted unknown-field rejection identified in the schema and chart NOTICE.

Generated cache claims use Retain/Retain; existing cache and prepared claims are
referenced without creating/adopting PVC objects. Prepared mounts are read-only.
Each Pod has separate bounded disk-backed `/tmp`, non-root UID/GID 10001,
read-only root, dropped capabilities, no privilege escalation or API token.
Startup covers provisioning + native operation + cleanup bounds; excessive
derived int32 probe counts reject. Termination grace defaults to shutdown +
cleanup + 5 seconds. API liveness uses `/health/live`; backend liveness checks
its supervisor PID independently of GPU readiness/recovery. It does not certify
GPU progress. No spare-GPU surge or zero-downtime promise was introduced.

### Verified local matrix and package

- Helm **3.22.0** (`g144ca65`) and **4.3.0** (`gbec5b06`), official Linux amd64
  archives checked against publisher checksums, then executable hashes pinned.
- Kubernetes **1.35.0 / 1.36.0 / 1.37.0** official OpenAPI schemas: 30 render/API
  cases across both Helm lines and five variants (defaults, two pairs/Docker Hub,
  shared existing cache + Ingress/TLS, prepared custom model, digest/scheduling).
- Actual Go **1.27.3**, Python **3.12.3**, PyYAML **6.0.3**, jsonschema **4.26.0**.
- Both strict linters, local package/show/template and loopback `helm pull`
  succeeded. Pulled archive bytes matched the served package. Package allowlist,
  actual license/default bytes and exclusions for fake private files passed.
- Helm caches/configuration/registry/plugin locations are isolated in test-owned
  directories; tests use no real kubeconfig or cluster. No global setup changed.

Official API validation uses saved rendered YAML and the real schema definitions.
The adapter handles Kubernetes `IntOrString` and int32/int64 bounds and rejects
unknown struct fields; an intentionally string-valued container port is detected.
The initial raw OpenAPI/string-quantity mismatch was corrected by handling the
declared union and emitting GPU quantities as strings, not by skipping validation.
Other preserved/corrected failures cover `.helmignore` as legitimate package
content, CIDR template arithmetic/Go parity and excessive startup probe counts.

The [Helm 3 support policy](https://helm.sh/docs/v3/topics/version_skew/) and
[Helm 4 support policy](https://helm.sh/docs/topics/version_skew/) place these
minor lines within the selected generic Kubernetes matrix. This is schema/render
evidence, not live API-server admission or Home Lab version qualification.

### RED / GREEN / PROVE and final checks

Missing-chart RED failed all four initial acceptance entry points. Subsequent
targeted RED caught malformed/duplicate proxy prefixes and overflowing startup
counts. GREEN passed the final chart suite. Fifteen isolated PROVE copies were
rejected at intended assertions: invalid API field type, missing backend GPU,
GPU on API, shared backend routing, widened policy, deleted retention, short
termination grace, disabled semantic validation, leaked package fixture,
RAM-backed scratch, writable prepared mount, short startup, wrong existing
claim, default-snapshot drift and ignored digest. Mutation copies were removed.

Final commands ran from the repository root with the pinned toolchain/Python:

```bash
rtk proxy env P06_CHART_TESTS=1 go test ./... -count=1
rtk proxy env P06_CHART_TESTS=1 go test -race ./... -count=1
rtk proxy go vet ./...
rtk proxy gofmt -l tests/integration/chart_test.go
rtk proxy python3 tools/check_go_architecture.py
rtk proxy python3 tools/check_contracts.py
rtk proxy python3 tools/check_agents.py
rtk proxy .local/p06/bin/helm3 lint charts/whisper-server --strict
rtk proxy .local/p06/bin/helm4 lint charts/whisper-server --strict
```

The harness defaults to `.local/p06/bin/helm3` and `.local/p06/bin/helm4`;
`P06_HELM3`, `P06_HELM4` and `PYTHON`
can select equivalent pinned inputs. Obtain the declared public files from
`chart-inputs.json` and verify their recorded identities before execution.
Ordinary Go tests skip opt-in chart execution. P05 image execution was not
repeated: every recorded production/image input hash remains unchanged.

Private evidence: `.local/p06/evidence/` contains `final-go-3.log`,
`final-race-3.log`, `final-vet-3.log`, `final-package.log`, `prove-summary.json`,
`prove-*.log`, `tools.json` and `artifacts.json`. The unpublished local package
is `whisper-server-0.0.0-dev.tgz`, SHA256
`5182bf80ca769eba4eb5afb2fd90b73565ba6685410a2879e04412b7048178f2`;
its chart/test source-tree identity is
`sha256:327886a62d1ee1c74c96f9aba8b3928ece60138e43d75c3ef09d7308d63772cb`.
These identify actual local bytes, not a Git commit or qualified release.

All required local P06 checks passed; final pinned specification/agreement
checks remain pass/ready. Cluster/GPU/CNI enforcement, actual claim permissions,
single-GPU update/rollback/uninstall and the Home Lab version remain P08.
Full CI/security/publication and final README remain C02/R01/P09/R02. No review,
commit, push, publication, deployment or following packet was launched.

## Review corrections — 2026-10-23

Fixed the two P2 findings from the separately requested P06 review. Shared labels
and selectors quote their values, preserving numeric-looking names and YAML
boolean words as strings. Cache validation compares normalized paths against
`/etc/whisper`, `/tmp` and `/prepared`, rejecting equality and ancestor/descendant
overlap while accepting distinct sibling paths such as `/etc/whisper-cache`.
No application code, public contract, dependency or frozen proposal changed.

`TestChartLabelStrings` checks exact string values throughout rendered resources
for numeric/boolean name overrides and a boolean-looking release name.
`TestChartCacheMountPaths` checks normalized conflicts and valid sibling/custom
locations. The initial focused run reproduced both defects. Two isolated chart
copies restoring the old helpers or validation subsequently failed at the
intended regression assertions; the working chart was never mutated for PROVE.

All eight chart tests passed on Helm 3.22.0 and 4.3.0, including the original
30-case official Kubernetes schema matrix and local package/show/template/pull.
The new tests cover six label cases and thirty accepted/rejected cache-path cases.
Full `P06_CHART_TESTS=1 go test -race ./... -count=1`, `go vet ./...`, gofmt and
the architecture, contract and agent checks passed with the pinned Go 1.27.3
environment. The initial PROVE attempt used an unsupported absolute harness
path; the initial architecture check lacked the pinned Go environment. Corrected
runs passed, with those earlier diagnostic logs preserved rather than counted
as acceptance. Specification/agreement freshness was rechecked as pass/ready.

Latest private evidence is under `.local/p06/review-fixes/`: `red.log`,
`green.log`, `prove-labels.log`, `prove-cache.log`, `final-race.log`,
`final-vet.log`, `check_*.log`, `final-package.log` and `artifacts.json`.
The corrected unpublished package is
`packages/whisper-server-0.0.0-dev.tgz`, SHA256
`900573f8386a05fd660008a2ae304433dfa303eb8f3b752ff7994cab8c13ee7c`.
The corrected chart/test source identity is
`sha256:39bd146c230d2403892092c7188d8acd34200a28d5adf519dafb9eb1ab4ea66e`,
computed from the compact sorted-key JSON file-hash map in `artifacts.json`.
These supersede the original local identities above for further P06 work;
neither archive was published. No cluster/GPU/CNI execution, commit, push or
following packet occurred.
