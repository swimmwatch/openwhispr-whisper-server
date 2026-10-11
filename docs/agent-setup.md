# Codex project setup

AGENTS.md is the single project instruction source. Codex discovers the two
project-local skills in .agents/skills/gpu-acceptance and
.agents/skills/release-verification. Their canonical SKILL.md files travel with
the repository; no personal path or external workflow snapshot is required.

## Scope

The user selected Codex project files only on 2026-10-11. The former second-client
and full external-bundle acceptance checks are cancelled by that explicit scope
change, not reported as passing. No other client is launched or configured.
External engineer-agent-workflows remains separately installed operator tooling;
its publication/distribution is outside I03. Explicitly invoked plugin workflows
still require their own matching manual/MCP and never receive authority from a
local replacement. Optional connectors are documented only when actually present.

Project/global model, provider, trust, sandbox and approval settings are preserved.
No empty settings or guessed MCP registration is needed. Ordinary builds and
contributor CI do not require agent clients, RTK, connectors or private access.

## Structural check

Python 3.11+ is required for this developer tool, not for the service:

```sh
rtk proxy python3 tools/check_agents.py
```

The check validates architecture rule markers and canonical references, skill
metadata and source containment, limited reviewed Codex configuration shapes,
obvious personal paths/credential material and absence of unsupported client
adapters/external snapshots. It does not comprehensively scan every possible
secret or prove runtime architecture, native discovery or cluster access.

Configuration is optional. If later required, .codex/config.toml accepts only
reviewed stdio mcp_servers registrations with command, string-list args and
variable names in env_vars. Secrets stay in environment/secure local settings.
Unsupported settings require a scoped review; global controls are never copied.

For a disposable fixture, the same checker accepts --root <fixture-directory>.
Negative cases must use that fixture, never break real project instructions or
copy private Home Lab files. One-off fixtures/runners remain ignored under .local.

## Native acceptance

Start a fresh Codex session with normal project discovery and configured controls.
From the repository root, the tested CLI entry point is:

```sh
rtk proxy codex exec --ephemeral --json --cd . PROMPT
```

PROMPT is a read-only acceptance request: identify native-discovered instruction
and skill sources before manual lookup, load both declared project skills, then
explain the three architecture cases below with actual source/rule/packet refs.
It grants no writes, installs, child agents, next packet, publication or deployment.

| Case | Expected source and explanation |
| --- | --- |
| Echo imported by application / HTTP status in a use case | G01-G03; consumed application ports and HTTP error mapping, F01 with later P02-P04 wiring |
| Root runtime / missing CUDA architecture / native HTTP host port | D04/D02/D06; P05 image/Compose checks and P07 runtime evidence |
| Shared backend Service / changing selector / cache-deleting teardown | H02/H06; P06 chart checks and P08 runtime/retention evidence |

No Go/image/chart runtime artifacts exist yet; explain planned symbols as planned.
The GPU and release skills must report the unavailable P07/P08/C02 harnesses and
refer to their owning packets, without inventing commands or acceptance reports.

On this workstation, also resolve .local/homelab/README.md privately and execute
only its recorded API version query with explicit dedicated kubeconfig/context.
Capture and suppress raw output/private identities; report only command exit and
connection success. Contributor checkouts can lack this directory. No namespace
or release is selected; runbook resolution never authorizes deployment or a probe.

The previous optional read-only sandbox override denied that API query with
operation not permitted. A fresh native session using normal configured controls
passed the same read-only operation. Do not add overrides or bypass controls to
force acceptance. Record actual client version and sanitized observations; raw
private transcripts are temporary and removed after inspection.

## Evidence

Observed on 2026-10-11 with Codex CLI 0.159.2:

| Check | Actual result |
| --- | --- |
| Scope RED | The populated project check rejected the superseded REQ-025 client requirement before the scope correction |
| Structural GREEN | `rtk proxy python3 tools/check_agents.py`: exit 0 |
| Fixture PROVE | Rejected an unsupported client adapter, external plugin snapshot, personal path, missing rule marker, wrong/duplicate/unterminated skill metadata, sandbox override, malformed MCP args/env/TOML and an escaping skill source; accepted the reviewed stdio MCP shape |
| Native discovery and source loading | Fresh normal-configured `codex exec` reported AGENTS.md and both project skill paths from its initial context before manual lookup, then read all three sources |
| Architecture comprehension | Located G01-G03 and application-owned ports/HTTP mapper, D04/D02/D06 and P05/P07 ownership, H02/H06 and P06/P08 ownership; distinguished planned symbols from absent Go/runtime code |
| Native structural command | The fresh client ran the project checker: exit 0 |
| Private connection | The fresh client resolved the runbook and ran its explicit-target `/version` query with captured output and a 30-second timeout: exit 0; JSON contained `gitVersion` |

Temporary fixture checks ran under ignored .local/i03-codex-only; real sources
were not mutated for PROVE. The native runner exited 0; acceptance is based on
the separate discovery, source, policy, checker and connection observations above.
Only sanitized observations are retained; the raw private transcript was removed.
Specification approval/freshness is a separate workflow check recorded in the
current handoff. Unavailable P07/P08/C02 harnesses are later-packet prerequisites,
not passed GPU or release checks.
