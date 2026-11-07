# Security review — 2026-11-07

**Status: partial remediation; the complete security gate remains unqualified.**
This review concerns the current source checkout and primary advisory records
checked on 2026-11-07 (Europe/Moscow). It does not renew the historical
[rc.6](<release-rc6.md>) scans or qualify the
[v1.0.0 release-only exception](<release-1.0.0-notes.md>).
The [overall audit](<audit-2026-11-07.md>) records integrated check results.
No native source, CUDA pin, deployment or published artifact was changed here.

## Go dependencies and exceptions

- Echo was updated from 5.4.0 to 5.4.1 by the source owner. The
  [official release](https://github.com/labstack/echo/releases/tag/v5.4.1) fixes
  the relevant Proxy, trusted-peer and BodyDump/BodyLimit defects. Official Go
  records [GO-2026-6838](https://vuln.go.dev/ID/GO-2026-6838.json),
  [GO-2026-6839](https://vuln.go.dev/ID/GO-2026-6839.json),
  [GO-2026-6841](https://vuln.go.dev/ID/GO-2026-6841.json) and
  [GO-2026-6844](https://vuln.go.dev/ID/GO-2026-6844.json) identify 5.4.1 as fixed.
  The baseline source scan reported four symbol findings; the final verbose
  scan covered 21 modules and Go 1.27.3, with zero symbol findings and zero
  imported-package findings. Scanner output is not proof of exploitability.
- One module-only result remains:
  [GO-2026-5932](https://vuln.go.dev/ID/GO-2026-5932.json) concerns the
  unmaintained `golang.org/x/crypto/openpgp` packages. The final scan identifies
  `golang.org/x/crypto@v0.57.0`, but no imported affected package or reachable
  call. This is not a zero-advisory dependency graph or a reason to claim that
  OpenPGP is safe. No unrelated crypto dependency change was made.
- Removed the four expired Echo Trivy scopes from
  [exceptions](<../../tools/security/exceptions.json>): `GHSA-cxf3-9h37-7rr8`,
  `CVE-2026-118264`, `CVE-2026-118262` and `GHSA-9969-6676-49h5`.
  Unused middleware is not used to retain obsolete exceptions after upgrading.
  The source owner's final gosec scan reported seven findings (raw exit 1),
  down from eight, with no Go compilation errors. The model-header G115 finding
  disappeared after validating the unsigned value before conversion; its obsolete
  exception was removed. The remaining six scoped records cover seven findings
  and keep their existing owner, exact match and 2026-11-22 expiry. The existing
  policy evaluator reported zero blocking findings and seven matched exceptions
  from those six scopes. Raw scanner findings and policy-normalized acceptance
  are separate results.
- Updated the Echo entry in [third-party notices](<../../THIRD_PARTY_NOTICES.md>)
  after reading the installed 5.4.1 MIT license. Corrected the obsolete claim
  that no images had been published. Neither change is a complete license audit.

## Native inference findings

The selected OpenWhispr revision is
`6ac14546046ffb17d144a08bcad65337e535cf5f`. The cached source archive matched
SHA256 `ef9c2b2e765c245686acb86b3f4668eb70a83894d1d5f06164206217979073ad`
from [build inputs](<../../containers/build-inputs.json>). Source inspection
confirmed the patterns below; no malformed-model or GPU exploit test was run.
Severity follows the CNA CVSS 4.0 assessment, not an invented project score.

| Advisory | Verified boundary and remaining limitation |
| --- | --- |
| [CVE-2026-17512][native-short] — MEDIUM | Native reflective padding copies 200 samples from `samples + 1` without a minimum-length check. The current [supervisor](<../../internal/adapter/backend/inference.go>) validates and pads short canonical WAV input to at least 201 frames. [Boundary regressions](<../../internal/adapter/backend/short_wav_test.go>) and [public API integration](<../../internal/adapter/backend/short_wav_integration_test.go>) use a native stand-in, not the actual CUDA server. Published rc.6 lacks this mitigation. |
| [CVE-2026-110810][native-tensor] — LOW | The pinned tensor loader lacks the `ttype` range check in the [upstream fix](https://github.com/ggml-org/whisper.cpp/commit/60c0be6ac8fa71b1a2ae2dd938a31a34a508e774). [Go metadata checks](<../../internal/adapter/models/metadata.go>) read only the fixed 48-byte header; they do not parse tensor records. Operator-selected, checksummed models reduce the input surface but a checksum does not prove parser safety. |
| [CVE-2026-10298][native-dimension] — MEDIUM | The unchanged native source retains the zero-dimension pattern described in [issue 3807](https://github.com/ggml-org/whisper.cpp/issues/3807). Go rejects zero/negative dimensions before native load, narrowing that specific trigger. This is not complete model validation or proof of actual native recovery. |

The three records were added to
[component advisories](<../../tools/security/component-advisories.json>), with
raw-source SHA256 values. The existing applicable MEDIUM CVE-2026-17513 remains
visible. These are native-component findings, even where the Go boundary supplies
a narrower mitigation. No new model parser or native fork was introduced.

### Native patch availability

- The tensor fix is reported in upstream ggml-org 1.9.5; its
  [tag](https://api.github.com/repos/ggml-org/whisper.cpp/git/tags/d2a764c648779feb36d44363f212a29b00aad1af)
  resolves to `d1be6fde11ac6e0407606b4e42fe72d34add8037`.
  This is not an established drop-in OpenWhispr update.
- The exact [OpenWhispr 0.0.10 → 0.0.11 diff](https://github.com/OpenWhispr/whisper.cpp/compare/0.0.10...0.0.11.diff)
  changes binary-packaging workflow configuration only, not the affected native
  source. A tag bump to 0.0.11 would not supply these fixes.
- The short-audio [PR 3925](https://github.com/ggml-org/whisper.cpp/pull/3925)
  was still open and unmerged. An automatically closed issue is not evidence of
  a fixed release. No compatible single update resolving all findings was verified.

## FFmpeg profile and new advisories

The selected source is FFmpeg 9.0.2 at
`946fcce07b6dcd0331c8cc609192aeff5e1924f8`. At the audit baseline, the source
configuration disabled networking, allowed only `pipe`/`cache` protocols and
selected seven media demuxers, but disabled only H.264 decoding explicitly.
Most image/video decoders were not excluded. Audio-only output does not establish their absence or prevent
all input probing. That baseline required actual compiled-profile checks, not an
assumption that audio-only output makes every video decoder unreachable.

The packaging owner has applied a narrow
[current-profile change](<../../containers/api.Dockerfile#L123-L147>): explicitly
exclude `h264`, `g2m`, `pcx`, `sunrast`, `cdxl`, `huffyuv`, `ffvhuff` and `hymt`,
without changing FFmpeg source. The last three share Huffyuv decoder code and must
be excluded together.

**Initially verified local image:** the first Linux/amd64 API image index was
`sha256:815eb87f85b1cd68dec933212e4156d5db001052359d513d4678f4ddd2743240`.
The build log's exported manifest-list identity, actual inventories and test output
were read independently after the packaging owner ran the checks:

- All eight named decoders and the IMF demuxer are absent. Input protocols are
  exactly `cache` and `pipe`; the only output protocol is `pipe`. Other video
  decoders and device-format entries remain, so this is not an audio-code-only build.
- `TestAPIDecoderExcludesUnusedNativeParsers` passed, including actual decoder,
  demuxer and native parser registry checks. The negative controls had rejected
  the old local image, which is not evidence for this new image.
- `TestPackagedAudioNormalization` passed all twelve cases: ten supported-audio
  cases plus H.264/AAC MP4 and Huffyuv/PCM Matroska. These exercise the actual
  packaged FFmpeg through the real normalizer, not only source configuration.

After the runtime OS/CA updates described below, rebuilt API index
`sha256:762f3d18466e4936d64b04d122e92dd749ba89d5219c998ee4a78939bb349e12`
repeated the actual profile/parser checks and all twelve normalizer controls with
`-race`: PASS, 2.808 seconds. Fresh demuxer/protocol inventories confirmed the same
exclusions. This was an intermediate result. The final API image listed below
then repeated these checks and all twelve controls successfully in normal and
Go race runs. Current advisory metadata binds the scoped findings to that final
image, not merely to either earlier image's results.

This supports exclusion of the six new FFmpeg cases for this local compiled
profile. It is not a complete image-security gate, upstream source fix, registry
publication or evidence that older published images have the same exclusions.

The current [Debian tracker](https://security-tracker.debian.org/tracker/source-package/ffmpeg)
exposed six additional records absent from the earlier inventory. Their CNA
records describe affected upstream versions through 9.0.2; all are MEDIUM.
Distribution version status alone does not prove applicability to this build.

| Advisory | Scope in the verified local image above | Official fix availability |
| --- | --- | --- |
| [CVE-2026-120210][ff-imf] | IMF ASSETMAP path traversal: not applicable because the actual IMF demuxer is absent. | [PR 25341](https://code.ffmpeg.org/FFmpeg/FFmpeg/pulls/25341) was open, not a merged release fix. |
| [CVE-2026-120243][ff-g2m] | G2M Kempf palette stack disclosure: not applicable because `g2m` is explicitly excluded and absent from the actual decoder registry. | [G2M source fix](https://github.com/FFmpeg/FFmpeg/commit/edad2d0dafcf44fa315eab716fd0183ce808053b). |
| [CVE-2026-120244][ff-pcx] | Truncated PCX RLE heap disclosure: not applicable because `pcx` is excluded, rather than relying only on direct-image demuxer absence. | [PCX source fix](https://github.com/FFmpeg/FFmpeg/commit/ecd5dfc308a164757e7f25b1afefd8db21b43cf9). |
| [CVE-2026-120245][ff-sun] | Sun Raster heap disclosure: not applicable because `sunrast` is excluded and absent from the actual decoder registry. | [Raster initialization](https://github.com/FFmpeg/FFmpeg/commit/356a391bf57df2fb80fd61f2821c8c83d15ce180) and [packed-line expansion](https://github.com/FFmpeg/FFmpeg/commit/66b6ca270b33fd74367d7d371f6de16ec664446e) fixes. |
| [CVE-2026-120246][ff-cdxl] | CDXL HAM stack disclosure: not applicable because the actual `cdxl` decoder and demuxer are absent. | [CDXL source fix](https://github.com/FFmpeg/FFmpeg/commit/47313ad3f9f33b892384b976fb7c09b3c85fd74e). |
| [CVE-2026-120247][ff-huffyuv] | Huffyuv heap disclosure: not applicable because all three shared-code entries `huffyuv`, `ffvhuff` and `hymt` are excluded and absent from the actual registry. | [Huffyuv source fix](https://github.com/FFmpeg/FFmpeg/commit/7c73707355708c846f718cca90096bc1a33c8989). |

All six records were added with source hashes, initially unresolved. After the
exact-image checks above, their dispositions became `not_applicable` to that
compiled profile. The whole FFmpeg review remains unresolved and expired; earlier
applicable MEDIUM findings have not been suppressed or declared fixed.
The [official tag list](https://api.github.com/repos/FFmpeg/FFmpeg/tags?per_page=10)
showed 9.0.2 and 9.1-dev; the exact 9.0.3 tag lookup returned HTTP 404. No newer
compatible stable patch containing all fixes was verified. Adopting a development
revision or local backports would require new source identity, regression,
build/profile and license evidence; it is not justified merely to clear metadata.
No FFmpeg source pin changed. The narrow packaging exclusion above removes unused
video/image decoder capabilities; the supported-audio positive controls passed.

## Fresh image OS findings and remediation

A subsequent Trivy 0.75.0 vulnerability scan used database metadata updated at
`2026-11-06T18:59:52Z` and downloaded at `2026-11-06T21:50:33Z`. The original
locally built indexes were:

- API: `sha256:815eb87f85b1cd68dec933212e4156d5db001052359d513d4678f4ddd2743240`.
- Backend: `sha256:d8c2fae2422d2fa7e2a26eb69102afc4f96feaa19bd1047bd7189be79d4581ed`.

**Both images had three HIGH package findings, zero Trivy-CRITICAL findings and
one UNKNOWN finding.** Three HIGH rows represent two distinct CVEs, not three
independent vulnerabilities. The earlier FFmpeg profile checks did not detect or
clear these OS packages.

| Advisory and package | API installed | Backend installed | Ubuntu 24.04 fixed version |
| --- | --- | --- | --- |
| [CVE-2026-109518](https://ubuntu.com/security/CVE-2026-109518), `apt` and `libapt-pkg6.0t64` | Both `2.8.3` | Both `2.7.14build2` | `2.8.4ubuntu0.1` |
| [CVE-2026-111497](https://ubuntu.com/security/CVE-2026-111497), `libbrotli1` | `1.1.0-2build2` | `1.1.0-2build2` | `1.1.0-2ubuntu0.1` |

The primary Ubuntu advisories independently confirm both fixed versions. Trivy's
HIGH label follows Ubuntu priority; the Brotli advisory also reports a critical
CVSS score (3.x: 9.8) and cites the CISA known-exploited-vulnerabilities catalog. Zero
Trivy-CRITICAL rows must not be read as proof of low risk.

The UNKNOWN row in both binaries is the previously described GO-2026-5932 for
`golang.org/x/crypto@v0.57.0`. Trivy observes module presence, whereas the source
scan found no affected imported package or reachable symbol. No new blanket
UNKNOWN exemption or HIGH-package waiver was added.

The lead initially approved the exact runtime APT/libapt and Brotli security
updates, retaining the existing base, CUDA, FFmpeg, native and Go versions. Both
first rebuilds then failed with APT exit 100 before compilation: the existing
`ca-certificates=20260816ubuntu0.24.04.1` pin was no longer present in freshly
validated Ubuntu indexes, a stale input previously hidden by cached layers.
The signed repository metadata exposed `20260816ubuntu0.24.04.2` in
noble-updates/noble-security; the lead approved only that additional CA revision.
The intermediate API rebuild at index
`sha256:762f3d18466e4936d64b04d122e92dd749ba89d5219c998ee4a78939bb349e12`
then scanned with zero HIGH/CRITICAL findings, 24 MEDIUM, four LOW and one UNKNOWN.
Its database snapshot was unchanged, downloaded at `2026-11-06T22:10:45Z`.
This verifies removal of the reported HIGH rows for that API image, not a complete
security pass. Additional runtime-only patches below make it an intermediate
image. Final paired-image inventories and scan results follow below; the original
scan findings and failed build attempts remain part of the history.

APT also affects the package-install/bootstrap boundary, not only an idle runtime
binary. Ubuntu's [official remediation guidance](https://ubuntu.com/blog/apt-vulnerability-fixes-available)
recommends temporarily setting `Acquire::Retries=0` while installing this security
update to prevent the vulnerable retry path, including a malicious-mirror case.
Both [API](<../../containers/api.Dockerfile>) and
[backend](<../../containers/backend.Dockerfile>) now explicitly use zero retries
for builder update/install operations and the runtime APT bootstrap. Builder APT
packages remain unchanged: this is a mitigated builder bootstrap, not a patched
builder inventory. Runtime installs use the configured retry count only after
installing the fixed APT/libapt versions. Fixing only the final package inventory
would not protect earlier installs. No global APT settings were changed by this
review.

### Additional available lower-severity fixes

The intermediate API scan had eight MEDIUM package records with a fixed version:
seven for `libpcre2-8-0` and one for `zlib1g`. The primary Ubuntu notices confirm
these exact Ubuntu 24.04 updates:

- [USN-8974-1](https://ubuntu.com/security/notices/USN-8974-1), published 2026-11-03:
  `libpcre2-8-0` from `10.42-4ubuntu2.1` to `10.42-4ubuntu2.2`, covering
  CVE-2026-103111, CVE-2026-86145, CVE-2026-89156, CVE-2026-89157,
  CVE-2026-89158, CVE-2026-89160 and CVE-2026-89161.
- [USN-8954-1](https://ubuntu.com/security/notices/USN-8954-1), published 2026-10-22:
  `zlib1g` from `1:1.3.dfsg-3.1ubuntu2.2` to `1:1.3.dfsg-3.1ubuntu2.3`, covering
  CVE-2026-85091.

The lead approved these exact updates in both runtime images only; native source,
base images and builder package inputs remain unchanged by this follow-up.
Vendor advisories have architecture/API-specific conditions; package findings are
not evidence of seven reachable amd64 service exploits. No lower-severity waiver
was added. The packaging owner verified signed repository availability and
rebuilt the final pair; the actual scan outcomes are recorded below.

The original backend image had a much broader Ubuntu package inventory: 189
MEDIUM rows across 116 advisory IDs and 34 packages, including 173 rows with
reported fixes; plus 19 LOW rows across 11 IDs and 15 packages, including 15 rows
with reported fixes. These are original-image counts, not a final-image result.
The two API package updates alone do not resolve this backend inventory. Any
additional remediation is limited to explicitly selected installed Ubuntu runtime
packages, signed Noble repository candidates and the same upstream/ABI series;
no blanket `apt upgrade`, CUDA/native changes or builder package updates are
included. The lead approved a 37-package backend runtime security-update set
after reviewing the signed repository candidates. The native builder was reused unchanged; the final
image checks covered the actual installed runtime package pins.

### Final local image results

The final Linux/amd64 pair uses the local `working-tree-audit-20261107-final`
label and these immutable local indexes:

- API: `sha256:00ad574bb6962c69520b89b752bdcb9a4477caed30501d3aa6b78ddc93b04586`.
- Backend: `sha256:8779c009dc0821e7b8dd2bdfbd1bae83cb6085b3c08093f00ce1bcf1aff2da2d`.

The build export identities, final scanner summaries and test logs were read
independently for this review:

- **Each final image:** zero HIGH, zero CRITICAL, 16 MEDIUM, four LOW and one
  UNKNOWN finding in Trivy 0.75.0. The database snapshot was still updated at
  `2026-11-06T18:59:52Z`, freshly downloaded at `2026-11-06T22:38:19Z`.
- Each image's residual MEDIUM rows cover 13 advisory IDs/eight packages; LOW
  rows cover two IDs/four packages. None of those MEDIUM/LOW rows had a fixed
  version reported by this database snapshot. That is not proof that no fix
  exists elsewhere or that the findings are harmless. The UNKNOWN module-only
  OpenPGP finding remains visible; no blanket exemption was introduced.
- Secret scans of image files and image configuration/history reported zero
  findings for each image, with scanner exit zero and no exceptions.
- The full P05 CPU suite passed in 66.499 seconds, including the actual image
  contracts, runtime security package pins, native profile and all twelve audio
  controls. The focused Go race suite passed in 13.158 seconds, again covering
  the profile, twelve audio controls and runtime package checks.
- An earlier standalone run failed because inherited umask `0077` made test
  fixtures unreadable. Test-only fixture permissions were corrected; the final
  normal/race runs passed without weakening runtime permissions.

These checks verify the selected local packaging remediations. They do not
constitute an aggregate security, native/GPU lifecycle or release qualification
pass. No image was published or deployed by this review.

## Scanner coverage limitation — HIGH, unresolved

The tooling owner reproduced a separate gap in pinned Trivy 0.75.0: an inline
`# trivy:ignore:AVD-DS-0002` suppresses a root-user Dockerfile finding even with an
empty ignore file and non-failing results included. Its configuration JSON drops
ignored rules without an exception counter; `--show-suppressed` is unsupported
for configuration scans, and the filesystem variant does not recover this case.
A clean-looking result therefore cannot establish complete configuration coverage.
The [real-tool regression](<../../tests/tooling/test_scanner_execution.py#L147-L168>)
is retained as an **expected failure**, alongside an unsafe-to-fixed positive
control. Expected failure is not a security-gate pass. A manual lead review found
no such suppression directives in the current containers/chart sources; that
bounded observation does not repair the general scanner coverage gap.
No custom repository-source checker, speculative flag or scanner-pin change was
introduced. The tooling owner and lead retain this open coverage issue; a native
scanner/reporting remedy and a passing regression are required before claiming
complete configuration-scan coverage.

A bounded follow-up checked the latest official release available on the review
date, [Trivy 0.76.2](https://github.com/aquasecurity/trivy/releases/tag/v0.76.2)
(published 2026-11-05), and its matching
[configuration CLI reference](https://github.com/aquasecurity/trivy/blob/v0.76.2/docs/guide/references/configuration/cli/trivy_config.md).
That reference still offers no inline-ignore disabling option; non-failure output
means successes. The released
[conversion code](https://github.com/aquasecurity/trivy/blob/v0.76.2/pkg/misconf/scanner.go)
still appends only passed/failed results in `ResultsToMisconf`. No compatible
released fix for this gap was verified. This was a documentation/source review,
not a test of a newly downloaded scanner or authorization to change the pinned tool.

## Evidence freshness and remaining work

The existing runtime-identity helper was rerun after freezing the entire approved
runtime package set, including the backend security-update batch. The final
partial inventory is bound to local inputs
`sha256:bfadeb149a48a1d34e3e582531f5bdb19fbbc69aeabc74e736b8078f869d8995`.
The prior
`sha256:67cacaabd78baabdc47e9e94013ac4039dcc9230633f5ee1d0d6d42223849511`
identity belongs to the intermediate OS/CA revision; the earlier
`sha256:fdc194de29a16c9903c9c20eb62066c674cbaa2f396208431407bbe414685b77`
identity belongs to the original images whose OS findings are retained above.
These are dirty-worktree input identities, not qualified source commits, registry
image digests or renewed security reviews.

- Both whisper.cpp and FFmpeg component conclusions are now `unresolved`.
  The original whole-inventory review date and all expired `2026-10-31` dates
  are intentionally retained. Newly hashed records do not refresh older sources
  or create a complete current inventory.
- The NVIDIA [CUDA bulletin](https://nvidia.custhelp.com/app/answers/detail/a_id/5755)
  returned HTTP 403 with a technical-difficulties page. The old CUDA review was
  not renewed. Current bulletin reconciliation and actual runtime package scope
  remain required; absence of fresh evidence is not a clean result.
- The scoped new FFmpeg cases now have exact final local API image evidence.
  Packaging and security owners still need complete native/CUDA/source-hash
  reconciliation, resolution of the Trivy coverage issue and the existing
  aggregate security gate. The scoped image scans above do not replace those
  checks, and a local source scan does not clear a published image.
- The backend/model owners' CPU tests support their Go boundaries. Actual selected
  native-image short-input behavior, model failure/reaping, and the independently
  authorized GPU/lifecycle acceptance evidence remain separate outstanding checks.

## Checks performed for this review

- Read the baseline and final verbose govulncheck outputs. The source owner
  reported baseline exit 3 and final exit 0; findings are summarized above.
- Standard JSON parsing of both changed security inventories passed using pinned
  uv 0.13.5 and the locked project environment. The initial attempt with PATH uv
  0.13.0 failed with exit 2; it was rerun using the repository-local pinned tool,
  without a global installation change.
- The final rerun of existing [security policy tests](<../../tests/tooling/test_security.py>)
  and [native advisory tests](<../../tests/tooling/test_native_advisories.py>) passed:
  29 tests, exit 0. The native tests cover unresolved/expired evidence and changed
  source hashes. No repository string checker or replacement security-policy
  test framework was added.
- This review did not run a complete fresh native/source-hash gate, full current
  image security gate, GPU exploit/recovery test, deployment or publication.
  Those omissions remain limitations, not passes.

[native-short]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/17xxx/CVE-2026-17512.json
[native-tensor]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/110xxx/CVE-2026-110810.json
[native-dimension]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/10xxx/CVE-2026-10298.json
[ff-imf]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120210.json
[ff-g2m]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120243.json
[ff-pcx]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120244.json
[ff-sun]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120245.json
[ff-cdxl]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120246.json
[ff-huffyuv]: https://raw.githubusercontent.com/CVEProject/cvelistV5/main/cves/2026/120xxx/CVE-2026-120247.json
