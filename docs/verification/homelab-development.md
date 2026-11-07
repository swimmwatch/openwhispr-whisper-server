# Home Lab development-first verification

Status: in progress, 2026-10-29. This is development/prevalidation on the explicitly
selected existing operator cluster, not published-candidate P08 qualification.
The user requested iterative application/chart fixes BEFORE another RC. No new
RC, image publication, commit or push was performed. P07's accepted limitations
and its completed packet status are not rewritten by this work.

## Deadline, recovery, second model and cross-namespace isolation — 2026-10-29

One approved GPU window ran the remaining scenarios serially and completed its
cleanup successfully. With a five-second development operation deadline, a real
CUDA-active long request and its subsequent waiting request each returned the
canonical `408 operation_timeout`; the backend recovered to `ready`/CUDA state.

A real client timeout (`curl` exit28) interrupted a CUDA-active request. The
backend recovered with a new native generation and unchanged configuration
fingerprint; a subsequent public transcription returned200. Deleting only the
owned API Pod while another CUDA-active request existed allowed its graceful200
completion; its controller replacement restored API readiness while preserving
that recovered backend generation.

The fixed resident model was then switched to `tiny` through the chart's canonical
configuration. Backend health reported `tiny`/`cuda_ready=true` with a different
fingerprint and public transcription returned200. Baseline was restored afterwards.

A non-GPU disposable Pod in a separate temporary namespace received the exact
permitted API labels but its connection to the ordinal backend timed out. This
proves namespace scoping in the actual CNI policy; the chart policy was not widened.
The temporary namespace was deleted. The final cleanup succeeded: baseline chart
configuration is deployed, owned backend is scaled0, and Ollama is again1/Ready/
Available. No model/PVC was deleted and no RC was created.

This closes the remaining development scenarios, not formal P08 qualification.
Outstanding work is result/build consolidation and any separately authorized
published-candidate qualification; target performance is still not claimed.
Next action/owner: the agent consolidates development evidence without publishing.

## FIFO and queued cancellation — 2026-10-29

The actual HomeLab queue entry passed49.503s using the shared Docker/Kubernetes
workload, not a second queue implementation. Two waiting slots were filled;
overflow returned `queue_full`, dispatch order matched admission, queued client
cancellation never dispatched, active work completed, request scratch was released
and the ready CUDA resident generation/fingerprint stayed unchanged. An explicit
wrong-namespace identity control rejected the observer before inference.
CPU helpers passed5.631s, affected race checks70.462s and go vet passed.

Normal baseline configuration was restored, the owned backend stopped and its
GPU process list was empty before Ollama was restored to its original1 Ready
replica. No model/PVC deletion or new RC. Remaining functional blocks are queued
deadline, active cancellation/failure recovery, second fitting model and
cross-namespace isolation. Next action/owner: the agent checks queued deadline;
already successful full HTTP/SDK matrices are not repeated without a new reason.

## Recovery, rollback and offline retention — 2026-10-29

Using the locally packaged development chart, a valid missing-prepared-file
scenario was installed with our retained claim mounted read-only. The backend
exited1 with `provisioning_failed`, API readiness returned503 and the actual GPU
compute-process list was empty. No valid model/cache data was removed.

Helm rollback to the recorded baseline succeeded. Only the owned Pods were
recreated to avoid the known unready StatefulSet revision hold. Both roles became
Ready; the original configuration fingerprint returned with a different native
generation. Public transcription then returned200. Model SHA256,147951465bytes,
mtime and inode were identical before/after. A controlled60-sample GPU capture
around this request observed nonzero SM activity (1% and4%), supplementing the
earlier resident-process/CUDA-readiness evidence. This is not a performance gate.

Before uninstall, actual Retain/Retain policy and claim ownership were checked.
Foreground uninstall of ONLY the test release completed; the same Bound claim/PV
survived, and no native GPU compute process remained. No namespace/PVC was deleted.
An additional temporary policy denied all backend egress. Actual access to the
configured HTTPS model source timed out, while paired health remained reachable.

The same packaged chart was installed again with its original baseline model
configuration and the deny-egress policy still active. Both roles became Ready;
model source access still timed out, but public transcription returned200.
The same claim identity, model hash/size/mtime/inode were preserved; the native
generation changed while the canonical fingerprint stayed unchanged. Thus this
is an actual offline retained-cache reinstall, not just a rendered retention flag.

The temporary egress policy was removed, the owned backend stopped gracefully
and its GPU compute-process list was empty. Ollama was restored to its original1
replica with a successful readiness rollout. Remaining work includes FIFO/deadline/cancellation,
active-failure scenarios, another fitting model and cross-namespace isolation.
Formal published-candidate P08 acceptance is still not claimed.
Next action/owner: the agent verifies cleanup/Ollama recovery, then exercises those
remaining development cases; no RC/publication or unrelated infrastructure change.

## Earlier live development results — 2026-10-29

