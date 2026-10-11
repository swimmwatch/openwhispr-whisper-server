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
- The P07/P08 harnesses are planned and not implemented. No executable GPU
  acceptance command is currently available. Report this prerequisite honestly;
  do not install drivers/plugins, create substitute workloads or invent a runner.
- When their owning packets provide actual commands, verify immutable candidate
  image/chart identities, target hardware/driver/runtime and the specified GPU
  use, no CPU fallback, transcription, readiness, cancellation/cleanup, offline
  cache, restart/rollout and retention evidence. Record commands/results/artifacts;
  resource advertisement, validators and successful Pod startup alone do not
  complete Whisper acceptance. Update this skill only with real owning harnesses.
