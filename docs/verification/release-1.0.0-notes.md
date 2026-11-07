# v1.0.0

> **Release with accepted exceptions:** GPU acceptance, security qualification and
> final documentation are incomplete. This release must not be described as fully
> qualified. The maintainer accepted these exceptions for v1.0.0 only; missing,
> expired and failed checks remain reported as such.

## What is included

Resident V1 provides a public transcription API and a separate backend using an
unchanged whisper.cpp server. The planned stable release promotes the immutable
API and backend images from v0.1.0-rc.6; it does not rebuild or replace them.
Deployment remains a separate operator action.

Real transcription on both GPU environments was exercised, including the Home
Lab. The recorded Home Lab checks also include selected public formats and SDK
requests, network-policy cases, and an offline cached-model transcription. These
are partial results, not the complete P07 or P08 acceptance suites for the
selected candidate.

## Important limitations and security risks

- Memory/garbage-collection qualification and continuous proof that old and new
  native GPU processes do not overlap during replacement are incomplete.
- P07/P08 evidence and P09 final documentation are incomplete. Do not interpret
  release publication as a pass for those checks.
- Security scans and narrowly scoped exception/component reviews have expired.
  The actual security gate is not a current pass, and new or unreviewed findings
  may remain. Image scans reported Echo Proxy advisories
  [CVE-2026-118264](https://github.com/labstack/echo/security/advisories/GHSA-cxf3-9h37-7rr8)
  (critical) and [CVE-2026-118262](https://github.com/labstack/echo/security/advisories/GHSA-9969-6676-49h5)
  (high); their earlier non-applicability reviews have expired and must not be
  presented as current approvals.
- The selected rc.6 images do **not** include the locally prepared mitigation
  for very short audio. Static review identified a possible path related to
  [CVE-2026-17512](https://ubuntu.com/security/CVE-2026-17512); end-to-end
  exploitability of the deployed server has not been established.
- Additional native-component advisories, including the malformed-model case
  [CVE-2026-110810](https://ubuntu.com/security/CVE-2026-110810) and
  [CVE-2026-10298](https://security-tracker.debian.org/tracker/CVE-2026-10298),
  require review against the pinned fork and its actual inputs.
- The public API has no built-in authentication. Restrict network access to
  trusted clients and provide transport protection as appropriate.

See the [rc.6 verification report](https://github.com/swimmwatch/openwhispr-whisper-server/blob/main/docs/verification/release-rc6.md) for exact results and the
recorded exceptions. The release-specific decision does not relax the checks
required for other releases or change the original image provenance.
