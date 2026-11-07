# Security policy

## Supported versions

Security reports about published artifacts and current development source are
welcome. Identify the exact release/source revision and immutable image/chart
identities; a source fix is not necessarily present in a published image. No
blanket support window or end-of-life schedule is promised here.

The [rc.6 report](<docs/verification/release-rc6.md>) records expired scans and
exception/component reviews, not a current clean security result. The
[v1.0.0 release-only exception](<docs/verification/release-1.0.0-notes.md>) discloses
those gaps and the absence of the working-tree short-audio mitigation from the
selected rc.6 images. It does not waive checks for other releases or establish
that these artifacts are safe for unrestricted exposure.

V1 has no built-in authentication. Restrict access to trusted clients and supply
appropriate transport/access controls outside the service. Do not expose the
supervisor or native backend directly. See [operations](<docs/operations.md>).

## Private vulnerability reporting

Use [GitHub private vulnerability reporting](https://github.com/swimmwatch/openwhispr-whisper-server/security/advisories/new).
Do not open a public issue for a suspected vulnerability. Include the affected
source revision or artifact digest, a minimal synthetic reproduction, expected
and observed behavior, and the security impact. Exclude secrets, real audio,
private transcripts and unnecessary personal information.

Reports are handled by the repository maintainer. No fixed response deadline is
promised. Coordinate disclosure privately before publishing exploit details.
Ordinary bugs and configuration questions belong in [support](SUPPORT.md).
