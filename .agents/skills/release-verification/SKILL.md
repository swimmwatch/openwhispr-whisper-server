---
name: release-verification
description: Inspect explicitly authorized Whisper release qualification, immutable artifact identities and required evidence without granting publication or deployment authority.
---

# Release verification

Read root AGENTS.md and the owning C02/R01/R02 packet under
docs/specs/gpu-whisper-server/tasks. This skill grants no publication, promotion,
commit, push or cluster deployment authority.

C02/R01 now provide real inventory and security gates. From the project root,
with the pinned uv/Python environment and actual downloaded evidence:

```bash
uv run --locked python tools/release/check.py --inventory "$INVENTORY" --stage candidate
uv run --locked python tools/security/check.py --inventory "$SECURITY" --stage complete
```

These validate the supplied evidence; they do not by themselves freshly read every
registry or run GPU acceptance. Reuse the existing release inventory, OCI and
publisher identity helpers plus native Docker/Buildx/Helm commands for independent
anonymous pulls. Keep client authentication state empty and task-local. Do not
invoke a publisher CLI as a supposedly read-only verification command.

Verify the same immutable API/backend image manifests in GHCR and Docker Hub,
image digests and source/base/toolchain identities, SBOM/provenance/security/license
evidence and identical chart archive bytes in OCI/Pages/GitHub, with the separate
raw chart OCI manifest digest. Preserve older index entries. Reuse the owning
contracts; do not invent tags, policy thresholds or artifact names. See the
[actual rc.5 verification](<../../../docs/verification/release-rc5.md>) and
[published-target harness](<../../../tests/acceptance/README.md>) for executed
commands, identity binding and explicitly partial server-only evidence.
Report missing artifacts/checks honestly; do not create a substitute success report.

R01 publishes a GPU-unqualified pre-release. Both P07 and P08 acceptance suites
are independently required before R02 stable release. Failed, cancelled, skipped
or unavailable required checks block qualification. Changed candidate bytes need
a new immutable RC and renewed affected evidence; never promote an older report.
GPU-free builds and successful uploads are not GPU acceptance. Keep credentials
and private cluster evidence out of artifacts/logs; retain only sanitized reports.

Update this skill with actual C02/P07/P08 harness commands in their owning packets.
