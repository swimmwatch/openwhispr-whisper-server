# Local pre-release defect verification

**Local server defect verification complete; full P07 remains incomplete**
(2026-10-26, Europe/Moscow). Server HTTP/GPU groups passed on the final local
images. The mixed server/client test command exited 1 solely on Electron startup;
that command is not relabeled as an aggregate pass. This is not immutable-candidate
qualification.
Published `v0.1.0-rc.2` is unchanged and GPU-unqualified. No R01 invocation,
commit/push, publication, tag, stable-channel or cluster operation follows.

## Exact local inputs

Base HEAD `364f1e49e080b317f403684ce5d80e3676309b1c` plus recorded uncommitted
changes; runtime identity before/after the cached canonical pair build:
`sha256:2cbb5d9db5451a3cbba00d76af845f35e4e86454bfe113eca10bda45c99eddbd`.

| Local artifact | Immutable reference |
| --- | --- |
| API | `whisper-p07-local-api@sha256:3ca14e840b41d0b5f5ebb819f4714e06c75b32890c85590a3768fd739d0b00e8` |
| Backend | `whisper-p07-local-backend@sha256:f1decddf0073329ad616a55743062561e3157b36fdde5a3904ae629ba38fe435` |
| HTTPS source-test derivative | `whisper-p07-local-backend@sha256:cdb4db53bc82ab1e447db0ae61c6d116f54de846becc9bbfa28fe7ce07acf76f` |

The [private receipt](../../.local/p07/local-fixed-Wd6DfmOw/receipt.json) records
`local_validation_not_release`, no registry push and no release qualification.
Earlier `f420…` and `5ad…` image results are historical only. The source-test
image changes only backend-process `SSL_CERT_FILE` and a fixture label; its public
CA uses the existing read-only prepared mount and the private key stays in the
test process. Host trust, canonical Compose, app UID/capabilities and native source
are unchanged. Tests used the actual RTX 5070 Ti Laptop GPU, not enumeration alone.

## Corrected defects

- **Stale owned Busy after success:** serialize health observation/commit and
  confirm bounded fresh health before retiring the successful request owner.
  Foreign Busy, CUDA/configuration/generation and overall-deadline guards remain.
- **Controlled retirement/rebind failure:** close owned idle native-health TCP
  connections before signaling the worker. Pinned native SO_REUSEPORT-only sockets
  could otherwise leave native-port TIME_WAIT incompatible with the exclusive probe.
- **Unexpected native loss/rebind failure:** owned private-native TCP connections
  use `SetLinger(0)` at teardown. Complete replies are still fully read/validated;
  lost/truncated replies fail and uncertain submitted POSTs are not replayed.
  Process-group stop/Wait/reap, fixed ports, public transport, app capabilities and
  exclusive foreign-listener/bound-socket guards are unchanged. CPU/state-specific
  tests support the reset-before-TIME_WAIT mechanism; no host sysctl was changed.

## Actual checks on these final local images

The final [resumed run](<../../.local/p07/local-fixed-Wd6DfmOw/local-final-resumed.log>)
completed in **144.277s, exit 1**. All server HTTP/GPU/resource/model/lifecycle
checks and both genuine OpenAI SDK representations passed. The sole failure was
OpenWhispr Electron startup: its sandbox aborted before any client HTTP request.
CPU-only diagnosis and the public API request count distinguish this environment
prerequisite from a server transcription failure. No sandbox bypass, root helper
installation or host setting change was performed. The user clarified that the
current fix stage concerns server HTTP behavior; Electron compatibility remains
unverified for full client/P07 acceptance, not a blocker for the local server
report or publication of an explicitly unqualified new RC.

The shared ordinary-input speech oracle now requires independent whole-word JFK
identity, clause and action cues, not arbitrary nonempty text or one exact token.
The explicit temperature-1 boundary checks protocol, nonempty output, media type
and one request; its lexical quality is not qualified. Public config/model/CA bind
modes are explicit under restrictive `umask 077`; private evidence directories
and files retain 0700/0600 permissions. The resumed run exercised those modes.

Historical evidence is retained without retroactive passing: the
[first run](<../../.local/p07/local-fixed-Wd6DfmOw/all-local-gpu.log>) failed MP3/text
single-word assertions; a [later replay](<../../.local/p07/local-fixed-Wd6DfmOw/final-local-all.log>)
failed leftover rigid WAV/limiter speech checks. Source permits native fallback,
but the original unretained responses do not prove their exact fallback cause.
The [restrictive-umask attempt](<../../.local/p07/local-fixed-Wd6DfmOw/local-final-validated.log>)
failed fixture startup, and its [interrupted recording](<../../.local/p07/local-fixed-Wd6DfmOw/local-final-validated-permissions-corrected.log>)
has no terminal aggregate result. None of these attempts is called passed.

