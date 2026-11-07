# v0.1.0-rc.3 publication verification

**R01 complete — `candidate_published_unqualified`** (2026-10-26,
Europe/Moscow). All six distribution channels and GitHub assets passed independent
post-publication read-back. This is not full GPU acceptance or stable promotion.

## Selected source and publication

- Explicit maintainer approval selected rc.3 from merged
  [PR #10](https://github.com/swimmwatch/openwhispr-whisper-server/pull/10).
- Source/tag commit: `a47647049ad69ae154e327d8d2536d650eaa4dee`.
  The new `v0.1.0-rc.3` tag was created once; older tags were not moved.
- [CI 40333890561](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40333890561)
  passed all seven required jobs on PR head `2ad580ad38416dde0900f64d65788d61a37dfa87`.
- [Release 40347039761, attempt 1](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40347039761/attempts/1)
  passed clean builds, runtime/license checks, fresh selected security qualification,
  image/chart publication, Pages and GitHub Release. The stable-only alias job was
  intentionally skipped. Normal protected-environment approvals used the selected
  source/version authorization; protection was not disabled or bypassed.
- [Public GitHub pre-release](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.3)
  is not a draft. Its public latest-release API returned HTTP 404: there is no
  stable/latest release designation. No stable image aliases were advanced.
- Build identity: `sha256:23a4c1472d2627186dde1b46591523f9c3e763e7743074b6c8c1f891ee39a3be`.
  Runtime identity: `sha256:2cbb5d9db5451a3cbba00d76af845f35e4e86454bfe113eca10bda45c99eddbd`.
  The remote job used clean selected checkouts, not the dirty operator workspace.

## Immutable artifacts

The same indexes are anonymously readable in both GHCR and Docker Hub:

| Artifact | Immutable identity |
| --- | --- |
| API image index | `sha256:6cd42d7046d27ea12974b1f6a4d746f381549258da81b273d90ee9a62c0accd2` |
| Backend image index | `sha256:d9e94866fcd015c8c2091e5c6d79afeec01432b110aaeb23bb6d14586453de1e` |
| Chart archive SHA256 | `5ed39898d44e087e1947456803a0b8e94467811f8cce8284e9bf83e49537ae35` |
| Chart OCI manifest | `sha256:faee81436a9dd0e5419265765057b8483980fce1d77d3c8cf1f1aa330250ba58` |

Image repositories are `ghcr.io/swimmwatch/openwhispr-whisper-server-api`,
`docker.io/swimmwatch/openwhispr-whisper-server-api` and the corresponding
`-backend` repositories. Image version tags omit the Git `v` prefix:
`0.1.0-rc.3`; the full-source alias is
`sha-a47647049ad69ae154e327d8d2536d650eaa4dee`.
The chart is `oci://ghcr.io/swimmwatch/charts/whisper-server`, version `0.1.0-rc.3`,
and the [classic chart repository](https://swimmwatch.github.io/openwhispr-whisper-server/).

## Actual post-publication checks

| Check | Result |
| --- | --- |
| Four native Docker pulls, selected `linux/amd64` indexes, new empty credential configuration | PASS |
| Version/source aliases, raw index/platform/config bytes across both registries | PASS |
| Actual attached SPDX 2.3 and SLSA v1 documents, source/build/base/toolchain metadata | PASS |
| Embedded component pins, non-root UID/GID 10001, unchanged native source and CUDA architecture flags | PASS |
| Native anonymous Helm OCI and classic pulls with isolated empty client state | PASS |
| OCI/Pages/GitHub chart archives, byte equality and SHA256 | PASS |
| Separate chart OCI manifest digest and public index mapping | PASS |
| Entire prior rc.2 index entry and archive checksum preserved | PASS |
| Published chart strict lint/render with both registry/digest overrides | PASS |
| Registry-normalized render equivalence (eight resources in each rendering) | PASS |
| Anonymous GitHub asset downloads and `sha256sum --check SHA256SUMS` | PASS |
| Existing candidate inventory gate on downloaded final channel state | `candidate_published_unqualified` |
| Existing complete security gate on downloaded selected report | PASS |
| Pinned Approved/reviewed specification and exact R01 agreement | `pass` / `ready` |

Existing gate commands, from the project root through the pinned uv environment:

```sh
rtk proxy .local/uv-migration/bin/uv run --locked python tools/release/check.py \
  --inventory .local/release-watch/rc3-readback/channel-state.json --stage candidate
rtk proxy .local/uv-migration/bin/uv run --locked python tools/security/check.py \
  --inventory .local/release-watch/rc3-readback/security/_temp/selected-security.json \
  --stage complete
```

The 22 selected security targets have no blocking findings or unresolved coverage
gaps under the owning policy. This is **not zero vulnerabilities**. The report
retains applicable MEDIUM native advisories `CVE-2026-107676`, `CVE-2026-107678`
(FFmpeg) and `CVE-2026-17513` (whisper.cpp). Other nonblocking package findings and
narrow documented exceptions remain in the report. No fresh independent advisory
research or signature/SLSA-level certification is claimed by this read-back.

BuildKit statements have empty in-toto subject arrays; checked OCI attestation
manifest annotations bind the actual runtime manifest. Backend SPDX does not
separately identify whisper.cpp; native source/build metadata and the selected
component advisory review provide the explicit additional evidence.

## Evidence and remaining scope

Private bounded receipts: [images](<../../.local/release-watch/rc3-readback/images/result.md>),
[charts](<../../.local/release-watch/rc3-readback/charts/result.md>),
[final public inventory copy](<../../.local/release-watch/rc3-readback/channel-state.json>),
[selected security report](<../../.local/release-watch/rc3-readback/security/_temp/selected-security.json>).
All delegated jobs completed and were collected; no runtime container or GPU
workload was started by independent read-back. No cluster operation followed.

Full P07/P08 qualification is still outstanding; the published inventory has no
GPU report references. Earlier [local server verification](<docker-local-pre-release.md>)
is not substituted for acceptance of these immutable published artifacts. Current
OpenWhispr Electron compatibility remains unverified.

A missed protected-approval wake was corrected in the local
[operator monitor](<../../tools/release/watch_pipeline.py>) with a discriminating
RED-to-GREEN regression: all 24 watcher tests, Ruff and mypy passed. It now wakes
on `waiting`, not only terminal outcomes. That operator-only follow-up and these completion records are separate from
rc.3's frozen source and do not change its published artifacts.
Parallel cache/action/release-document changes were preserved and not published.
