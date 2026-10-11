# Current handoff

Stage: **C01 complete (2026-10-11)**. I01/I02/I03 remain complete.
F01 is next and requires a separate explicit invocation.

## Authorization and published source

The user invoked incremental-implementation/context-scouting for C01 and then
explicitly authorized initial public I01-I03/C01 commit/push and a test PR with
`ok`. Only reviewed public files were committed; private/generated data stayed
ignored. No PR merge, artifact release, deployment or later packet was performed.

Initial main commit: `33cefaf56cdf271b0ba6cdccd4c3099a8b95ee0c`.
The initial main push succeeded without weakening existing rules.
[Test and evidence PR](https://github.com/swimmwatch/openwhispr-whisper-server/pull/1)
uses `feature/c01-hosted-verification`; its final workflow matches main.

## Implemented boundaries

- `.github/workflows/ci.yml`: hosted main/pull_request checks, pinned actions/tools,
  bounded Bootstrap checks job, contents read only, no persisted checkout token,
  caches, secrets, uploads or publication rights.
- `tools/check_contracts.py`: strict YAML, requirement/scenario/packet references,
  pinned official OpenAPI schema, effective job permissions, command wiring,
  links/settings/exclusions. MCP-owned sources are never locally parsed.
- `tools/security/check_secrets.py`: verified Trivy 0.75.0, source-secret scanning
  including Markdown, private/Git/MCP exclusions, redacted output and fail-closed
  report/exit handling. No Go vulnerability/image/Helm security pass is claimed.
- `tests/tooling/**`: 16 tests including actual scanner detection/exclusions.
  Hash-locked Python dependencies and action/tool/schema identities are tracked.
  Third-party OpenAPI schema license and notice are retained.
- Dependabot covers Go, GitHub Actions and permanent Python tooling.

## Required hosted acceptance evidence

| Case | Source / checkout identity | Result and actual run |
| --- | --- | --- |
| Trusted main | `33cefaf56cdf271b0ba6cdccd4c3099a8b95ee0c` | [38143463708](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/38143463708): success; every Bootstrap checks step passed, 16 tests, zero source-secret findings |
| Deliberate failure PR | head `8dad848a1f8721d789a4d9e137c08796aa063f7d` | [38143508169](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/38143508169): failure at explicit exit 1; later checks skipped, PR mergeStateStatus BLOCKED with required check active |
| Restored PR | head `46bdcf60299b63e98263fa0e8aed44b360e48196`; tested merge checkout `fa3528a539d56b8cf4fa3a844c8c6ee528f534b3` | [38143589236](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/38143589236): success; 16 tests, zero findings, PR mergeStateStatus CLEAN |

Main and PR runner logs show GITHUB_TOKEN `Contents: read` and `Metadata: read`.
Repository Actions defaults are read-only and cannot approve PR reviews;
configured repository Actions-secret count was zero. The permitted disposable
same-repository PR path was exercised; an independent fork was not created.
Static checks enforce pull_request (not pull_request_target), no secret context
and no publish/cache writes. These results do not prove C02 release predicates.

After main's real success, existing active ruleset 24875239 was extended with
required `Bootstrap checks`, GitHub Actions integration 15368, strict up-to-date
policy and enforcement on creation. GET read-back preserved deletion,
non-fast-forward and PR/resolved-thread protections, with no bypass actors.
No failed PR was merged. The temporary exit-1 step was removed after its failure
was recorded, and the unchanged real checks then passed.

## Local evidence and verification limits

From project root, installed hash-locked tooling runs:

```sh
rtk proxy python3 tools/check_contracts.py
rtk proxy python3 tools/check_agents.py
rtk proxy python3 -m unittest discover -s tests/tooling -v
rtk proxy actionlint .github/workflows/ci.yml
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod download
rtk proxy env GOTOOLCHAIN=local GOWORK=off go mod verify
rtk proxy python3 tools/security/check_secrets.py
```

Fresh local Python 3.12.3 environment, permanent hash lock and pip check passed.
All 16 tests, contract/agent checks, actionlint 1.7.12, Go 1.26.5 module checks
and actual Trivy 0.75.0 source scan passed; hosted CI independently repeated them.
Local ShellCheck was unavailable; no standalone ShellCheck pass is claimed.
Go has no application packages yet, so no application-test/runtime pass is claimed.
Default git diff --check found pre-existing blank lines at EOF; checking other
whitespace classes with core.whitespace=-blank-at-eof passed. Approved source
bytes were preserved rather than silently reformatted.

Initial RED was an import/harness error, not semantic proof. GREEN fixes covered
YAML colon quoting, the real clean Trivy report without Results and a false
permission-detector match. Six independent in-process PROVE mutations triggered
assertions for writable permissions, unknown requirements, duplicate YAML,
exit-zero findings, raw match leakage and private-directory scanning. Mutations
were restored and temporary probe removed. Hosted exit 1 separately proved real
job failure and required-check denial.

Scanner binary SHA256: `93f9da8e4ba5e0c1c76d8234ed2494cf9afb0a96fd21953e424bb795f3299b8e`.
Secret config SHA256: `1ca45873ff134fddd96ff540386f280777fb429b2c328349d9614bb3a8e91c82`.

## Reviewed specification inputs

- source_version: `626342ee71f5181542a95897369aa7cfa0c2982a7e3a9e599df437e5f3b7df90`
- specification_version: `25616a15c355593138f32986719f830ca2ce2fac6cf1e9e1a2c03209fdd33c02`
- review_ref: `8af11f7b-6523-4d43-926e-bf5ff1d3d1b2`

Fresh specification check passed before publication and completion. No approved
behavior or MCP-owned source changed. Luna scout research was read-only;
implementer verified findings and owned all writes/checks. Effective model
identity and savings were not independently measured. Home Lab information
remains private under .local/homelab; no cluster/GPU operation occurred.

C01 completion evidence updates are retained in the authorized test PR; merging
that PR is outside the granted authorization. Stop here. The next eligible packet
is F01, after its own explicit invocation; no further implementation is pending
within C01.
