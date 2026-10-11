# Current handoff

Stage: **C01 local implementation verified; required hosted acceptance pending
(2026-10-11)**. I01/I02/I03 remain complete. C01 remains unchecked.

## Authorization and required next step

The user invoked incremental-implementation/context-scouting after I03 and its
documentation refinement. This invocation covers C01 only. The packet's manual
gate reserves commits and test-branch pushes for explicit authorization. The user explicitly authorized the initial public source commit/push and test PR
with `ok`. No remote mutation has yet been performed.

The verified public repository is swimmwatch/openwhispr-whisper-server, with
Actions enabled and usable scoped administration rights. Its main branch API
returned 404 before initial source publication. Existing active main protection
(id 24875239) retains deletion/non-fast-forward protection and a PR requirement.
No settings were weakened; no required check name was added before a real pass.

Next action: with the user's explicit authorization, the C01 implementer publishes only reviewed public
files, runs trusted and fork-safe hosted CI plus a deliberate failed-check case,
records exact SHAs/run URLs/conclusions, then adds and reads back the successfully
observed required check. Preserve existing protections; address any actual push
restriction through scoped authorization instead of silently disabling it.
The implementer owns those authorized CI actions.

## Changes and actual check commands

- .github/workflows/ci.yml: pull_request and main push, hosted ubuntu-24.04,
  bounded Bootstrap checks job, job-level contents read only, checkout without
  persisted credentials, no caches/secrets/publishing. Third-party actions use
  reviewed full SHAs. Downloads verify published archive SHA256 before extracting
  only the requested executable.
- tools/check_contracts.py: strict YAML with duplicate/anchor/alias/tag rejection
  and correct on-key parsing, requirement/scenario/packet/environment links,
  checksum-pinned official OpenAPI schema validation and internal references,
  effective workflow permissions/triggers/actions/failure policy, required command
  wiring, secret-rule configuration, current settings/documentation/exclusions.
  It never parses MCP-owned workflow state or substitutes approval verification.
- tools/security/check_secrets.py and trivy-secret.yaml: pinned Trivy secret-only
  scan including Markdown, explicit private/Git/MCP exclusions, captured raw
  output and private temporary reports. Only counts/tool/rule/time/source metadata
  are emitted; matches, paths, titles and code are not public output. Findings
  block even with exit 0; unsupported/missing/error reports fail closed.
- tests/tooling/test_contracts.py and test_secrets.py: 16 tests, including real
  pinned-scanner marker detection, exclusions and safe report/exit handling.
- tools/requirements-ci.txt: seven exact Linux amd64/CPython 3.12 wheel versions
  with verified PyPI hashes; tools/versions.yaml records current CI/action/schema/
  scanner identities and distinguishes initial metadata from actual C01 coverage.
- tools/schemas/openapi-3.0.json: unchanged official 2024-10-18 schema for OpenAPI
  3.0, SHA256 2385f5bbb8c37878daae73baeabe7f34b2f022a4a8c049329ee61f71796f039c.
  Upstream Apache-2.0 license and source attribution retained beside it.
- Dependabot additionally covers GitHub Actions and the permanent tooling lock.
  C01/plan/checklist/handoff record local readiness and the pending hosted gates.

From the project root, with installed hash-locked Python check dependencies and
verified actionlint/Trivy executables:

```sh
rtk proxy python3 tools/check_contracts.py
rtk proxy python3 tools/check_agents.py
rtk proxy python3 -m unittest discover -s tests/tooling -v
rtk proxy actionlint .github/workflows/ci.yml
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod download
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod verify
rtk proxy python3 tools/security/check_secrets.py
```

Actual local validation used a fresh .local/c01/venv, the permanent hash lock and
checksum-verified .local/c01/bin tools. These local installation paths are not CI
prerequisites; hosted CI installs from the tracked lock/identity manifest into its
own environment. C01_TRIVY_BINARY supplies a verified executable to fixture tests
when it is not on PATH.

## Verification evidence and limits

- Matching engineering-manual verifier: exit 0; separate Git-worktree check:
  true; staged diff empty. Existing and new public files remain untracked.
- Fresh Approved/reviewed specification check passed before work and at checkpoint.
- Tests were written first. Initial RED was a missing-module/import failure,
  not semantic proof. GREEN followed implementation and two observed corrections:
  a YAML plain scalar containing a colon was changed to a block scalar, and the
  scanner parser was corrected for Trivy's real empty-filesystem report that omits
  Results. A permission detector's false match on check_secrets.py was also fixed;
  actual secret-context access still fails.
- PROVE: six independent in-process mutations produced intended assertion
  failures for ignored job permissions, unknown requirement refs, duplicate YAML,
  exit-zero findings, public raw matches and private-directory scanning. Mutations
  were restored; real production files were never broken.
- Fresh Python environment installed the permanent --require-hashes lock; pip
  check passed. All 16 tooling tests passed, including two actual Trivy scans.
- Public contract/bootstrap checks, agent checks and actionlint syntax: exit 0.
  Local ShellCheck is unavailable; no ShellCheck pass is claimed.
- Go module download/verify: exit 0; no external Go dependencies/packages exist.
  No placeholder application-test pass or full Security Check is claimed.
- Real current-source Trivy 0.75.0 secret scan: exit 0, zero findings. Executable
  SHA256 93f9da8e4ba5e0c1c76d8234ed2494cf9afb0a96fd21953e424bb795f3299b8e;
  secret-config SHA256 1ca45873ff134fddd96ff540386f280777fb429b2c328349d9614bb3a8e91c82.
  Evidence is local-uncommitted, not a qualified remote source SHA.
- actionlint 1.7.12 and Trivy 0.75.0 archives matched publisher checksum files;
  actual tool versions were exercised. Three v7 action commits were checked against
  upstream refs/source. Hosted action/runner compatibility remains unverified.
- Required trusted/fork hosted runs, deliberate failed required job and required
  check read-back are not performed: hosted execution is pending; commit/push authority is granted. There are
  no run URLs/source SHAs to report. These are incomplete requirements, not passes.

## Reviewed specification inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

No specification behavior or MCP-owned source was changed. Requested Luna scout
research was read-only; the implementer validated sources and performed all edits
and checks. Effective inference identity and savings were not independently
measured. Private Home Lab context remains under .local/homelab; C01 does not use
cluster credentials or perform hardware operations.

No later packet, service code, images/chart, deployment or artifact release was
executed. F01 is next only after C01's required hosted/settings evidence passes
and a separate explicit invocation.

Next action/owner: the C01 implementer publishes the initial source and completes
hosted acceptance and required-check read-back under the user's explicit approval.