| Check | Verified local result |
| --- | --- |
| Resident regression | 60/60 successful sequential requests, unchanged native worker; no retries/delays hiding failures |
| Public/client matrix | All 51 public cases (10 formats, 7 options, 34 invalid/raw) and genuine OpenAI 2.11.0 JSON/text passed; current OpenWhispr 1.10.4/Electron 41.10.7 transport remains unverified because sandbox startup failed before HTTP |
| Bounds/isolation/privacy | Exact file cap/one-byte excess, verified 901s decoded rejection, prompt bound, distinct recordings/reset, emitted-log and owned-runtime-file marker checks passed |
| FIFO/expiry/cancellation | Controlled order/overflow, queued disconnect, active cancellation and queued deadline without dispatching expired work passed |
| Lifecycle | 8/8 passed in 45.14s, including native SIGKILL recovery (4.89s), ordinary restart and retained-prepared down/up; reduced 3s operation deadline returned 408 `operation_timeout` without transport error in 3.521s, then fresh inference |
| Active API termination | Graceful active drain and forced API death/recovery passed |
| GPU absence | Exact backend without allocation failed closed after native startup; no ready/CUDA-ready event or CPU-ready fallback |
| Cache/models | Counted HTTPS cold download, deny-trap offline restart, prepared/missing-prepared without fallback, pinned tiny restart, deterministic unsupported-ftype init failure and retained valid base checksum passed |
| Resident observations | Exact-900s input and three blocks of twenty; actual native-PID-correlated CUDA activity, serial RSS/FD/file snapshots and zero recurring model open/read/mapping operations passed |
| CPU/tooling | Full Go race suite and vet passed; Ruff format/lint, mypy (38 sources) and actionlint passed. Python tooling: 117 tests with 12 disclosed C02/Helm fixture skips; actual-SDK unit suite: 12 tests, no skips |

Final [resident measurements](<../../.local/p07/run/local-1385521375/resident-measurements.json>):
60 successes/0 errors; public median **99.443ms**, p95 **436.563ms**; actual queue
median 0.325ms, private HTTP median 90.500ms, API-overhead median 9.123ms. Observed
native initialization-to-ready interval: 550.225ms, **not pure weight-file load**.
Fourteen native-PID GPU samples recorded peak SM utilization 37% and framebuffer
557842432 bytes. Model-path operations: zero. API FD count was 12 after every block;
API RSS snapshots were 17567744/17985536/17993728 bytes. These are observations,
not a numeric performance SLO or proof that every possible small leak is excluded.

Logging was debug; rate limiting was disabled for queue/resident workloads and
tested enabled separately. Trace/sampling overhead is included. Snapshots are
serial/non-atomic; replacement sampling has 100ms scheduling plus CLI latency,
not continuous visibility. The tracer joins only the backend PID namespace,
without network, using read-only strace/libraries and SYS_PTRACE/KILL; raw
read/pread/recv arguments avoid buffer dumps. Unknown per-request resource fields
remain null despite independent phase peaks. Cache volumes were retained.

## Reproduction and remaining scope

From the repository root with the pinned Go 1.27.3 compiler (not an older shim):

```sh
rtk proxy env P07_LOCAL_TESTS=1 \
  P07_LOCAL_RECEIPT="$PWD/.local/p07/local-fixed-Wd6DfmOw/receipt.json" \
  P07_LOCAL_SOURCE_BIND_IP=CONFIRMED_DOCKER_BRIDGE_ADDRESS \
  P07_LOCAL_CA_BACKEND_IMAGE=whisper-p07-local-backend@sha256:cdb4db53bc82ab1e447db0ae61c6d116f54de846becc9bbfa28fe7ce07acf76f \
  GOTOOLCHAIN=local GOWORK=off \
  go test ./tests/acceptance -run '^TestLocal' -count=1 -v -timeout=45m
```

The private bridge selection is intentionally not copied into this report. This
opt-in local command builds/publishes nothing and is not the planned full
candidate qualification command.


No further server replay is required to close this local defect-fix report.
The parent next owns release preparation after explicit commit/push/publication
authorization; the prepared [pipeline monitor](<pipeline-monitor.md>) remains
unarmed until an actual new run is selected. Full P07 still needs genuine current
OpenWhispr compatibility and its complete absence/leak-sensitivity calibration (including all prescribed
per-success reload/raw-output/overlap mutations), configured idle/retention and
failure-envelope coverage, and any feasible direct-native comparison not executed
here. Real clients cover base/JFK WAV, not full UI or every client-by-codec pairing.
Later qualification must use a separately authorized new immutable candidate and
required registry/channel evidence; these local images cannot qualify rc.2.
P07 stays unchecked. No Kubernetes test/workflow or deployment was started.
