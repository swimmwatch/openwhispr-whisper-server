# Current handoff

## Current release recovery — reviewed snapshot, then rc.5

rc.4 failed before any distribution write. Its native advisory integrity guard
rejected a changed Debian tracker page; the reviewed byte-level difference is
only forky package `7:9.0.2-3` → `7:9.0.2-4`, with all CVE information unchanged.
The user explicitly authorized the narrow snapshot correction, commit/PR/CI,
merge after successful checks and new immutable `v0.1.0-rc.5` with event-driven
Release monitoring and final anonymous/server HTTP checks. Existing rc.4 tag and
all earlier artifacts stay unchanged. The [failure/review report](<../../../verification/release-rc4-failure.md>)
records actual evidence and limitations. New rc.5 publication remains incomplete;
no Desktop, Kubernetes, stable or complete-P07 claim is authorized.

## Current server-only result — published rc.3 (2026-10-26, Europe/Moscow)

The user explicitly excluded Desktop/Electron and requested direct server HTTP
checks. **That selected server stage passed**, not complete P07. Do not ask again
for a desktop installation or run Electron under this scope.

Published GHCR rc.3 passed its resident baseline (60/0), all 51 public wire cases,
bounds/isolation, FIFO/cancellation/deadlines, eight lifecycle cases, both active
API shutdown modes, model observation and no-GPU fail-closed. Genuine SDK JSON/text
HTTP also passed without desktop execution. A separate longest-input/three-block
measurement passed 60/0 with correlated native CUDA activity and zero observed
recurring model-file operations. Docker Hub references separately passed the
public matrix and lifecycle recipes. All three workload jobs finished exit 0 and
were collected; owned acceptance containers were cleaned without volume deletion.
See the [actual server report](<../../../verification/docker-rc3-http.md>).