The preload finished successfully (Pod Succeeded, exit0). During the next approved
GPU window the backend became Ready. Its actual paired health response reported
`base`/`cuda_ready=true`; NVIDIA's process query showed `whisper-server` using
412MiB. A short utilization sample stayed at0%, so it is not independent proof of
kernel activity or a qualified measurement.

The [development HTTP suite](<../../tests/acceptance/development_http_test.go#L16>)
passed on the actual Kubernetes API in47.603s. Official OpenAI SDK JSON/text each
passed all10 codec fixtures; liveness/readiness and the existing public-contract
positive/options/negative cases passed. Public speech was checked through bounded
hash/allowlisted observations, not transcript logs. First SDK request35.73s is
observed execution history, not a performance qualification.

Actual same-namespace NetworkPolicy discrimination passed: wrong API ordinal
could not connect; giving the same disposable non-GPU probe the permitted ordinal
allowed health access; restoring its wrong ordinal again denied access. The
backend policy itself was not widened. Cross-namespace isolation remains untested.

Both unpublished local amd64 Docker builds passed. Existing image contract and
security tests passed52.787s; CPU acceptance helpers passed4.836s and go vet passed.
No image/RC publication. Cluster images remain the recorded immutable baseline;
these local build results are not mislabeled as newly deployed candidates.

The backend was then gracefully stopped; the GPU process query was empty after
termination. Its cache claim was kept. Disposable probe deletion's first30s wait
timed out at its normal grace boundary; a separate90s observation finished, without
forcing a running workload. Ollama was independently restored to its original1
replica with a successful readiness rollout. The loopback forward was closed.

Still unverified: FIFO/cancellation, active-failure recovery, alternative-model/
invalid-artifact startup, rollback, offline reinstall/retention and target
measurements. Formal P08 and the overall development objective remain open.
Next action/owner: the agent verifies Ollama restoration, then continues those
remaining lifecycle/network/storage checks within the existing authorizations.

## Initial application progress — before the separately approved driver repair

- Dedicated access refreshed without changing the global kubeconfig/context.
  Actual Kubernetes1.35.3 and Helm4.3.0 respond; existing CNI, RuntimeClass,
  storage and permissions were inspected. Private identifiers, target settings,
  credentials and full diagnostics remain outside tracked artifacts.
- Installed the working-tree development chart into a new isolated owned
  namespace/release. API/backend remain separate ordinal-paired StatefulSets,
  with private backend Service/NetworkPolicy, non-root/read-only execution and
  retained per-backend cache. No Ingress/public exposure or unrelated workload,
  driver, node, GPU operator, storage or RBAC changes were made.
- Reused the already available rc.5 API/backend digests for the FIRST development
  baseline; no new candidate per iteration. Chart fixes are tested directly
  without relabeling or replacing those image bytes. Changed application builds
  will require their own verified development delivery route to cluster nodes.

## First actual chart failure and narrow correction

The first live API container exited1 immediately with `configuration_rejected`.
Kubernetes defaults `enableServiceLinks` to true and injects Service environment
variables. Service names with the application's prefix produce unknown
`WHISPER_*` variables, while the service intentionally rejects unknown settings.
The behavior was traced to the strict environment consumer in
[configuration sources](<../../internal/platform/config/sources.go#L93-L110>).
Actual target API documentation (`kubectl explain pod.spec.enableServiceLinks`)
confirmed the default injection behavior.

The shared [StatefulSet Pod template](<../../charts/whisper-server/templates/statefulsets.yaml#L31-L35>)
now disables service links for BOTH roles. Ordinal pairing continues to use DNS.
No configuration validation, image identity, GPU guard, privilege or network
policy was weakened.

The failed, unready StatefulSet Pod retained its old controller revision after
upgrade. After verifying the namespace identity and the own Pod owners, only the
two owned Pods were gracefully recreated from the corrected template. PVCs and
foreign workloads were untouched.

Observed regression evidence:

- RED: original cluster API process exited1 / `configuration_rejected`.
- GREEN: corrected API passed its actual startup HTTP probe; direct public API
  `/health/live` returned200. `/health/ready` correctly remained503 while the
  backend could not obtain a GPU. Temporary loopback-only port forwarding was
  closed after checks.
- Existing `TestChart*` integration checks passed18.570s, including pinned Helm3/4
  matrix, configuration parity, packaging, invalid inputs and boundary cases.
  Current-target strict lint/render and Helm server dry-run also passed. These
  do not substitute for real CUDA transcription/network/storage verification.

## GPU repair and shared test window — 2026-10-28

The exact compiler error was subsequently captured: the open570.124.06 module
calls `pci_resize_resource` with three arguments against a kernel declaration
requiring another argument. The user explicitly authorized a narrow repair through
the existing GPU Operator. A general version upgrade was not applied: checked R580
release notes require DCGM4.3+, outside the existing4.1.1 monitoring configuration.
See [NVIDIA's release notes](https://docs.nvidia.com/datacenter/tesla/tesla-release-notes-580-178-04/index.html#known-issues).

Instead, changed only the documented `driver.kernelModuleType` from `auto` to
`proprietary`, keeping570.124.06 and all other component versions. No manual kernel
patch or node reboot. Before mutation, actual node state had no running allocated
GPU clients; namespace/policy identity and unchanged settings were checked.
The [Operator supports this setting](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/25.3.0/getting-started.html#gpu-operator-helm-chart-options).

Actual results: new driver Pod became Ready; `nvidia-smi` reports RTX2060,
6144MiB and570.124.06; device plugin/toolkit/validator/exporter became Ready;
allocatable GPU returned to1, Operator reached `upgrade-done` and uncordoned the
node. The initial `rollout status` observer was inapplicable to the Operator's
OnDelete DaemonSet; this observer error was corrected to an actual Pod readiness
watch, not relabeled a successful rollout check. The driver setting is persisted
in ClusterPolicy; owning Helm values have not yet been aligned, so a later Helm
reapply must deliberately preserve it.

Ollama then acquired the single GPU. The user explicitly approved pausing ONLY
that application for tests, preserving its original scale and restoring it
subsequently. Its original deployment and claims were recorded; no HPA or matching
GitOps application was found. Desired replicas were changed1→0 with identity and
current-value checks. The agent MUST release the development backend's GPU and
restore Ollama1 replica plus verify readiness before closing this test window.
No model/PVC deletion is permitted.

The development backend then scheduled onto the GPU node; its retained10Gi cache
claim became Bound. A600s readiness observation timed out while the container was
still pulling its first image, not running Whisper. Read-only containerd content
status confirmed two matching image layers actively downloading (244.3MB and
270.5MB received after11minutes); the full compressed image is about2.7GB. This
is delivery progress, not CUDA or readiness proof.

To avoid unnecessary Ollama downtime during this CPU/network-only preparation,
the owned backend is temporarily scaled0 and Ollama restored toward its original1.
A non-root, read-only CPU-only preload Pod on the same target node downloads that
exact image and exits without inference. It requests no GPU, sets
`NVIDIA_VISIBLE_DEVICES=void`, mounts no model/cache/PVC and receives no API token.
No manual parallel registry pull, new image/RC or infrastructure tuning is used.
The cold image pull held the terminating owned backend Pod beyond its grace
period. Before removing that API object, the agent verified its exact UID,
confirmed an empty CRI container list, stopped its specific sandbox, verified
`SANDBOX_NOTREADY`/PID0 and removed that sandbox. Thus no future native start could
be inferred safe merely from API deletion. No runtime configuration, foreign
sandbox, PVC or model was removed. Ollama was then verified at original desired1,
Ready1, Available1. The existing layer downloads retained progress (475MB and
510.7MB after19minutes), rather than being replaced by another manual pull.
The next actual inference window reuses the user's explicit temporary-pause
permission and must again finish by restoring Ollama. No transcription PASS yet. A new
[development HTTP entry](<../../tests/acceptance/development_http_test.go>) reuses
existing real SDK/public-contract cases without a new cluster-client framework.
Its compile check is not a live-target pass. Both Go components built locally,
and the corrected development chart packaged locally without publication.

Next action/owner: the agent waits for the actual backend image/model readiness,
runs real CUDA/public/SDK and lifecycle/network/storage checks in the approved GPU
window, then restores Ollama. The following failure history is retained, not the
current GPU diagnosis or authority.

## Earlier GPU prerequisite failure — retained execution history

Initial and repeated observations found zero allocatable GPUs. Bounded startup
observation was followed by fresh logs rather than assuming a permanent failure.
The existing operator did progress through package-cache and kernel-header setup.
However, the actual CURRENT-DAY driver attempt21:22–21:26Moscow then exited1:
`Building kernel modules` failed and the NVIDIA kernel module was not created.
This is NVIDIA570.124.06 with the actual6.8.0-147-generic kernel. A subsequent
attempt was again running; restart count reached108. These are fresh observations,
not reuse of the historical Oct25 installer failure. The exact compiler-level
cause remains unknown: a read-only log-copy attempt failed because the driver
container was no longer present at the time. No alternate privileged workaround
or repair was attempted.

Backend startup observation timed out after300s waiting for readiness; scheduling
reported insufficient GPU resources. The claim waits for its first scheduled
consumer. No public CUDA transcription, backend recovery, real pair isolation,
upgrade/rollback, retained-cache reinstall or target performance PASS is claimed.
The user asked to recheck startup and did NOT grant infrastructure repair.

Next action/owner: the agent continues bounded read-only driver diagnosis and
application verification when GPU readiness permits. Any required NVIDIA/kernel/
containerd repair or disruption of another workload needs explicit operator
agreement before that dependent action. The development objective remains open;
formal P08 candidate acceptance will need a refreshed technical binding after the
requested development-first phase, not a fabricated pass from this report.
