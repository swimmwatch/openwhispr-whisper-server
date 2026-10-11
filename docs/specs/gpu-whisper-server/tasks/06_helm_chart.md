# P06 — Complete Helm chart with dedicated Pod pairs

Outcome: a validated chart deploying complete API/backend pairs with private boundaries and retained cache.
Requirements: REQ-001, REQ-002, REQ-003, REQ-006, REQ-013, REQ-014, REQ-016, REQ-017, REQ-019, REQ-023, REQ-027, REQ-029, REQ-030.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Require explicit invocation, usable Git worktree and passing dependencies. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Recheck guidance/.codegraph, exact packet requirements through MCP and pinned source before work/completion. Run from project root with rtk. Tests/checks below are to create; no implementation tests exist yet. Write/run discriminating RED before behavior, implement to GREEN, PROVE absence/wiring or assertions not reached by RED with a reversible mutation, then restore.

Prerequisites: P05 checks pass. Read core/contracts.yaml deployment/configuration/delivery.chart, contracts/openapi.yaml health/private-boundary references; quality/scenarios.yaml Q-CONFIG/Q-CHART/Q-PAIRING/Q-ROLLOUT/Q-CONTRACTS/Q-OBSERVABILITY/Q-RATELIMIT; core observability/rate_limit. Source sections: spec.md:192 (chart), :204 (rollout), :224 (access); no existing chart is assumed.

## Scope and architecture

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

