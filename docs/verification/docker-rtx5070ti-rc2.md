# P07 — rc.2 RTX 5070 Ti Laptop verification checkpoint

Status: **incomplete; candidate not GPU-qualified** (2026-10-25).

## Selected candidate

- Version: [v0.1.0-rc.2](https://github.com/swimmwatch/openwhispr-whisper-server/releases/tag/v0.1.0-rc.2).
- Source: `3faa5cffa82917e590994cf1d2edcd2256d74c33`.
- API OCI index: `sha256:d32e6678d87315ae99e8d68c35818c0aff45d05c82cf1ccb7905fcbaf2731391`.
- Backend OCI index: `sha256:803f0d7ac1459681bb5f1a580d34e0abfd584d26d7195aa2ab1f14317f9f0b15`.
- Published images were used unchanged through the canonical Compose stack; no local replacement image or CPU fallback was selected.

## Observed environment and fixture

- Local NVIDIA GeForce RTX 5070 Ti Laptop GPU, driver `595.99.02`, reported total VRAM `12227 MiB`.
- Docker Engine `29.9.2`, Compose `5.6.1`; GPU enumeration also succeeded inside the exact candidate backend image.
- The initial restricted-session `nvidia-smi` attempt exited 9. After the user changed the session's file/access policy, the same command succeeded. No host driver or global configuration was changed; the earlier failure is not evidence of a defective host driver.
- Public fixture: the eleven-second JFK recording from the [pinned upstream sample](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5f/samples/jfk.wav).
- WAV SHA256: `59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e`.
- The underlying government inaugural-address recording is documented as public domain in the [Wikimedia source/licensing record](https://commons.wikimedia.org/wiki/File:JFK_inaugural_address.ogg). The pinned upstream repository is MIT licensed. No personal recording was used.
- Derivative format fixtures were generated with host FFmpeg `6.1.1-3ubuntu5`; this is an input encoder, not a substitute for the candidate's packaged decoder/toolchain identity. Their checksums and encoder settings remain in private task evidence.

## Actual results

| Check | Result |
| --- | --- |
| Required GPU visible on host and inside candidate backend | Passed enumeration only; this alone is not inference proof |
| Canonical stack reaches public readiness with pinned `base` | Passed |
| First public multipart transcription of the identified speech | Passed, no transcript retained in this report |
| Actual non-root/read-only component identities and private backend publication | Passed baseline inspection |
| Sixty sequential public requests, rate limiter explicitly disabled for this workload | **Failed: 11 successful, 49 HTTP 503 `not_ready`** in the diagnostic run |
| Complete resident/resource/lifecycle/client/performance acceptance | **Not established** |

During the failed repeated workload, the backend's health requests continued to return HTTP 200 and all inference requests actually submitted to it returned HTTP 200. The API rejected the remaining requests with `not_ready` before private inference. Deterministic regressions confirmed stale owned-busy health after completion, including a delayed in-flight Busy response. No delay/retry was added to turn the failed steady-state check green.

A narrow local source correction now serializes health observation commits and obtains one bounded fresh dependency observation after successful inference before retiring its Busy owner. It preserves nonblocking public probes, unclaimed-busy/CUDA/configuration/generation failure checks, end-to-end cancellation/deadlines and completed-inference success under a health-only failure. The complete transcription package passed ten race runs and six isolated PROVE mutations; the broader non-GPU Go suites and vet also passed after correcting a separate fake-CLI test readiness handshake. This is local source evidence only: the published rc.2 images do not contain the fix, and its real repeated-request failure is not cleared.

All test-owned containers/networks were torn down through bounded Compose cleanup, without `--volumes`; named model caches were preserved. No Kubernetes, Home Lab, stable/latest, registry publication or tag mutation was performed.

## Helper validation is separate evidence

The shared HTTP workload/statistics/Compose helpers have deterministic local behavior tests, race/static checks and isolated PROVE mutations. Those tests do not establish CUDA activity, bounded resident resources, real client compatibility or successful P07 completion. There is no complete acceptance report or stable eligibility claim.

## Remaining work and ownership

The implementation agent owns the deterministic regression, narrow implementation correction where authorized, remaining harness scenarios, actual client/format/resource/lifecycle evidence, and sanitized read-back. A runtime/build-input correction requires a separately authorized R01 invocation and a new immutable RC before affected acceptance can be renewed; rc.2 must not be overwritten. P08 remains separately invoked and must use its prepared task workflow only after its own gates pass.
