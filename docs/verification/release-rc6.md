# v0.1.0-rc.6 candidate publication

Status: published candidate, GPU qualification pending; security revalidation
currently fails on expired and stale evidence. The following facts are not formal
P07/P08 acceptance and do not authorize a stable release.

## Source and pipeline

- [PR #14](https://github.com/swimmwatch/openwhispr-whisper-server/pull/14)
  passed [CI 40934107731](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40934107731)
  on head `eec588a179947e6ba3d3cb0f37395241c1130718`; all seven required jobs
  succeeded. Merge/tag source is `994daa22d214739207ef52d04ffbe4a66f63174b`.
- [Release 40936341950](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40936341950)
  completed the cold image builds, runtime checks, selected security gate and all
  publication jobs. Stable aliases were intentionally skipped. Protected release
  environments were approved using the user's explicit authorization.
- The [GitHub release](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.6)
  is a published prerelease and not a draft; no stable/latest release exists.

## Identity and independent read-back

| Artifact | Immutable identity |
| --- | --- |
| API image index, GHCR and Docker Hub | `sha256:c309f5dfa0b3f03857cb4ac3077dd39ed1b947fd1f5217637eae19316a6ab636` |
| Backend image index, GHCR and Docker Hub | `sha256:45957d1a1c171557399ee313c4cdf6f385801104cd932c644bfe71788c027467` |
| Chart archive SHA256 | `b520bb93153191f074af232aaf4cee01706150e34008f6de5e8c6f05953c6b81` |
| Separate chart OCI manifest | `sha256:368429a1154eca89920c9469b425e32ba067a19199efb31d8bc45748e9e76309` |

Independent read-back with empty task-local Docker/Helm credential directories
verified the four raw image index digests and identical chart archive bytes from
OCI, Pages and GitHub. The downloaded candidate inventory passed the existing
candidate gate as `candidate_published_unqualified`; the actual selected security
report passed the complete security gate with no recorded coverage gaps. This
independent read-back also pulled all four exact image references with an empty
Docker credential directory; Docker reused layers already cached locally. On
2026-10-31 a new anonymous read-back again matched all four image indexes and the
three chart archives; the OCI chart manifest also matched. The release workflow
separately performed anonymous image read-back. Applicable
nonblocking native findings remain in the selected security report. That pass
was historical: on 2026-10-31 the saved security inventory failed fresh
revalidation because the scoped exceptions and component reviews had expired and
the selected scans exceeded the permitted freshness window. Two upstream GitHub
advisory feeds initially returned HTTP 403 through the configured proxy; direct
requests returned HTTP 200, and all 15 reviewed source hashes matched. Fresh
Trivy image scans of both pinned indexes found the same two HIGH/CRITICAL Echo
Proxy findings as the previous reports; the scoped exceptions have expired.
A fresh `govulncheck` source scan reported one module-level finding without a
reachable call and exited successfully. A fresh review also identified additional
whisper.cpp advisory records absent from the earlier component review. The pinned
native source contains the code described by [CVE-2026-17512](https://ubuntu.com/security/CVE-2026-17512)
for very short audio and [CVE-2026-110810](https://ubuntu.com/security/CVE-2026-110810)
for a malformed model. A CPU test confirmed that the Go supervisor forwards a
200-sample canonical WAV unchanged to a fake native endpoint; static inspection of
the pinned native server shows a possible short-buffer path. This does not prove
exploitation or the deployed server's end-to-end behavior. The malformed-model
case and [CVE-2026-10298](https://security-tracker.debian.org/tracker/CVE-2026-10298)
still require review against the pinned fork and its actual model inputs.
These checks do not renew an expired review or replace the complete security gate.
New, reviewed security evidence is required; expired exceptions were not renewed
automatically.

The extracted published chart passed the Helm 3/4 matrix, metadata, configuration
parity, invalid values, labels, cache mounts, proxy parity and boundary tests.
The published chart also passed lint and render with the observed target
Kubernetes version and private target values. A separate anonymous pull by OCI
manifest digest produced the same archive hash, and Helm accepted that digest in
a client-side install dry run. These are not cluster execution.
The first attempt supplied an absolute path to a helper that expects a path relative
to the repository and failed; the relative-path rerun passed the applicable cases.
The source-only package-exclusion injection case was omitted from that rerun because
it needs the source chart's `.helmignore`, not an already packaged archive; it had
passed in the actual PR chart CI. Neither test is runtime GPU evidence.

## Home Lab partial run — 2026-10-30

A target-cluster GPU packet used the published rc.6 artifacts. The public HTTP
and official SDK cases, FIFO and deadline checks passed, as did the two-model
functional exercise and a 60-request public steady-state sample. CUDA activity
was observed in discrete samples, but the observer did not establish a direct
generation-to-host-PID mapping. Active cancellation failed its combined
identity/readiness/scratch assertion. The replacement observer did not complete,
so the subsequent retained-cache/offline checks were not run. An unready Pod on
the previous StatefulSet revision had to be removed with UID and resourceVersion
preconditions for each role to unblock Helm rollback; this was not an autonomous
rollback pass. The test backend was returned to zero replicas, its retained claim
remained Bound, and Ollama returned to one Ready and Available replica. The
partial packet did not establish P08 qualification.

A second target-cluster run repeated the successful HTTP/SDK, FIFO, deadline,
model-change and steady-state exercises. Actual paired and non-paired CNI
connections and the scoped policy sensitivity check passed. The deliberately
unready configuration and rollback were observed, but that observation does
not uniquely identify the cause of provisioning failure. Active-cancellation,
API deletion, backend deletion and the sampled replacement observer failed or
could not establish their recovery evidence; the retention/offline stage was
therefore not run. After cleanup, Ollama was again one Ready and Available
replica, the test backend was at zero and the existing claim remained Bound.
The second packet also remains P08 incomplete.

A third target-cluster packet passed the functional active-cancellation and
graceful API-deletion checks, exercised backend-Pod deletion with active and
waiting requests, and observed a sampled replacement. Its 60 sequential public
requests had a median of 148.8 ms and p95 of 160.7 ms, with 60 successful
responses and no errors. Identified speech was checked in a separate preflight;
the timed samples verified response status and decoding, not every transcript.
The retained claim and cached artifact were verified before uninstall and after
reinstall, and an offline transcription succeeded with the model-source
connection denied. Later target runs attempted natural Go-heap calibration. After
fixing the log-reading limit, one observed window contained five natural API GC
cycles but no backend cycle. A subsequent window split 4,096 health requests
between both processes and observed three API and four backend cycles. The shared
calibration requires a barrier followed by three comparable cycles for each role,
so this result remained unknown; no memory qualification was obtained. The packets
did not measure all model-source traffic or continuously establish native process
or GPU lifetimes, so these results remain partial. Cleanup restored Ollama to
one Ready and Available replica, left the test backend at zero, and verified
the retained claim Bound. Owned probe Pods and policy were removed, while the
unique probe namespace was retained for manual disposition.

## Home Lab partial run — 2026-10-31

A further target window used the same immutable candidate. Public development
HTTP, FIFO, queued deadline, functional active cancellation and graceful API
Pod deletion passed. Backend Pod deletion with active and waiting requests,
sampled CUDA/replacement observation and rollback remained partial. Network
pairing and the policy sensitivity mutation passed; the retained claim and cache
were checked across uninstall/reinstall, and transcription succeeded while the
selected source connection was denied. These results do not establish complete
source-traffic, process-lifetime or GPU non-overlap evidence. The 60 sequential
public timing responses had zero errors, median 132.187 ms and p95 142.551 ms;
identified speech was checked separately in the preflight.

The new read-only Helm policy observer failed before the candidate-bound shared
public/SDK cases ran: it incorrectly required a component label on the
StatefulSet metadata, although the selected chart places it in the selector and
Pod labels. The repeated-resource observer stopped after five requests because
the new decoder sampler returned unavailable; the exact failure category was
not captured. The source code also revealed that its sampler incorrectly required
`ready` during a legitimate `busy` inference, but this alone does not prove the
cause of this failure. Both findings belong to the test harness, and the failed
results are retained rather than reclassified. Ollama returned to one Ready and
Available replica, the test backend was stopped and the retained claim remained
Bound. The unique probe namespace was retained for manual disposition.

A targeted follow-up fixed those two harness issues and reran them against the
same published artifacts. The actual Helm policy read, the shared P07 public
formats and official SDK cases, and 30 repeated requests completed successfully.
The decoder sampler took 10 serial samples and observed no descendant RSS in
those samples; this does not establish a zero peak, a memory envelope or full
P08. The earlier failed window remains recorded. Cleanup returned Ollama to one
Ready and Available replica, stopped the test backend and retained the Bound
claim. Formal P08 remains unrun.

A subsequent target window ran the shared P07 input-bounds slice on the selected
candidate: an exact-size WAV and one-byte excess, overlong decoded audio,
oversized prompt, and distinct-recording/prompt controls passed. Log and file
privacy checks were sampled and do not prove complete privacy. A separate
single-epoch memory diagnostic failed in its resource observer before any of its
20 requests; the recorded evidence does not identify which source failed.
A memory-only retry likewise failed before its first request, at GPU-sample
preflight; the specific failing operation was not recorded. A further retry,
after separating GPU allocation from activity observation, completed all 20
requests and captured one resource point but failed the first idle observation
at its final GPU identity check. A further retry completed 20 requests, recorded
a completion point and three idle points, then failed during host-process
enumeration. The specific read error remains unknown. After making the
observer tolerate a confirmed disappearing unrelated process while failing
closed on other errors, a further run completed 20 requests and all five idle
resource snapshots. This is one unfrozen diagnostic epoch, not the calibrated
memory or GC gate, and does not establish the cause of previous failures.
Cleanup restored Ollama to one Ready and Available replica, stopped the test
backend and retained the Bound claim after each window. Full P08 remains unrun.
A read-only follow-up on 2026-11-01 observed the test API at one desired but zero
ready replicas and the backend at zero desired replicas. A read-only health
check against the same API Pod and container identity returned HTTP 200 for
liveness and HTTP 503 for readiness, as expected with the backend stopped. A
valid public audio request also returned the expected HTTP 503 error envelope
with the same API identity and backend still at zero. The first attempt's local
response parser failed; the corrected repeat passed. No GPU workload or Ollama
check was run in this follow-up. The earlier successful
transcriptions therefore describe the test windows, not current availability.

The available CRI logs do not establish the P07-equivalent natural-GC barrier:
the selected [containerd 2.2.1 logger](https://github.com/containerd/containerd/blob/v2.2.1/internal/cri/io/logger.go)
can continue after a log-write error, while the [Kubernetes 1.35.4 log reader](https://github.com/kubernetes/kubernetes/blob/v1.35.4/staging/src/k8s.io/cri-client/pkg/logs/logs.go)
does not retrieve rotated files. Independent stdout and stderr delivery also does
not establish the ordering needed for the barrier. No complete source was found
under the current candidate and observation constraints; sampled logs must not
be promoted to a memory pass. The user chose to leave rc.6 unchanged rather than
start separate work on new production GC observability.

## Outstanding qualification

On 2026-11-01 the user directed that the remaining checks stop and requested
P08 closure and stable v1.0.0 publication. The user explicitly accepted and
requested public disclosure of the incomplete GPU/memory/overlap evidence,
unfinished final documentation, expired security reviews/scans and unpublished
short-audio mitigation for this release (decision
`066c9483-50e8-4436-bc05-6ca99936e2d4`, revision 4). The remaining P08 test
executions are cancelled for this release at the user's direction; no further
P08 workload was started. The requested closure does not establish the missing
results: the tests and security evidence described above remain incomplete or
failed, not passes.

P08 still requires the exact published artifacts, target-specific execution,
measurements and retained evidence. On 2026-10-31 the complete non-GPU acceptance
suite passed under `-race` in 845.581 seconds after the latest harness changes,
with the GPU opt-ins unset. An earlier run timed out at eight minutes and was
not counted as a pass. No opt-in
Kubernetes/GPU workload was executed by these CPU runs. The local
development scenarios and earlier
rc.5 GPU results are not substituted for rc.6 qualification. The existing named
Kubernetes acceptance entry points must be implemented or verified rather than
assuming the opt-in development smoke is the formal suite. The agent owns the
next action: complete P08 within the approved HomeLab/Ollama boundaries; record
all unsuccessful, missing or unrun checks. Stable remains a separate decision.
