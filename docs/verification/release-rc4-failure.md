# rc.4 pre-publication failure and reviewed snapshot refresh

Status: rc.4 publication **failed**; a separately authorized rc.5 is pending.
No rc.4 release qualification is claimed. No existing tag or artifact was replaced.

## Actual failure

[Release 40477031110, attempt 1](https://github.com/swimmwatch/openwhispr-whisper-server/actions/runs/40477031110/attempts/1)
selected source `bbafe3f965f9a6ab5fe3ffbf99276b6c5c36ce35`. Metadata, both cold
builds, chart and runtime/license checks passed. The pre-publication qualification
step rejected `api-vuln`. Registry/Helm authentication, image/chart publication,
Pages and GitHub Release were skipped: this attempt made no distribution writes.
The missing partial-channel-state upload is secondary: publication had not started.

The runner intentionally suppressed raw scanner exceptions. Its exact original
exception was not preserved, so the following local reproduction distinguishes
observed stages rather than inventing that missing diagnostic.

## Reproduction on the selected artifact

Selected API index:
`sha256:b6fdeedd271b0b706f337f62c087a60e8649b3124c2deac65aa4405f0a053b57`.
Existing OCI inspection verified its source and immutable identity. Pinned
Trivy 0.75.0, the original scan flags, empty config/ignore and sanitized environment
returned exit 0: **33 findings, zero blocking findings**. Existing report evaluation
and current supported database validation passed. This is not zero vulnerabilities.

The separate existing native advisory coverage function reproducibly rejected the
selected build with `Advisory source changed; review required`. All seven reviewed
CVE JSON responses still matched their expected hashes. Only the
[Debian FFmpeg tracker](https://security-tracker.debian.org/tracker/source-package/ffmpeg)
HTML changed.

## Reviewed external-source difference — 2026-10-27

Both captured HTML documents contain 107202 bytes. They differ at exactly one
byte, zero-based offset 911: ASCII `3` became `4` in Available versions / forky:
`7:9.0.2-3` → `7:9.0.2-4`.

All 570 CVE rows, their statuses/descriptions/notes and all 633 ordered links are
identical. There are no added or removed CVE entries between these two snapshots.
This is an available Debian package revision change, not changed advisory data.

- Previous SHA256: `9dcb820e9767e8a2c2f74ad7ad0e441f3177815e5f63df4cb6f99613f3df4710`.
- Reviewed SHA256: `a128f2afae51d4972d31c47c9f303e19600536dd4e02d23e7eccff26ab81f242`.

The correction changes only the review date and this reviewed source hash in the
[native inventory](<../../tools/security/component-advisories.json>). It changes
no scanner, integrity/freshness rule, expiry, severity, applicability, runtime
source, dependency or suppression. Applicable native MEDIUM findings remain
visible: two FFmpeg advisories and the whisper.cpp model-loader advisory.

## Verification and remaining work

The original native source coverage failed before the refresh. After the reviewed
refresh, actual HTTPS/hash coverage passes for both API and backend with no
coverage gaps. All 141 tooling tests passed with 12 disclosed infrastructure skips,
including existing source-change/expiry/invalid-evidence denial checks. No new
checker, ignore, exemption or source-scanning test was introduced.

The user explicitly authorized the narrow correction, PR/CI, merge after all
required jobs pass and a new immutable `v0.1.0-rc.5`. The rc.4 tag remains frozen;
its failed publication is not relabeled successful. The agent must obtain fresh
complete CI/security qualification, execute rc.5 through the existing protected
Release workflow, then verify all channels and the new candidate's server HTTP
stage. Desktop, Kubernetes, stable and complete P07/P08 remain outside this scope.

Private evidence: [scan summary](<../../.local/release-watch/rc4-failure-analysis/api-trivy-summary.json>),
[source review](<../../.local/release-watch/rc4-failure-analysis/source-review.md>),
[exact external-source comparison](<../../.local/release-watch/rc4-failure-analysis/normalized-semantic-diff.json>),
[refreshed live coverage](<../../.local/release-watch/rc4-failure-analysis/refreshed-native-coverage.json>).
