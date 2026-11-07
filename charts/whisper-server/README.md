# Whisper Server Helm chart

One application chart deploys a public Go API and its dedicated CUDA backend in
**separate, ordinal-paired StatefulSets**. Each API Pod calls only its backend
ordinal through a private Service; NetworkPolicy restricts that listener to the
paired API when the cluster CNI enforces it. Native HTTP stays on backend loopback.

> **Evaluation, not full qualification:** the recorded rc.6 artifacts have incomplete
> GPU/memory/replacement evidence and expired security evidence. They do not include
> the working-tree short-audio mitigation. See the
> [release limitations](https://github.com/swimmwatch/openwhispr-whisper-server/blob/main/docs/verification/release-1.0.0-notes.md)
> and [rc.6 report](https://github.com/swimmwatch/openwhispr-whisper-server/blob/main/docs/verification/release-rc6.md).
> Lint/render success is not deployment acceptance. There is no built-in authentication.

## Prerequisites

Linux amd64 nodes; compatible NVIDIA drivers/runtime/device integration; an enforcing
CNI; and storage readable/writable by UID/GID `10001:10001`. Only backend Pods request
GPUs (one by default). Additional replicas need additional GPU capacity and compatible
storage. No global queue, autoscaling or CPU fallback is supplied. Optional Ingress
requires an existing controller and, when configured, an existing TLS Secret.

The chart does not install any of those prerequisites, create credentials or grant
access control. Its API Service is ClusterIP and Ingress is disabled by default.
Anyone who can reach the API can transcribe; ClusterIP and TLS are not authentication.

## Select an exact chart and both images

The source [Chart.yaml](<Chart.yaml>) and [values.yaml](<values.yaml>) use
`0.0.0-dev`; these are **unpublished** defaults, not runnable registry references.
Chart version and quoted `appVersion` are separate identities. Packaging a release
sets its actual versions and default image tags; image digests take precedence over
tags. Select a reviewed inventory and do not mix unrelated API/backend versions.

For reproduction of the **unqualified** recorded rc.6, download either channel to a
new local directory from the repository root:

```sh
mkdir -p .local/chart-evaluation
helm pull oci://ghcr.io/swimmwatch/charts/whisper-server \
  --version 0.1.0-rc.6 --destination .local/chart-evaluation

# Alternative channel; use it instead of the OCI command.
# helm pull whisper-server \
#   --repo https://swimmwatch.github.io/openwhispr-whisper-server/ \
#   --version 0.1.0-rc.6 --destination .local/chart-evaluation

printf '%s  %s\n' \
  b520bb93153191f074af232aaf4cee01706150e34008f6de5e8c6f05953c6b81 \
  .local/chart-evaluation/whisper-server-0.1.0-rc.6.tgz | sha256sum --check -
```

The archive SHA256 above is **not** the OCI manifest digest. These identities come
from the linked historical report; no fresh registry read-back is implied by this
guide. For a different version, obtain its own chart checksum and image inventory.
See [Helm's OCI documentation](https://helm.sh/docs/topics/registries/).

Save the following non-secret example as `.local/chart-evaluation/values.yaml`.
It pins both rc.6 images rather than relying on mutable aliases:

```yaml
images:
  api:
    repository: ghcr.io/swimmwatch/openwhispr-whisper-server-api
    digest: sha256:c309f5dfa0b3f03857cb4ac3077dd39ed1b947fd1f5217637eae19316a6ab636
  backend:
    repository: ghcr.io/swimmwatch/openwhispr-whisper-server-backend
    digest: sha256:45957d1a1c171557399ee313c4cdf6f385801104cd932c644bfe71788c027467
```

For Docker Hub, replace **both** repository prefixes with `docker.io/swimmwatch/`,
retaining the same recorded digests. Add only your confirmed storage/scheduling
settings; do not put credentials or private cluster identities in tracked files.

## Render, then install only on a confirmed target

For local source validation (no cluster contact):

```sh
helm lint charts/whisper-server --strict --values .local/chart-evaluation/values.yaml
helm template whisper charts/whisper-server --namespace whisper-eval \
  --values .local/chart-evaluation/values.yaml
```

This validates the current source, **not** the downloaded rc.6 package. To inspect
the selected release, run `helm template` against its downloaded archive instead.
The project render matrix uses Helm 3.22.0/4.3.0; it does not certify all live
Kubernetes versions. Check your actual cluster and selected Helm compatibility.

Before installation, set `KUBE_CONFIG`, `KUBE_CONTEXT`, `NAMESPACE` and `RELEASE` to
an explicitly confirmed target, existing namespace and chosen release. The names
are placeholders, not private inventory. Do not change the global current context.
This command mutates that target and is **not** a documentation-only check:

```sh
helm upgrade --install "${RELEASE:?}" \
  .local/chart-evaluation/whisper-server-0.1.0-rc.6.tgz \
  --kubeconfig "${KUBE_CONFIG:?}" --kube-context "${KUBE_CONTEXT:?}" \
  --namespace "${NAMESPACE:?}" --values .local/chart-evaluation/values.yaml \
  --wait --timeout 45m
```

A Helm wait does not prove CUDA inference, privacy, network isolation or memory
stability. Inspect the actual Pods/events when it fails. For this chart's default
resource naming, forward only the public API to local loopback:

```sh
kubectl --kubeconfig "${KUBE_CONFIG:?}" --context "${KUBE_CONTEXT:?}" \
  --namespace "${NAMESPACE:?}" port-forward --address 127.0.0.1 \
  "service/${RELEASE:?}-whisper-server-api" 8080:8080
```

Use the rendered Service name if name overrides or truncation apply. In another
terminal, check `/health/live`, wait for `/health/ready`, then send the public
multipart request described in the
[operator guide](https://github.com/swimmwatch/openwhispr-whisper-server/blob/main/docs/operations.md#start-and-check).
Stop port-forwarding when finished; do not forward supervisor/native ports.

## Values and ownership

Use the documented [values](<values.yaml>), strict [schema](<values.schema.json>)
and [canonical service contract](https://github.com/swimmwatch/openwhispr-whisper-server/blob/main/docs/specs/gpu-whisper-server/core/contracts.yaml)
rather than a second defaults table:

- `config` merges into the canonical JSON defaults; arrays replace. Exactly one
  `models.served` entry is allowed. Deferred/unknown fields reject. The API's
  backend URL is set from its ordinal, not a shared backend pool.
- `backend.gpu.requests` and `limits` must be positive and equal. The selected
  device ordinal is within that allocation; extra devices do not enable distributed
  inference. API resources contain no GPU request.
- `cache` creates per-backend claims unless `existingClaim` is set. Claims are
  retained on scale-down and uninstall. Multiple replicas sharing an existing
  writable claim require actual `ReadWriteMany` support and compatible locking.
- `prepared.existingClaim` mounts read-only at `/prepared`; declare its real access
  modes. Prepared model configuration uses that mount and `downloadSet: []` when
  no downloads are intended. The chart does not copy host model files into a claim.
- `transientStorage` creates separate disk-backed, size-limited `/tmp` emptyDirs.
  They are not the model cache. Increasing input/queue bounds may require larger
  scratch capacity; validation rejects undersized values.
- `api`/`backend` scheduling and resources are independent. Preserve non-root,
  read-only-root, dropped-capability execution and disabled service-account tokens.
- Startup/readiness/liveness probes are distinct. Backend liveness checks supervisor
  process existence, not inference progress. The termination grace must cover
  shutdown + cleanup + 5 seconds; raising budgets may require a longer Helm wait.
- `ingress` exposes only the API. Forwarded client-IP trust needs explicit proxy
  CIDRs in `config.api.clientIP` and header sanitization at the proxy. No default
  private-network trust or distributed rate quota is implied.

## Upgrade, rollback and removal

Keep the previous chart/configuration/image identities and persistent cache.
Controlled replacement uses ordered StatefulSets without surge. **One-GPU upgrades
and rollbacks have downtime**; both components must converge to compatible settings.
A failed unready StatefulSet revision may need carefully scoped operator recovery;
a rollback command alone does not guarantee recovery or stopped GPU work.

Use the same explicit target flags for history, rollback and uninstall:

```sh
helm history "${RELEASE:?}" --kubeconfig "${KUBE_CONFIG:?}" \
  --kube-context "${KUBE_CONTEXT:?}" --namespace "${NAMESPACE:?}"

# PREVIOUS_REVISION is a revision selected from that release's history.
helm rollback "${RELEASE:?}" "${PREVIOUS_REVISION:?}" \
  --kubeconfig "${KUBE_CONFIG:?}" --kube-context "${KUBE_CONTEXT:?}" \
  --namespace "${NAMESPACE:?}" --wait --timeout 45m

# Only when removal is intended:
helm uninstall "${RELEASE:?}" --kubeconfig "${KUBE_CONFIG:?}" \
  --kube-context "${KUBE_CONTEXT:?}" --namespace "${NAMESPACE:?}" \
  --wait --timeout 45m
```

Uninstall is not a PVC or namespace cleanup command. Verify retained claims and
actual process/GPU shutdown; do not infer either solely from deleted API objects.
Removing model data is a separate, explicit operator action, never routine rollback.

Project-owned chart files use [Apache-2.0](<LICENSE>); see [NOTICE](<NOTICE>).
Container contents retain their separate third-party licenses.
