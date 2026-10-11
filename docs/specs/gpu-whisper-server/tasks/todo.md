# Resident V1 completion checklist

Status: I01, I02 and I03 complete. C01 local implementation/checks passed; mandatory hosted CI and required-check settings are pending hosted execution; commit/push and test PR are authorized, so C01 remains unchecked. External plugin copying and other-client checks were cancelled by the user's explicit scope change. Runtime implementation not started. This is the sole packet completion checklist. Planned or unavailable checks for the remaining packets are not passes.

- [x] [I01 — local project initialization](init_01_local_project.md)
- [x] [I02 — public GitHub and open-source foundation](init_02_github_open_source.md)
- [x] [I03 — Codex project tooling](init_03_agent_tooling.md)
- [ ] [C01 — early contributor CI](ci_01_bootstrap_checks.md)
- [ ] [F01 — Go settings, logging, middleware and architecture](foundation_01_go_service.md)
- [ ] [P01 — configuration and models](01_configuration_models.md)
- [ ] [P02 — backend supervisor](02_backend_supervisor.md)
- [ ] [P03 — HTTP/audio adapter](03_http_audio_adapter.md)
- [ ] [P04 — queue, health and lifecycle](04_queue_health_lifecycle.md)
- [ ] [P05 — images and standalone composition](05_images_standalone.md)
- [ ] [P06 — Helm chart](06_helm_chart.md)
- [ ] [C02 — complete build and release pipeline](ci_02_build_release_pipeline.md)
- [ ] [R01 — pre-release publication and anonymous pull-back](release_01_prerelease.md)
- [ ] [P07 — Docker / RTX 5070 Ti Laptop acceptance](07_docker_gpu_acceptance.md)
- [ ] [P08 — Home Lab / RTX 2060 acceptance](08_homelab_gpu_acceptance.md)
- [ ] [P09 — final README and durable documentation](09_documentation.md)
- [ ] [R02 — qualified stable release](release_02_stable.md)

Before each explicit invocation, verify the confirmed scope, fresh Approved specification/review and exact input pins. I01 is the separately invoked local bootstrap before Git-dependent incremental execution. Every later packet requires a usable Git worktree and passing prerequisites. Read exact requirements via MCP, recheck source freshness before work/completion, and stop after the invoked packet.

R01 completion means a published but GPU-unqualified candidate. It does not mark P07/P08 or R02 complete. Changed candidate bytes require a new immutable RC and renewed affected evidence; reopen the affected checkboxes until their checks pass. Home Lab deployment remains manual.

