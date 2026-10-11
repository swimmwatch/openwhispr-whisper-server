---
name: release-verification
description: Inspect explicitly authorized Whisper release qualification, immutable artifact identities and required evidence without granting publication or deployment authority.
---

# Release verification

Read root AGENTS.md and the owning C02/R01/R02 packet under
docs/specs/gpu-whisper-server/tasks. This skill grants no publication, promotion,
commit, push or cluster deployment authority.

The C02 build/release harness is planned and not implemented. No executable
release-verification command is currently available. Report missing harnesses,
candidate artifacts or checks; do not create a substitute success report.

When C02 supplies real commands, verify the same immutable API/backend image
manifests in GHCR and Docker Hub, image digests and source/base/toolchain identities,
SBOM/provenance/security/license evidence, anonymous pulls, and identical chart
archive bytes in OCI/Pages with separately recorded OCI digest. Reuse the owning
contracts; do not invent image tags, policy thresholds or artifact names.

R01 publishes a GPU-unqualified pre-release. Both P07 and P08 acceptance suites
are independently required before R02 stable release. Failed, cancelled, skipped
or unavailable required checks block qualification. Changed candidate bytes need
a new immutable RC and renewed affected evidence; never promote an older report.
GPU-free builds and successful uploads are not GPU acceptance. Keep credentials
and private cluster evidence out of artifacts/logs; retain only sanitized reports.

Update this skill with actual C02/P07/P08 harness commands in their owning packets.