The published adapter/shared case increment and selected server report are
recorded in a separate follow-up; the exact private tested harness snapshot is
preserved. Full P07 cache/idle/envelope/sensitivity
and comparison gaps remain explicit; its checkbox must stay unchecked. The
monitor/report follow-up alone is committed as `29ea5fc` in
[PR #11](https://github.com/swimmwatch/openwhispr-whisper-server/pull/11),
[CI 40372264093](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40372264093).
All seven required jobs passed on exact head `29ea5fc`; the one-shot watch has
finished. The user then explicitly approved merging PR #11; it merged as
`f83dc1a7f39854b800a65095fb6e5220864dea05` on 2026-10-26. The rc.3 tag still
points to `a47647049ad69ae154e327d8d2536d650eaa4dee`; no artifact was republished.

Preserve concurrent user cache/action/CHANGELOG/release-document changes and all
published rc.3 bytes/tag. P08, cluster operations and stable remain unauthorized.

## Current completion — R01 rc.3 (2026-10-26, Europe/Moscow)

**`v0.1.0-rc.3` is published and independently verified as
`candidate_published_unqualified`.** The user explicitly approved PR #10 as the
source and rc.3 as the new immutable version. Merged/tagged source is
`a47647049ad69ae154e327d8d2536d650eaa4dee`; CI 40333890561 passed all seven jobs,
and release 40347039761 passed every required candidate job. Stable-only aliases
were intentionally skipped. All six channels and GitHub assets passed anonymous
post-publication read-back; candidate/security gates and final MCP checks passed.

See the [complete rc.3 report](<../../../verification/release-rc3.md>) for exact
image/chart identities, actual commands, retained rc.2 index/archive proof,
attestation limits and three disclosed nonblocking MEDIUM native advisories.
No GPU report was added; full P07/P08 and current Electron compatibility remain
incomplete. No stable/latest advancement or cluster deployment followed.

The release pipeline has finished; no release watch or background job is needed.
The local monitor correction now wakes on protected-environment `waiting`; all
24 watcher tests, Ruff and mypy passed. Its three source/test/guide files and these
completion records are submitted separately from preserved concurrent user
cache/action/CHANGELOG/release-doc changes. Do not silently include those unrelated
changes or move the rc.3 tag. R01 itself started no next packet; P07 is now separately
selected by the subsequent user directive above.

## Previous P07 local verification checkpoint (historical)

Packet: **P07 — local server defect verification complete; full candidate acceptance incomplete (2026-10-26, Europe/Moscow)**.
The user explicitly selected P07 after loading the matching
`incremental-implementation` skill. Manual integrity and Git worktree checks
passed; pinned Approved/reviewed specification passed and the exact P07 agreement
`{id: be6db71a-6ebf-4f95-bca4-b81f2da2171a, revision: 4}` remained ready with digest
`a91beca407917e6c1206f99d93223f0d72452634c9da1fe53f0efeb0a418b31e`.
The user subsequently authorized committing/pushing the prepared corrections,
opening a PR and starting CI, with event-driven waiting and correction. The current
branch is `fix/resident-recovery-rc3`; merge/publication follow successful checks
and the owning immutable-candidate gates. No stable/latest or cluster operation
is authorized. Existing R01 evidence is preserved.

## Current user directive — fix and verify locally before release

The user explicitly requested finishing defect correction and local verification
first, before publishing any corrected release. Local image builds and local GPU
checks are authorized for that purpose. This does not qualify rc.2 or authorize
R01, commits/pushes, registry writes, tags, P08 or stable. Preserve all previous
R01 evidence. The published-candidate-only completion gate still applies to the
future final P07 report; local evidence is separately labeled, not substituted.

Current local runtime identity:
`sha256:2cbb5d9db5451a3cbba00d76af845f35e4e86454bfe113eca10bda45c99eddbd`,
base HEAD `364f1e49e080b317f403684ce5d80e3676309b1c` plus recorded uncommitted
changes. Canonical cached pair-build inputs matched before/after. Actual identities
are in the [local receipt](../../../../.local/p07/local-fixed-Wd6DfmOw/receipt.json)
and [safe local report](../../../verification/docker-local-pre-release.md).
Earlier `f420…`/`5ad…` results are historical, not evidence for later runtime bytes.

- Stale owned-Busy completion and private-native TCP retirement/crash recovery
  are corrected. Private `SetLinger(0)` plus close-idle-before-stop preserve full
  response validation, no uncertain POST replay, fixed ports, app caps, exclusive
  foreign-socket guards, native source and process-group stop/reap ownership.
- On these final images, the last resumed lifecycle **8/8 passed** in 45.14s:
  native SIGKILL recovery 4.89s; reduced 3s operation/10s cleanup delivered 408
  `operation_timeout` without transport loss in 3.521s, then fresh inference.
  Active API drain/forced death, queue expiry/FIFO/cancel, bounds/no-GPU and all
  cache/model recipes including deterministic unsupported-ftype failure passed.
- Baseline sixty and independently measured sixty requests both passed. Native-PID
  CUDA SM/framebuffer activity, separate calibration/repeat RSS/FD/file snapshots,
  actual queue/private HTTP correlation and zero recurring model-path operations
  were observed. API FD counts were 12 after all three blocks. Serial snapshots,
  trace overhead and sampling gaps are explicit; these are not latency SLOs.
- **Local server checks are complete; the mixed command is not an aggregate pass.**
  The [final resumed run](<../../../../.local/p07/local-fixed-Wd6DfmOw/local-final-resumed.log>)
  completed in 144.277s with exit 1 solely on Electron sandbox startup before HTTP.
  All 51 public cases, both genuine SDK representations and every server GPU,
  queue, model/cache, resource and lifecycle group passed. The user clarified
  server HTTP validation as the current fix scope; current OpenWhispr compatibility
  remains unverified for full P07, without blocking the server report/new unqualified RC.
  Shared whole-word JFK checks and explicit public-bind modes under `umask 077`
  are fixed and exercised; private evidence stays 0700/0600. Temperature 1 remains
  protocol-only, not lexical-quality qualification. Earlier lexical/fixture failures
  and the interrupted recording remain retained as failed/incomplete, not passed.
  Native fallback is possible, not a proven cause of the lost original responses.
- Full CPU race/vet, Ruff format/lint, mypy (38 sources), actionlint and SDK12
  tests passed. Python tooling117 passed with 12 disclosed C02/Helm fixture skips;
  skips are not GPU/security/publication qualification.

Private evidence is under `.local/p07/local-fixed-Wd6DfmOw/`; the first full run
is [retained as failed](../../../../.local/p07/local-fixed-Wd6DfmOw/all-local-gpu.log).
HTTPS source-test trust is process-local through the read-only prepared mount,
not a host trust change. No cache volume is deleted by ordinary cleanup.
Full P07 absence/leak-sensitivity calibration, configured idle/retention and
failure-envelope coverage, unexecuted feasible direct-native comparison and new
immutable-candidate/channel qualification remain separate outstanding scope.
P07 stays unchecked; rc.2 remains unchanged/GPU-unqualified. The local server
report is now complete. The parent next owns the separately authorized
commit/PR/CI/new-RC sequence. The current action is to launch PR CI, bind its
exact run/head/attempt to the prepared [pipeline monitor](<../../../verification/pipeline-monitor.md>),
and return control until a terminal event. Its test wake delivery was confirmed
on a historical run only. Record actual new run identities in the private live
selection; do not confuse preparation with a running pipeline or publication.

## Previous published-candidate P07 checkpoint (2026-10-25)

- Shared helpers now exist under `tests/acceptance/`: public HTTP multipart
  workload with bounded responses/cancellation/privacy, real sequential
  measurement collection, independent optional observation statistics, and a
  bounded canonical Compose target adapter. They do not certify GPU acceptance.
- Final non-GPU Go suites (`cmd`, `internal`, integration and acceptance helpers)
  passed with `-race`; `go vet ./...` and pinned gofmt passed. The transcription
  package additionally passed race ten times. Semantic RED and isolated PROVE
  evidence is under `.local/p07/`: HTTP/workload 12 mutations, statistics 4,
  Docker 6, readiness correction 6, and SDK bounded/privacy/retry/deadline checks.
  Original sources were restored after every mutation. A separate fake-Docker
  cancellation test was corrected to wait for an actual child-start handshake
  before cancellation; default-GORACE regression and CLI-only-kill PROVE passed.
- Initial restricted-session `nvidia-smi` exited 9. After the user's runtime
  policy change, host and exact backend-container GPU enumeration succeeded:
  RTX 5070 Ti Laptop, driver 595.99.02, 12227 MiB. No driver/global setting changed.
- Real rc.2 canonical Compose execution reached readiness and transcribed the
  pinned eleven-second public JFK fixture. The subsequent sixty-request
  steady workload **failed: 11 successes, 49 HTTP 503 not_ready** in the diagnostic
  run. Backend health remained HTTP 200 and submitted private inference succeeded.
  No sleeps/retries were added to hide the failure. Containers/networks were
  removed through owned cleanup; model-cache volumes were retained.
- Confirmed source defect: an owned cached BackendBusy health snapshot survives
  successful completion after the worker clears its active ticket; a late health
  reply can repeat it. Local `service.go` / `lifecycle.go` now serialize health
  observation commits and perform a bounded confirmed refresh before retiring the
  successful owner. Cached probes remain nonblocking; foreign Busy, missing CUDA,
  dependency/configuration/generation failure and operation deadlines stay guarded.
  Added post-success health I/O can affect latency up to its configured budget;
  real measurements must be renewed. The published rc.2 does not contain the fix.
  Changed runtime bytes require separately invoked R01/new RC; never overwrite rc.2.
- Genuine OpenWhispr 1.10.4 / pinned commit 7a6ae826de9ec1caf65f3a6e769475dab2ae829f
  upload transport has been prepared with actual Electron 41.10.7 net.fetch,
  without Node-fetch replacement or full-UI claims. Preparation passed, but no
  actual service call has passed yet. The official OpenAI 2.11.0 dev dependency,
  safe bounded SDK probe and 12 durable fault-server unittest cases are ready.
  Both actual client integrations remain unverified until a corrected candidate.
- Locked Python formatting/lint/types passed (38 sources). Tooling unittest:
  117 tests, 12 explicitly skipped environment-dependent C02/Helm fixtures;
  the pinned real secret-scanner fixture passed. SDK unittest: 12 tests, no skips.
  The skips do not qualify security/publication or GPU behavior. Existing CI and
  contributor commands now include acceptance helper/SDK validation without GPU
  execution, new triggers or publication permissions. Pinned actionlint 1.7.12
  with existing ShellCheck 0.9.0 passed all three workflows; optional Pyflakes was
  unavailable, while standalone Python was checked with Ruff/mypy.
- Sanitized current results: [rc.2 Docker checkpoint](../../../verification/docker-rtx5070ti-rc2.md).
  Raw owned-service logs, exact commands and fixture checksums remain under
  `.local/p07/`. No complete GPU/resource/client/lifecycle report exists.

The local correction and currently available checks are ready for handoff; no
commit/push or image publication was performed. Final MCP checks returned pinned
Approved/reviewed specification `pass` and the exact P07 agreement `ready`, with
no issues. Confirmed scope and public/port contracts are unchanged; new helper
boundaries remain within shared acceptance ownership, and the readiness fix is a
narrow correction in the existing runtime unit, not a new architecture.

The implementation agent owns the remaining harness and actual checks. A
separately authorized R01/new-RC step is required before changed runtime inputs
can receive renewed candidate acceptance. On the last read-only check,
`v0.1.0-rc.3` and its five versioned image/chart manifests were absent; recheck
before selecting/writing that version. P07 remains unchecked. Stop at this
checkpoint pending the user's next packet decision; do not start R01/P08 or
count helper tests/enumeration as a GPU pass.

## Preserved R01 completion

Packet: **R01 — complete (2026-10-25, Europe/Moscow)**.
Candidate: **v0.1.0-rc.2 — candidate_published_unqualified**.
This historical completion authorized no later packet; the later explicit user
invocation above selects only P07.
Baseline I01/I02/I03/C01/F01/P01–P06 and C02 are complete.
The user authorized the current corrections, commits/pushes, PR/merge after
successful CI and pre-release publication. In this continuation the user
explicitly selected `v0.1.0-rc.2`; the existing `v0.1.0-rc.1` tag is preserved.
GPU/cluster execution, stable/latest and subsequent packets remain separate.

## Completed R01 publication

- [PR #9](https://github.com/swimmwatch/openwhispr-whisper-server/pull/9)
  merged as `3faa5cffa82917e590994cf1d2edcd2256d74c33` after all seven jobs in
  [CI 40207794716](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40207794716)
  passed on head `364f1e49e080b317f403684ce5d80e3676309b1c`, including actual
  runtime/license/Compose checks, security scans and the final Security Check.
- The earlier [rc.1 run 40199668493](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40199668493)
  failed during inventory qualification, before registry authentication/writes.
  PR #9 fixes `qualify()` to take `build_input_identity` from the selected release
  inventory, not the source security report. The updated behavior test passed CI.
- Immutable tag `v0.1.0-rc.1` still resolves to
  `9641cad40d0e09795242d573b408cf3934447c56`. New tag `v0.1.0-rc.2` resolves to
  `3faa5cffa82917e590994cf1d2edcd2256d74c33`.
- [Release 40212576458](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40212576458)
  succeeded on source `3faa5cffa82917e590994cf1d2edcd2256d74c33`: cold API/backend
  builds (6m2s / 48m39s), actual runtime/license/Compose checks (6m17s), fresh
  complete security qualification, GHCR/Docker Hub publication, OCI/Pages chart
  publication and final GitHub assets. Stable-only `aliases` was correctly skipped.
- [GitHub pre-release v0.1.0-rc.2](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.2)
  is public, not draft and not latest. Its chart, `channel-state.json` inventory
  and `SHA256SUMS` were downloaded anonymously; native checksum checks passed.
  The inventory gate returned `candidate_published_unqualified`.
- Both Linux amd64 images were pulled by exact digest from both registries with
  an empty task-local Docker configuration; source labels match the tag.
  Anonymous manifest/config/attestation reads retained actual SPDX SBOM and SLSA v1
  provenance in all four repositories. RC and full-source tags match the inventory.
- Native Helm 4.3.0 pulled the exact chart from OCI and the classic Pages endpoint
  with isolated empty client settings. Both archives are byte-identical to the
  GitHub asset. Rendering with GHCR and Docker Hub digest overrides succeeded;
  rendered outputs differ only in the two image repository references; after
  normalizing those references, the outputs are byte-identical.
- Complete release security evidence passed all 22 targets with no blocking
  findings or coverage gaps. Existing narrow exceptions remain explicit; three
  applicable native MEDIUM findings remain visible, not waived as clean.
- Published identities (OCI index digests include attestations):

  | Artifact | Identity |
  | --- | --- |
  | API, both registries | `sha256:d32e6678d87315ae99e8d68c35818c0aff45d05c82cf1ccb7905fcbaf2731391` |
  | Backend, both registries | `sha256:803f0d7ac1459681bb5f1a580d34e0abfd584d26d7195aa2ab1f14317f9f0b15` |
  | Chart archive, OCI/Pages/GitHub | `32176f3f6f4156afcdabc2919f6203bae5f528ffb4fa149d2d25930b7bc6cbb1` |
  | Chart OCI manifest | `sha256:06f8cdf014015aeaa7d77611561397b01114f4fa2eb7a03e144588fb0317dd9d` |

  Build identity: `sha256:23a4c1472d2627186dde1b46591523f9c3e763e7743074b6c8c1f891ee39a3be`.
  Runtime identity: `sha256:8c10149c969606e9d49207bfb210670e0dad335f16e1c4b75a26518bfbc51a08`.
  Safe read-back evidence is retained under `.local/r01/rc2-readback/`.
- Historical preflight: GitHub Releases and both public Docker Hub tag lists were
  empty; GHCR anonymous tokens returned 403 and Pages index returned 404. The local
  GitHub token could not enumerate packages without `read:packages`. These were
  not treated as proof of private-package absence: authenticated collision checks
  ran in the publisher. Actual anonymous read-back now passes every channel.
  The prior `gh-pages` index had no entries; its history is preserved and the
  current index advertises rc.2 with the expected checksum. There was no older
  published archive to pull; prior-entry preservation is also covered by CI fixtures.
- `release` and `release-pages` require the owner as reviewer. Both Docker Hub
  secret names are present in `release`; their presence alone does not prove
  successful authentication. Main's active ruleset requires strict up-to-date
  Security Check and a PR (the legacy branch-protection endpoint returns 404).
- MCP preflight: pinned Approved/reviewed specification passed; the exact R01
  agreement remained ready with no issues. Requirement continuations were read.

R01 has no outstanding required check. Final MCP freshness returned Approved /
reviewed / pass, and the exact R01 agreement remained ready with no issues.
No tag was moved and no stable/latest alias, GPU workload or Home Lab deployment
was performed. Subsequent packets require a separate explicit user invocation.

## Verified workflow inputs

- source_version: `2c17104536fbf2438169efc6f53d592173ec8d90e326999bee3aa6701c38d3b9`
- specification_version: `6f76eff8ed1b97caa18bcbbaf6da09961f4b4ffafe5736c4a2688106f843d939`
- review_ref: `a2b606b5-12d9-4c9e-a805-86f889709220`
- Proposal: `c02-security-release-ci-v2`.
- agreement_ref: `{id: 0707c637-98d3-4781-bc32-f86d5ba105b6, revision: 2}`.
- proposal_digest: `582e82cbffbf7364569716554a5cc603dc22c3a2dad1fc98db89b5a3e423b6be`.
- Matching manual/Git preflight passed. Specification is Approved/reviewed.
  The frozen technical proposal is preserved. Final checks returned specification
  `pass` and exact v2 agreement `ready`, with no issues.

## Current release workflow refactor

The later user-approved tag-driven flow supersedes the former main-CI artifact
selection described in the historical C02 record. CI runs only on pull requests.
Release starts on `release: published` or manual `version_tag`: RC builds cold
images and a versioned chart; stable preserves accepted RC image digests and
checks the new chart for runtime equivalence. Both paths require successful
merged-PR CI and fresh security qualification before publication. Stable still
requires reviewed acceptance metadata and actual P07/P08 reports.

Build and runtime verification are shared with PR CI. Operator instructions and
current inputs are in [release operations](../../../releases.md#tag-driven-release-workflow).
The completed R01 runs and channel evidence above now qualify this refactor for
GPU-unqualified candidate publication. Historical C02 results below remain useful
background, not substitutes for the exact rc.2 evidence. No hardware acceptance
was performed.

## Historical local implementation and validation

Trusted CI/security verification, immutable image/chart publication, index
preservation, explicit Pages deployment and selected-image stable promotion are
wired. Stable uses accepted image manifests and the current stable chart;
actual five-profile Helm equivalence denies runtime changes.

Alias publication snapshots previous identities before writes, performs exact
read-backs and restores existing aliases on failure. An immutable pre-write
artifact survives runner loss; same-run retries restore it by ID. Conservative
rollback includes remote writes lost before progress upload. New aliases without
a previous value remain explicit partial state; immutable manifests are never
deleted to conceal failure. Changed retry inputs and arbitrary conditional CI
bypasses are rejected. See [release operations](../../../releases.md).

Current backend GnuPG/OpenSSL package findings are remediated. Native review is
bound to actual build inputs and reverified primary-source hashes. All 22 targets
pass with no blocking findings or coverage gaps. Three applicable MEDIUM findings
remain visible: FFmpeg CVE-2026-107676/CVE-2026-107678 and whisper.cpp
CVE-2026-17513. Two exact Echo Proxy exceptions are supported by both binaries'
linked-symbol tests plus a positive Proxy fixture. Echo remains v5.4.0.

## Actual current local evidence

Pinned Go 1.27.3 and private Python/tool installations were used.

- Tooling: 100 tests, no skips, including actual scanner and disposable publisher
  fixtures (`.local/workflow-optimization/tooling.log`).
- Source: `go test -race -coverprofile=... ./cmd/... ./internal/...` passed.
  This invocation's command/internal statement coverage is 90.9%; it is not a
  task-wide or GPU coverage claim (`go-final.log`, `coverage-final.log`).
- Images/Compose/Helm: `go test -race ./tests/integration -count=1 -timeout=15m -v`
  with P05/P06 flags and the current images passed in 372.625s
  (`runtime-final.log`). The earlier unsupported FFmpeg `-parsers` harness
  failure was corrected with an actual libavcodec parser registry probe.
  The current profile is GREEN; the old API image is semantic RED.
- Format/vet/module verification, architecture, contracts, agent setup,
  actionlint, Hadolint, current-source secrets and the complete security CLI pass.
- Seven isolated in-memory guard mutations were detected without harness errors
  or source mutation (`prove-remaining.log`). The missing pre-write workflow
  snapshot was semantic RED and is now GREEN.
- Post-edit contracts/agents/actionlint/current-source secrets and the complete
  security CLI pass. Scoped tracked/untracked whitespace checks pass.
- Private logs/scripts are in `.local/c02/resume/` (mode 0700).
  They are excluded from source/image/security artifacts.

Historical local artifacts, superseded by the trusted main build below, are in
`.local/c02/current-artifacts/`:

- API: `sha256:f499a34b2ee4f5c5a33ffe181d335d9f8157f78d0ce1f2bfb0f655442bbbbb24`.
- Backend: `sha256:797f5db62d423cab41169da42b885232994a4d8bc60f602281188dd7418670b2`.
- Chart archive: `2bbeab744544aa60f5cdf808920b7a65917154db223fd4b30cbaa7b587129ec0`.
- Both images retain actual SBOM/provenance and verified OCI identities.
  They are cached local builds (`cold=false`) from a dirty checkout. Their
  source labels do not prove those bytes exist in the labeled Git commit.
  Earlier cold exports remain historical, not current clean qualification.

## Workflow optimization — 2026-10-24

Both images now use a two-role matrix, preserving actual qualification names.
PR layers, Go/pip dependencies and SHA256-verified tool downloads are cached;
main image builds remain cold. Trivy caches only its vulnerability database,
without disabling refresh. Scans and runtime checks run independently before the
final always-run aggregate. Go/tool/BuildKit concurrency follows `nproc`; existing
native memory/quota limits remain authoritative.

Latest local evidence is `.local/workflow-optimization/`: 100 tooling tests,
four additional detected in-memory bypasses, actionlint/contracts/agents and
the actual 22-target Security Check with persistent DB input pass. A real
BuildKit fixture imported its cached COPY layer and retained OCI attestations.
Five tool/schema installations measured 6.383s cold versus 0.515s warm locally.
These are not full GitHub/CUDA build timings. Native runtime/image inputs are
unchanged, so the earlier actual image/Compose/Helm run remains applicable.

## Trusted main qualification — 2026-10-25

[CI run 40138859398](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40138859398) passed all seven required jobs on committed
source `76279e618a2692551bbdad48d2dd77461ad4b530`. Both Linux amd64 images were built
without GPU injection, with fresh isolated builders, `--no-cache` and no imported
PR layer caches. Export checks retained and verified SBOM/provenance attestations.

Runtime, component, license and Compose checks passed in 204.491s. All ten
resource-budget cases passed; the first cold backend CPU probe took 66.12s.
The former 60s deadline had failed twice in main run 40124786409. PR #7 increased
only this bounded probe budget to 180s; CPU, affinity, memory and override
assertions remain unchanged. The source job also passed production Go race
checks; packaging artifact checks ran without race instrumentation.

Security Check passed all 22 required targets against these same image indexes,
chart archive and source revision. Main protection requires this aggregate check
with strict up-to-date checks; unrelated protection settings were preserved.

| Artifact | Qualified identity |
| --- | --- |
| API OCI index | `sha256:40b6ce3d7e9b2d4cb97608798358f0373c52cd807f6ad443c676a447f4b34f75` |
| Backend OCI index | `sha256:5dd326f3e5ff3abcd1cd20a96eb4a9aee5a8a7c19b2d89ebc8b1b174e0d132bc` |
| Chart archive SHA256 | `42b8f493ec7d707f6551cd5752b4792e431919b7be304bf719e8f7227d47ce57` |

The inventory field `image_manifest_digests` binds the OCI index identities above,
including their executable manifests and attestations.
The exported Linux amd64 manifests and attestation identities were also checked.
Private evidence is under `.local/c02/ci-followup/main-76279e6/`; the GitHub run
contains the corresponding public job results and artifacts. Local cached/dirty
exports in earlier checkpoints remain historical and are not the selected
qualification evidence. Release publication and GPU/cluster acceptance remain
outside this packet. Final pinned specification check passed and the exact C02 agreement remained
ready after these bookkeeping changes; the frozen technical proposal is unchanged.

## Required remaining work and ownership

R01 publication and mandatory read-back are complete. The protected publisher
successfully used the configured credentials; no credential action is outstanding.
The candidate remains GPU-unqualified. Do not start P07/P08/P09/R02 automatically;
the user owns any later separate packet invocation. Preserve rc.1/rc.2 tags and
all published immutable artifacts.
