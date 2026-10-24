# Codex project setup

AGENTS.md is the project instruction source. The project-local skills live in
`.agents/skills/gpu-acceptance` and `.agents/skills/release-verification`.
Codex discovers these files through its native project support; do not introduce
a Python checker for their presence, syntax, metadata or rule markers.

Use the existing Go tooling, Ruff, mypy, actionlint, ShellCheck and Helm tools for
source and configuration linting. Project tests exercise service behavior, shell
commands, artifact handling, scanner results and release operations. Follow the
[behavior-focused testing rules](../AGENTS.md#test-behavior-not-repository-files)
and [contributor checks](../CONTRIBUTING.md#python-tooling).

The GPU and release skills guide explicitly authorized acceptance work. Reading
instructions, passing a linter or finding files does not prove that a GPU,
Kubernetes deployment or release works. Report only evidence actually obtained.

External engineer-agent-workflows is separately installed operator tooling. An
explicitly invoked workflow requires its matching manual/MCP. No project-local
replacement grants authority to edit workflow-managed state or publish artifacts.

Keep temporary scripts and observations under ignored `.local/`. Resolve private
Home Lab access there only when required by an authorized task. Never copy private
identities, credentials or agent history into repository files or CI artifacts.
Preserve global model, provider, trust, sandbox and approval settings.
