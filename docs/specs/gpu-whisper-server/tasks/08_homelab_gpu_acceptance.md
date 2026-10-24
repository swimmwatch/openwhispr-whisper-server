# P08 — RTX 2060 Helm acceptance, recovery and rollback

Outcome: the actual Home Lab target runs public CUDA transcription and survives declared lifecycle/storage transitions.
Requirements: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-007, REQ-011, REQ-012, REQ-013, REQ-014, REQ-016, REQ-017, REQ-018, REQ-019, REQ-020, REQ-027, REQ-028.

## Reviewed inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`

Status: planned; technically agreed (2026-10-22), not executed. Exact agreement below checked ready against the pinned Approved/reviewed specification. A separate incremental invocation, passing dependencies and any scoped external authority remain required. Preserve completed work; recheck source and agreement freshness before execution and completion.

Prerequisites: P06 and R01 checks passed; shared harness from P07 is available; I03's private Home Lab runbook/access is available; the same candidate's two public image digests and chart checksum/OCI identity are fixed. P07's GPU result cannot substitute for this environment. Read quality/scenarios.yaml scenarios referencing P08; core/contracts.yaml deployment/models/private_backend/delivery and contracts/openapi.yaml public operations. Evidence: spec.md:192, :204, :214, :236 and REQ-027/028.

## Target gate and scope

MANUAL GATE: user-authorized Kubernetes context, isolated namespace/release and actual image availability. Reuse authorization if already given for these exact targets. Read .local/homelab/README.md and its local inventory/kubeconfig before asking again for known access details. P08 executor owns read-only freshness checks: follow the recorded login/re-login flow, verify API authentication, auth can-i permissions and the explicit target, then refresh actual Kubernetes/Helm, OS/runtime, RTX 2060/driver/VRAM, device plugin, runtime class, CNI and storage/claim facts before mutations. Do not use the global current context or change it. The saved kubeconfig and earlier successful login do not prove credentials or infrastructure are still current. If renewal needs user interaction, report the exact established login step; unresolved external infrastructure ownership remains unassigned until established. Insufficient GPU/storage access blocks verification; do not install infrastructure or alter unrelated GPU workloads.

R01 already supplies publicly pulled candidate artifacts; do not upload a replacement from this packet. Runtime/build/rendered-behavior fixes require a new RC through R01 and renewed affected acceptance. Ingress checks use only an existing authorized controller/host/TLS reference; default remains disabled.

