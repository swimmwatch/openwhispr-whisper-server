# v0.1.0-rc.5 publication and server verification

**Completed: R01 publication/read-back and the selected server-only HTTP/GPU stage
passed** (2026-10-27). Release state remains `candidate_published_unqualified`.
This is not a stable release or complete P07/P08 qualification.

## Selected publication

- [PR #13](https://github.com/swimmwatch/openwhispr-whisper-server/pull/13) merged
  after [CI 40491959663](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40491959663)
  passed all seven required jobs on `78a82b1fccd99b23cbcca9db3915f1fa7ed9d533`.
- Exact merged/tagged source: `a98b7ccc8186ec8557d6430e005ad83ecfc2f4fa`.
- [Release 40499791944, attempt 1](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40499791944/attempts/1)
  passed every required job. Stable-only aliases were intentionally skipped.
- [GitHub pre-release](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.5)
  published `2026-10-27T13:04:38Z`, not draft. The anonymous latest-release endpoint
  returned HTTP 404: no stable/latest release is designated.
- The user explicitly selected rc.5 after the [rc.4 integrity-check failure](<release-rc4-failure.md>).
  rc.4 stayed frozen and failed, not overwritten or relabeled passed. Normal
  protected reviews were used; protection was never disabled or bypassed.

## Immutable identities

| Artifact | Identity |
| --- | --- |
| API index, both registries | `sha256:f0415bd794f12d1154cbc06f8cad5520802593d31ae8d15f65d1aaa7c3a3fe16` |
| Backend index, both registries | `sha256:017580bf66b109f5ccf16e243ed0d9d1ccdd3f6725af5b3c8149611b595150f9` |
| Chart archive SHA256 | `414bb9d5826b1d5ec33b0831e1a75d09cd31c9230a2200465425bb67e824c997` |
| Separate chart OCI manifest | `sha256:34f5c82096820873e3d2e626e9652ab4c35e72906dd7c2aa193d2727f5089e77` |
| Build input identity | `sha256:23a4c1472d2627186dde1b46591523f9c3e763e7743074b6c8c1f891ee39a3be` |
| Runtime input identity | `sha256:2cbb5d9db5451a3cbba00d76af845f35e4e86454bfe113eca10bda45c99eddbd` |

Image repositories are `ghcr.io/swimmwatch/openwhispr-whisper-server-api`,
`docker.io/swimmwatch/openwhispr-whisper-server-api` and corresponding `-backend`
repositories. Image tags use `0.1.0-rc.5`, not a `v` prefix; the full-source alias
is `sha-a98b7ccc8186ec8557d6430e005ad83ecfc2f4fa`. Chart version is `0.1.0-rc.5` at
`oci://ghcr.io/swimmwatch/charts/whisper-server` and the
[Pages repository](https://swimmwatch.github.io/openwhispr-whisper-server/).

## Independent checks

| Check | Result |
| --- | --- |
| Anonymous GitHub downloads and SHA256SUMS including GitHub API asset digests | PASS |
| Existing candidate gate on the downloaded final inventory | `candidate_published_unqualified` |
| Existing complete security gate on the actual selected release report | PASS |
| Anonymous native Helm OCI and classic pulls with empty isolated client state | PASS |
| OCI/Pages/GitHub/classic chart archives, byte equality and SHA256 | PASS |
| Raw chart OCI manifest digest and content layer, distinct archive identity | PASS |
| Complete older rc.2/rc.3 index entries and fresh prior-archive checksums | PASS |
| Published chart strict lint/render, both exact registry/digest overrides | PASS |
| Normalized render equivalence, eight resources per registry | PASS |
| Four real anonymous Docker pulls, exact runtime/config identities | PASS after disclosed retry |
| Actual SPDX/SLSA and embedded source/base/tool/component identities | PASS |
| GHCR resident baseline, 60 successful requests and worker continuity | PASS |
| GHCR public HTTP matrix: 10 formats, 7 options, 34 negative/raw cases | PASS, all 51 |
| Bounds/isolation, FIFO/overflow/cancel/queued deadline | PASS |
| Limiter, active cancel/deadline, API/backend/native death, restart/down-up | PASS, eight lifecycle cases |
| Active API SIGTERM drain / SIGKILL recovery, model observer, no-GPU fail-closed | PASS |
| Genuine SDK JSON/text HTTP, without Desktop | PASS, both cases |
| New longest-input / three-by-20 resident/CUDA/model-I/O workload | PASS, 60 successes / 0 errors |
| Docker Hub selected references, public matrix and lifecycle/recovery | PASS, 51 plus eight cases |

The actual selected security report has no blocking findings or unresolved coverage
gaps under the owning policy. It still reports applicable native MEDIUM advisories
`CVE-2026-107676`, `CVE-2026-107678` and `CVE-2026-17513`, alongside other nonblocking
package findings. This is not a zero-vulnerability or signature/SLSA-level claim.

The first backend GHCR pull reached its 900s timeout. The identical backend from
Docker Hub then pulled successfully; one bounded 300s GHCR retry passed with
existing content layers. All four real pulls and inspections ultimately passed;
the original timeout remains recorded, not relabeled successful. Its exact
transfer/daemon cause is unverified. Actual source/version aliases, index/platform/
config/SPDX/SLSA bytes and embedded metadata agree across both registries.
BuildKit statement subjects are empty but linked by checked OCI attestation
annotations; backend SPDX does not separately detect whisper.cpp. Explicit native
metadata and advisory coverage are retained rather than claiming complete scanner
detection or cryptographic certification.

## Server scope and evidence

The user selected direct server HTTP/GPU checks, without Desktop/Electron. Actual
workloads used these new immutable published references through canonical Compose,
the committed shared harness and verified public audio/model fixtures. The live
binding records rc.5/source `a98b7ccc8186ec8557d6430e005ad83ecfc2f4fa`, these image
indexes, inventory/configuration/audio hashes and actual container/engine image IDs.
Earlier rc.3 results are not reused as rc.5 acceptance evidence.

The selected GHCR suite passed in 110.155s, including its 60-request baseline.
SDK HTTP passed in 4.019s; the independent resident measurement passed in 22.018s;
the focused Docker Hub recipes passed in 53.20s. Both measured 60-request series
had no errors. The independent measurement had public median **98.475ms**, p95
**416.324ms**, queue median 0.338ms, private HTTP median 88.000ms and observed API
overhead median 9.813ms. Fifteen correlated native GPU samples recorded peak SM
38% and framebuffer 557842432 bytes; observed recurring model-path operations: zero.
Initialization-to-CUDA-ready was 558.478ms, not pure weight-file I/O.

Hardware/runtime refreshed before execution: RTX 5070 Ti Laptop, driver 595.99.02,
12227 MiB, Docker 29.9.2 / Compose 5.6.1, actual Go 1.27.3. Other GPU consumers
were present and left untouched. These are observed, instrumented/shared-GPU
conditions, not an isolated benchmark or numeric SLO. Model: public multilingual
base SHA256 `60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe`;
JFK WAV and all 15 public fixture checksums were verified anew. No personal audio.
No calibrated idle/retention/failure-memory-envelope or universal leak proof is
inferred from successful requests or sampled resources.

All parent and delegated jobs were collected. Ordinary bounded cleanup left no
owned acceptance containers; operator processes, containers and caches remained.
The pipeline is terminal, so no new release watch or model polling is needed.

Private receipts: [final inventory](<../../.local/release-watch/rc5-readback/channel-state.json>),
[selected security report](<../../.local/release-watch/rc5-readback/security/_temp/selected-security.json>),
[image verification](<../../.local/release-watch/rc5-readback/images/result.md>),
[chart verification](<../../.local/release-watch/rc5-readback/charts/result.md>),
[GHCR server HTTP](<../../.local/p07/rc5-run/server-http.log>),
[SDK HTTP](<../../.local/p07/rc5-run/sdk-http.log>),
[Docker Hub HTTP](<../../.local/p07/rc5-run/dockerhub-http.log>),
[measurement](<../../.local/p07/run/published-2885353015/resident-measurements.json>),
[actual source/configuration binding](<../../.local/p07/run/published-2885353015/candidate-binding-3440275362.json>).
Full P07 cache/idle/envelope/sensitivity/comparison gates remain outstanding;
P08/Kubernetes and stable were not invoked. No next packet follows automatically.
