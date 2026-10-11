#!/usr/bin/env python3
"""Validate portable Codex project files; native acceptance remains separate."""

import argparse
from pathlib import Path
import re
import sys
import tomllib


LOCAL_SKILLS = ("gpu-acceptance", "release-verification")
POLICY_REFERENCES = (
    "docs/specs/gpu-whisper-server/tasks/plan.md",
    "docs/specs/gpu-whisper-server/tasks/foundation_01_go_service.md",
    "docs/specs/gpu-whisper-server/tasks/05_images_standalone.md",
    "docs/specs/gpu-whisper-server/tasks/06_helm_chart.md",
)
UNSUPPORTED_ARTIFACTS = (
    "CLAUDE.md", ".claude/skills", ".claude/settings.json", ".mcp.json",
    ".agents/workflows",
)


def check(root):
    errors = []

    def require(condition, message):
        if not condition:
            errors.append(message)

    def read(relative):
        path = root / relative
        if not path.resolve().is_relative_to(root):
            errors.append(f"Managed file escapes project: {relative}")
            return ""
        if not path.is_file():
            errors.append(f"Missing file: {relative}")
            return ""
        return path.read_text(encoding="utf-8")

    for relative in UNSUPPORTED_ARTIFACTS:
        path = root / relative
        require(not path.exists() and not path.is_symlink(),
                f"Unsupported project agent artifact: {relative}")

    agents = read("AGENTS.md")
    for group in "GDH":
        for number in range(1, 7):
            require(f"{group}{number:02d}." in agents,
                    f"Missing architecture policy: {group}{number:02d}")
    for reference in POLICY_REFERENCES:
        require(reference in agents, f"Missing canonical policy reference: {reference}")
        read(reference)
    require(".local/homelab/README.md" in agents, "Missing private Home Lab runbook pointer")

    for skill in LOCAL_SKILLS:
        text = read(f".agents/skills/{skill}/SKILL.md")
        metadata = re.match(r"\A---\n(.*?)\n---(?:\n|$)", text, re.DOTALL)
        require(metadata is not None, f"Missing skill frontmatter: {skill}")
        header = metadata.group(1).splitlines() if metadata else []
        require(len(header) == 2, f"Unsupported skill metadata fields: {skill}")
        require(f"name: {skill}" in header and any(
            line.startswith("description: ") and line.removeprefix("description: ").strip()
            for line in header
        ), f"Invalid skill metadata: {skill}")
    require(".local/homelab/README.md" in read(".agents/skills/gpu-acceptance/SKILL.md"),
            "GPU skill must resolve private runbook")
    read("docs/agent-setup.md")

    # Configuration is optional. Accept only reviewed stdio MCP registration.
    config = root / ".codex/config.toml"
    if config.exists() or config.is_symlink():
        text = read(".codex/config.toml")
        try:
            value = tomllib.loads(text)
            require(set(value) <= {"mcp_servers"}, "Unreviewed Codex settings")
            servers = value.get("mcp_servers", {})
            require(isinstance(servers, dict), "Invalid MCP server mapping")
            if isinstance(servers, dict):
                for server in servers.values():
                    require(isinstance(server, dict) and
                            set(server) <= {"command", "args", "env_vars"},
                            "Unsupported MCP server schema")
                    if not isinstance(server, dict):
                        continue
                    command = server.get("command")
                    require(isinstance(command, str) and bool(command.strip()),
                            "Missing MCP command")
                    arguments = server.get("args", [])
                    require(isinstance(arguments, list) and
                            all(isinstance(x, str) for x in arguments),
                            "Invalid MCP arguments")
                    names = server.get("env_vars", [])
                    require(isinstance(names, list) and all(
                        isinstance(x, str) and re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", x)
                        for x in names
                    ), "Invalid MCP environment variable names")
        except (ValueError, TypeError):
            errors.append("Invalid Codex config")

    managed = [root / "AGENTS.md", root / "docs/agent-setup.md"]
    for directory in (".agents", ".codex"):
        base = root / directory
        if base.exists():
            managed.extend(p for p in base.rglob("*")
                           if p.suffix in {".md", ".json", ".yaml", ".toml"})
    for path in managed:
        relative = path.relative_to(root)
        if not path.resolve().is_relative_to(root):
            errors.append(f"Managed file escapes project: {relative}")
            continue
        if not path.is_file():
            continue
        text = path.read_text(encoding="utf-8")
        require(not re.search(r"/home/[^/\s]+/|/Users/[^/\s]+/|[A-Za-z]:\\Users\\", text),
                f"Personal absolute path: {relative}")
        require(not re.search(
            r"-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|\bgh[pousr]_[A-Za-z0-9]{20,}",
            text
        ), f"Credential material: {relative}")
    return sorted(set(errors))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    try:
        errors = check(args.root.resolve())
    except (OSError, UnicodeError, RuntimeError) as error:
        print(f"FAIL: agent check prerequisite: {type(error).__name__}", file=sys.stderr)
        return 1
    for error in errors:
        print(f"FAIL: {error}")
    if errors:
        return 1
    print("PASS: Codex project structure; native discovery/policy/access acceptance remains separate")
    return 0


if __name__ == "__main__":
    sys.exit(main())