Allowed: tests/acceptance/**, private .local/homelab/** inventory/access refresh and target values/evidence, non-sensitive generic values/examples and sanitized docs/verification/** reports, narrow fixes within existing implementation/chart ownership. Keep the complete private inventory, topology, connection/credential material and identifying target values in .local; public reports contain test outcomes/artifact identities, not inventory/access dumps. Authorized target namespace installs/upgrades/rollback/uninstall are limited to this test release; retain generated/operator claims by the agreed policy. Do not delete cache/PVCs as automatic cleanup.

## Checks and sequence

1. Re-render/chart-validate using the actual Home Lab Kubernetes/Helm version, even if outside generic 1.35/1.36/1.37 coverage. Generic rendering is separate evidence; record official client/server skew and exact patches. Unqualified target support returns to planning, not silent exclusion.
2. Install both components with immutable image references, one backend GPU and appropriate existing/generated model storage. Observe separate Pods, pair-specific Services, actual canonical configuration/probes/mounts/UID, API without GPU, fixed resident model and public CUDA transcription.
3. Run the same public formats/client/input/FIFO/deadline/cancellation/isolation/readiness/privacy/memory/performance suite as P07 on RTX 2060. Use base and another fitting model through configuration/restart plus invalid artifact. Record separate target measurements, not copied local results.
4. Positive paired connection and negative non-paired same-namespace/other-namespace connections must demonstrate actual NetworkPolicy enforcement. Native loopback/raw /load and management routes cannot be reached from the public boundary. A rendered policy alone never passes.
5. While accepted/queued work exists, exercise API/backend crash, SIGTERM/forced loss and config/model replacement. Observe declared success/cancel/lost-response outcomes, safe old-process stop before reuse and no stale transcript/model under a new configuration.
6. On one allocated GPU, upgrade without surge; deliberately bad startup remains unready; rollback restores pinned prior model/images/config with cache preserved. Observe GPU/Pod lifetime, bounded stop/start, generation mismatch protection and documented downtime.
7. Uninstall only the test release and verify chart-generated cache plus every operator-owned claim/file is retained unchanged. Reinstall can reuse valid cache offline without model-source traffic. Separate explicit disposal remains outside this packet.

Use the packaged candidate, not an unrecorded local chart tree. Verify anonymous OCI and classic Helm pulls yield the same archive bytes; install once from OCI and use the second-channel package for the retained-cache reinstall/transcription smoke. Record actual image registry overrides and source/chart/model identities. Identical bytes do not require duplicating the full GPU suite, but both channel installation commands must be exercised.

Harness/checks to create or extend: TestHelmDeployment, TestPrivatePairing, TestResidentSuite, TestClientCompatibility, TestResidentMeasurements, TestSingleGPURollback, TestClaimRetention.

```bash
rtk proxy env WHISPER_TEST_TARGET=kubernetes go test ./tests/acceptance -run 'TestHelmDeployment|TestPrivatePairing|TestResidentSuite|TestClientCompatibility|TestResidentMeasurements|TestSingleGPURollback|TestClaimRetention' -count=1 -v
```

Require harness to validate explicit kubeconfig/context/namespace/release before writes; never default to an arbitrary kube context. Resolve target settings from the confirmed .local/homelab inventory at the authorized gate, without embedding private credentials/identifying values in fixtures or harness source. Use explicit kubectl --kubeconfig/--context and Helm --kubeconfig/--kube-context selectors. Keep private settings out of process diagnostics and public reports. Kubernetes Go/shell clients may be narrow test-only dependencies; no production cluster-client dependency is needed.

PROVE boundary/retention/no-surge checks by one scoped test-release mutation before final verification (widen policy/route to wrong backend/remove retention or introduce surge); ensure the check actually detects the breach, restore and run final real target suite. Do not remove storage belonging to the operator for a mutation. Any required unsupported target/CNI/hardware check keeps P08 incomplete with a concrete correction/access action and established owner.
Record effective logging/rate/proxy settings with each workload. For FIFO/resource/resident bursts, explicitly raise/disable rate limiting in that scenario so assertions exercise queue/resource ownership; retain dedicated enabled-limiter checks elsewhere. A rate_limit_exceeded response is not evidence of queue_full. No candidate code/guard is changed to obtain acceptance.

## Completion

Record exact successful/failed/unrun checks, environment/artifact identities and any mutation/fix. Recheck pinned specification freshness, update this packet's evidence, its sole [todo.md](todo.md) checkbox and current [handoff.md](handoff.md), preserve unrelated work and stop. No automatic next packet, commit, publication or unrelated infrastructure work.

## Technical proposal

Reuse P07's acceptance workload/measurement/assertion code with a separate Helm/Home Lab target adapter; do not duplicate the suite or substitute Docker results. Bind ../core/contracts.yaml deployment/models/private_backend/configuration/delivery; ../quality/scenarios.yaml homelab_gpu, chart_matrix, observation and P08 scenarios, including Q-PAIRING/Q-ROLLOUT/Q-PERFORMANCE; public OpenAPI applies unchanged.

Before any mutation, read the ignored .local/homelab README/inventory and refresh authenticated read-only access, explicit kubeconfig/context, target permissions and actual RTX 2060/driver/VRAM, Kubernetes/Helm/CNI/storage/runtime prerequisites. Keep private inventory/credentials/identifying values only there with directory 0700 and credentials 0600. Never dump them into tracked reports, CI artifacts or image/chart inputs. kubectl always selects kubeconfig/context and scoped namespace; Helm selects kubeconfig/kube-context and explicit release. Do not change the global context.

Use R01's exact public candidate image digests and chart archive/OCI identities, testing both OCI and classic chart paths and both image registries. Generic render/version-skew checks do not prove this actual cluster. Install/upgrade/rollback/uninstall only the separately authorized isolated namespace/release on the actual RTX 2060. No driver/plugin/CNI/storage/Ingress installation, RBAC expansion, label mutation or displacement of unrelated GPU workloads. Optional Ingress tests require existing specifically authorized controller/host/TLS inputs; default is disabled.

Run the same real CUDA public transcription, client/format/isolation, FIFO/cancel/health/privacy/resident and performance evidence as P07, with independently recorded target outcomes. Verify real CNI positive paired API-n -> backend-n and negative non-paired/same-namespace/other-namespace/native access. Use only authorized disposable probe Pods/namespaces. Where two-pair concurrent runtime checks need more GPUs than available, test pair identities/policies with bounded non-GPU probes plus actual installed pair inference; do not claim a second GPU workload ran.

Exercise real one-GPU ordered replacement, config/model change, dependency loss, active/waiting SIGTERM, failed upgrade/rollback and uninstall/reinstall. Observe old/new Pod/native/GPU lifetime, generation compatibility, explicit downtime and retained generated/operator PVCs/cache/prepared files. Routine cleanup deletes only this run's transient workloads, not claims or operator files.

Use the shared test-first/PROVE assertion harness without weakening it for access/hardware limits. Publish only sanitized outcome/performance reports and exact public artifact identities. Any runtime/rendered fix returns to its owner and a new R01 candidate with affected acceptance renewed. Manual target/deployment authority is required independently of this planning answer; missing prerequisites keep P08 incomplete with the established owner and concrete next action.

## Technical agreement

Selected proposal: `p08-homelab-gpu-evidence-v1`. Packet: `P08`.

- proposal_digest: `73141b2225d0865a53871cc9b07d3af9da72d5e035ce15de2284c9f78cfaab51`
- agreement_ref: `{id: be6db71a-6ebf-4f95-bca4-b81f2da2171a, revision: 4}`
- sources: this packet's `Technical proposal` and `plan.md#shared-technical-commitments`.
- provenance: actual synchronous Request User Input acceptance preserved in the managed record; revision 4 corrects only the evidence name Configure to Install and preserves that original answer, not a fresh reply.
- planning_packet_check: `ready`, no issues (2026-10-22); rechecked after repair. Technical readiness does not satisfy dependencies or authorize implementation/external actions.

Recheck the exact reference/digest and pinned specification before and after execution. Keep metadata/progress outside the frozen sections.

## Execution stages

For each affected behavior stage, write and run its discriminating check first (RED), implement/correct until the correct check passes (GREEN), then apply required one-at-a-time PROVE mutations and restore them before final validation. Missing tools/symbols/hardware alone are not behavioral RED; required unavailable or failed checks keep the packet incomplete.

1. Refresh private Home Lab access/inventory read-only and confirm exact scoped mutation authority.
2. Resolve candidate chart/images anonymously and revalidate generic matrix against actual target/skew facts.
3. Use the shared harness with real Helm install and actual RTX 2060 public CUDA inference.
4. Exercise actual CNI pairing/isolation and one-GPU replace/failure/rollback/uninstall/retained-cache behavior within authorized scope.
5. Run independent resource/performance/reader-facing recipe evidence, sanitize report and clean only owned transient workloads.
