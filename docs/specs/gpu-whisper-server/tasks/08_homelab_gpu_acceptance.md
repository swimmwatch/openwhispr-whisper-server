# P08 — RTX 2060 Helm acceptance, recovery and rollback

Outcome: the actual Home Lab target runs public CUDA transcription and survives declared lifecycle/storage transitions.
Requirements: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-007, REQ-011, REQ-012, REQ-013, REQ-014, REQ-016, REQ-017, REQ-018, REQ-019, REQ-020, REQ-027, REQ-028.

## Reviewed inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Status: planned; not executed. Verify the confirmed scope and pinned Approved specification/review before invocation. Explicit invocation, usable Git worktree, passing dependencies and named target access are required. Git/worktree preparation is planned in I01; its explicitly invoked executor owns that prerequisite. Planning has not initialized Git. Read linked exact requirements via MCP, recheck guidance/.codegraph and pinned source before work and completion. All commands run from project root with rtk. Create/run discriminating checks before any fixes; record relevant RED and corrected GREEN. A missing environment is not semantic RED. PROVE newly written absence/wiring assertions with one reversible mutation and restore. Required missing hardware/access/checks keep the packet incomplete.

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

