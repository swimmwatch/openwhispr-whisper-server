# Private resident backend

`cmd/backend` assembles the existing configuration loader, model provisioner,
fingerprint and this supervisor. It serves the private HTTP protocol described in
[core contracts](../../../docs/specs/gpu-whisper-server/core/contracts.yaml)
under `private_backend`. Public transcription routing, decoding and scheduling
belong to later API packets. Do not expose this listener to clients.

Only `GET /health` and `POST /inference` are routed. Health includes the current
generation and fingerprint; a caller must copy both into its inference headers
along with protocol 1 and a positive remaining timeout in milliseconds. Busy
healthy inference remains ready for probes; a second inference is rejected
without a waiting queue. There are no load, conversion or file-serving routes.

Private inference is multipart: `file` contains canonical mono 16 kHz signed
16-bit PCM WAV; optional fields are `language`, `prompt`, `temperature` and
`response_format=json`. The filename is not a path. The response is JSON text or
the shared safe HTTP error envelope. The normalized WAV budget follows configured
decoded duration, capped by classic RIFF/WAV representation; native JSON reads
share that byte budget. No additional public configuration defaults are defined.

`Infer` borrows its bounded WAV stream until return, including cancellation. Its
owner must keep the stream readable; closing the native transport is not proof
that inference stopped. The supervisor stops, kills and reaps an uncertain
native process group before replacement or reuse. Success retains the process.
Exactly one lifecycle owner calls `Run`; only that owner waits for native exit.

The HTTP adapter owns staged files through parsing, forwarding, cancellation and
panic recovery. It interrupts blocked network reads on cancellation and releases
the operation only after cleanup. Files live in locked, marked disposable work
storage, separate from read-only prepared models and the persistent model cache.

The command expects `whisper-server` and `nvidia-smi` on PATH. Its native listener
is loopback-only; startup probes require exact pinned CUDA allocation evidence
and a compatible live GPU query. SIGTERM first marks unready and drains, then
stops/reaps native work and closes the HTTP server. Lifecycle logs contain only
allowlisted structured events and safe error codes. Tests use subprocess fixtures;
actual CUDA, images and Kubernetes acceptance remain separate packets.
