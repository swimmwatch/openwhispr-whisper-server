# Resident V1 completion checklist

Status: I01, I02, I03, C01, F01, P01–P06 and C02 complete. P05's CPU-quota correction passed all ten BuildKit resource cases, both image rebuilds, the full race/image/standalone suite and final freshness checks. P06 passed 30 local Helm/Kubernetes API-schema cases, configuration/boundary/package checks, full Go tests/race/vet and fifteen PROVE mutations (2026-10-23). Hosted CI/security qualification and R01 pre-release publication are complete. Cluster/GPU/CNI acceptance, stable publication and final documentation remain unexecuted. C01 hosted evidence stays historical. This is the sole packet completion checklist. Planned or unavailable checks for remaining packets are not passes.

## Current hosted qualification — 2026-10-25

C02 is complete. [Trusted main CI 40138859398](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40138859398) passed all seven required
jobs on source `76279e618a2692551bbdad48d2dd77461ad4b530`, including both cold image builds,
runtime/license/Compose checks and all 22 security targets. The earlier probe
timeout is fixed and validated in main. The historical checkpoints below are
superseded by this result. R01 subsequently published the GPU-unqualified rc.2
candidate; no GPU/cluster acceptance or stable release has been performed.

## R01 completion — 2026-10-25

[PR #9](https://github.com/swimmwatch/openwhispr-whisper-server/pull/9) merged only
after all seven jobs in [CI 40207794716](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40207794716)
passed. [Release 40212576458](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40212576458)
then passed cold builds, runtime/license/Compose checks, fresh complete security
qualification and all publication/read-back jobs for
[v0.1.0-rc.2](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.2).
Independent anonymous pulls checked both exact images in GHCR/Docker Hub,
SBOM/provenance, identical OCI/Pages/GitHub chart bytes, both chart image-registry
overrides and GitHub pre-release/not-latest metadata. Final inventory gate:
`candidate_published_unqualified`; pinned specification passed and R01 agreement
remained ready. The rc.1 tag is unchanged. See [handoff](handoff.md) for identities.
Only R01 is newly checked; subsequent packets are not started.

## Historical checkpoints

P06's [review corrections](06_helm_chart.md#review-corrections--2026-10-23)
also passed both new regressions, isolated PROVE checks, all eight chart tests,
full Go tests with `-race`, vet and structural checks (2026-10-23).

C02 remains unchecked. Local qualification now passes 100 tooling tests without
skips, actual scanner/registry/index fixtures, accepted-image promotion and
runner-loss alias recovery. All 22 security targets have zero blocking findings
and no coverage gaps; three applicable native MEDIUM findings remain reported.
The current image/Compose/Helm race matrix also passes (372.625s). Current builds
are cached exports from a dirty checkout; earlier cold exports are historical.
Actual trusted remote CI on committed current inputs, including clean cold-build
qualification, remains required. No commit/push/publication is authorized here.
See the [latest C02 checkpoint](ci_02_build_release_pipeline.md#current-local-checkpoint--2026-10-24)
and [current handoff](handoff.md) for identities, checks and remaining ownership.

P05's [configured-port correction](05_images_standalone.md#configured-port-correction--2026-10-23)
also passed render, real container publication/network, focused race and full Go
checks (2026-10-23). Its later [pipeline correction](05_images_standalone.md#build-pipeline-correction--2026-10-23)
is followed by the [build optimization report](05_images_standalone.md#build-optimization-and-comparison--2026-10-23)
with paired timings and passing image/race checks. The latest
[CPU-quota correction](05_images_standalone.md#cpu-quota-correction--2026-10-23)
records current rebuilt identities and passing resource/image/race checks;
older identities remain historical.

P01's exact technical agreement remained ready and the pinned specification
remained Approved/reviewed at completion. Its frozen proposal is unchanged.
The whole remaining plan was technically agreed through four synchronous user
answers (2026-10-22). P02-P09, C02, R01 and R02 each have an exact managed
agreement and planning_packet_check=ready; the earlier P02 ANSWER_REQUIRED blocker
is resolved. P02–P06 retain their implementation evidence, including completed
P05 revalidation. The remaining delivery packets stay unchecked because planning
readiness is separate from acceptance. P05 CPU-quota correction is complete. A separately
invoked C02 resume needs fresh preflight and its remaining required checks.
R01 is not yet eligible.

## Delivery checklist

- [x] [I01 — local project initialization](init_01_local_project.md)
- [x] [I02 — public GitHub and open-source foundation](init_02_github_open_source.md)
- [x] [I03 — Codex project tooling](init_03_agent_tooling.md)
- [x] [C01 — early contributor CI](ci_01_bootstrap_checks.md)
- [x] [F01 — Go settings, logging, middleware and architecture](foundation_01_go_service.md)
- [x] [P01 — configuration and models](01_configuration_models.md)
- [x] [P02 — backend supervisor](02_backend_supervisor.md)
- [x] [P03 — HTTP/audio adapter](03_http_audio_adapter.md)
- [x] [P04 — queue, health and lifecycle](04_queue_health_lifecycle.md)
- [x] [P05 — images and standalone composition](05_images_standalone.md)
- [x] [P06 — Helm chart](06_helm_chart.md)
- [x] [C02 — complete build and release pipeline](ci_02_build_release_pipeline.md)
- [x] [R01 — pre-release publication and anonymous pull-back](release_01_prerelease.md)
- [ ] [P07 — Docker / RTX 5070 Ti Laptop acceptance](07_docker_gpu_acceptance.md)
- [ ] [P08 — Home Lab / RTX 2060 acceptance](08_homelab_gpu_acceptance.md)
- [ ] [P09 — final README and durable documentation](09_documentation.md)
- [ ] [R02 — qualified stable release](release_02_stable.md)

Before each explicit invocation, verify the confirmed scope, fresh Approved specification/review and exact input pins. I01 is the separately invoked local bootstrap before Git-dependent incremental execution. Every later packet requires a usable Git worktree and passing prerequisites. Read exact requirements via MCP, recheck source freshness before work/completion, and stop after the invoked packet.

R01 completion means a published but GPU-unqualified candidate. It does not mark P07/P08 or R02 complete. Changed candidate bytes require a new immutable RC and renewed affected evidence; reopen the affected checkboxes until their checks pass. Home Lab deployment remains manual.

