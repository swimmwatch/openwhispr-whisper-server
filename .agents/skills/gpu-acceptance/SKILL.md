---
name: gpu-acceptance
description: Prepare or inspect explicitly authorized Whisper GPU acceptance evidence; preserve private target selection and report unavailable harnesses.
---

# GPU acceptance

Read root AGENTS.md and the explicitly invoked owning packet. This skill does not
authorize deployment, infrastructure changes, inference or probe workloads.

- P07 owns Docker/RTX 5070 Ti Laptop acceptance; P08 owns Kubernetes/RTX 2060.
  Read docs/specs/gpu-whisper-server/tasks/07_docker_gpu_acceptance.md or
  docs/specs/gpu-whisper-server/tasks/08_homelab_gpu_acceptance.md as applicable.
- For Home Lab read .local/homelab/README.md and private cluster.yaml. Use its
  confirmed kubeconfig/context and refresh read-only access/permissions. Do not
  infer the target from the current context. Scoped kubectl commands include
  --kubeconfig, --context and --namespace; Helm includes --kubeconfig,
  --kube-context, namespace and the confirmed release.
- Keep full inventory, connection details and credentials under ignored .local.
  Preserve permissions, global context and operator-owned claims/cache. Reports
  contain sanitized evidence, never credential values or private identifiers.
- P07 has an implemented strict published-target harness. Read
  [its actual selection/command reference](<../../../tests/acceptance/README.md>).
  `P07_GPU_TESTS=1` plus absolute `P07_PUBLISHED_SELECTION` selects the downloaded
  candidate inventory, registry and public input files; keep `P07_LOCAL_TESTS`
  unset. The separate local-only constructor still requires its own local inputs.
  From the project root with the pinned Go version, actual selected commands are:

  ```bash
  env -u P07_LOCAL_TESTS P07_GPU_TESTS=1 P07_PUBLISHED_SELECTION="$SELECTION" \
    go test ./tests/acceptance -run '^TestResidentSuite$/(baseline|public_contract|input_bounds_and_isolation|fifo_and_queued_cancellation|queued_deadline|lifecycle|active_api_shutdown|model_observer|gpu_unavailable)$' -count=1 -v -timeout=45m
  env -u P07_LOCAL_TESTS P07_GPU_TESTS=1 P07_PUBLISHED_SELECTION="$SELECTION" \
    go test ./tests/acceptance -run '^TestClientCompatibility$/^official-openai-(json|text)$' -count=1 -v -timeout=12m
  env -u P07_LOCAL_TESTS P07_GPU_TESTS=1 P07_PUBLISHED_SELECTION="$SELECTION" \
    go test ./tests/acceptance -run '^TestResidentMeasurements$' -count=1 -v -timeout=38m
  ```

  These require explicit workload authorization and exclusive ownership of the
  selected GPU acceptance lock. The SDK selector is HTTP-only, not Desktop.
  The user explicitly excluded Desktop/Electron in the current server stage.
  An unfiltered full P07 run still fails required unimplemented gates; partial
  server success does not qualify P07/P08 or authorize stable. Do not infer P08
  harness availability from P07; read its independently authorized owning packet.
  Report missing prerequisites honestly; do not install drivers/plugins, create
  substitute workloads or invent a runner.
- With the owning packet's actual commands, verify immutable candidate
  image/chart identities, target hardware/driver/runtime and the specified GPU
  use, no CPU fallback, transcription, readiness, cancellation/cleanup, offline
  cache, restart/rollout and retention evidence. Record commands/results/artifacts;
  resource advertisement, validators and successful Pod startup alone do not
  complete Whisper acceptance. Update this skill only with real owning harnesses.
