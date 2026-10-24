# Startup configuration

`Load(args, environ)` returns owned `Settings` without opening a listener,
starting a process or inspecting model files. Command assembly owns startup;
model provisioning and capability checks are implemented by the separate
`internal/adapter/models` adapter. Startup wiring remains a later packet.

Precedence is explicit CLI flags > environment > JSON file > embedded defaults.
Only supplied flags override values. `--config=/path/settings.json` selects the
file; there is no implicit file search or hot reload. A nil environment argument
uses `os.Environ()`; an empty slice disables that layer.

Every source is validated before merging. Unknown or case-mismatched fields,
duplicate JSON properties/environment names/flags, nulls, trailing JSON,
incorrect scalar types and deferred settings fail with `ErrInvalid`. Invalid
values cannot be hidden by a later override. Final validation checks the
combined settings. Arrays replace earlier arrays, including an explicit `[]`.
Raw values and file paths are excluded from error diagnostics.

`load.go` orchestrates these steps. `sources.go` binds the existing settings
tags to Koanf providers and uses `flag.Func` to parse explicit flags once and
reject duplicates. `json.go` performs the required JSON token precheck.
`validation.go` checks typed source values before merging. Koanf/mapstructure
owns structural decoding, native JSON-number conversion, exact key matching and
unknown-key rejection; the loader does not implement a recursive type decoder.

The embedded [defaults.json](defaults.json) implements the canonical defaults
in [core contracts](../../../docs/specs/gpu-whisper-server/core/contracts.yaml).
The contributor checker verifies parity. Do not maintain a second defaults
table here.

## Logging, rate limit and proxy bindings

| JSON setting | Environment | CLI flag |
| --- | --- | --- |
| `logging.level` | `WHISPER_LOG_LEVEL` | `--log-level` |
| `api.rateLimit.enabled` | `WHISPER_RATE_LIMIT_ENABLED` | `--rate-limit-enabled` |
| `api.rateLimit.store` | `WHISPER_RATE_LIMIT_STORE` | `--rate-limit-store` |
| `api.rateLimit.requestsPerSecond` | `WHISPER_RATE_LIMIT_REQUESTS_PER_SECOND` | `--rate-limit-requests-per-second` |
| `api.rateLimit.burst` | `WHISPER_RATE_LIMIT_BURST` | `--rate-limit-burst` |
| `api.rateLimit.idleSeconds` | `WHISPER_RATE_LIMIT_IDLE_SECONDS` | `--rate-limit-idle-seconds` |
| `api.clientIP.source` | `WHISPER_CLIENT_IP_SOURCE` | `--client-ip-source` |
| `api.clientIP.trustedProxyCIDRs` | `WHISPER_TRUSTED_PROXY_CIDRS_JSON` | `--trusted-proxy-cidrs-json` |

Complete field bindings are the `env` and `flag` tags in
[settings.go](settings.go). Boolean flags take explicit `true` or `false` values,
for example `--rate-limit-enabled=false`. Numbers are parsed strictly; array
overrides use JSON. Shell-quote JSON when supplying flags:

```sh
--log-level=debug
--rate-limit-requests-per-second=2
--rate-limit-burst=4
--client-ip-source=x-forwarded-for
--trusted-proxy-cidrs-json='["10.42.0.0/16"]'
```

The proxy CIDR above is an example, not a default or a confirmed cluster range.
Direct peer identity is the default. XFF mode requires explicit trusted CIDRs
and disables implicit private, loopback and link-local trust. `direct` mode
rejects a nonempty trust list; unrestricted `/0` trust is rejected.

V1 supports only a per-process memory rate-limit store. Rate and burst must be
positive; idle TTL must be at least `burst / requestsPerSecond`. The HTTP adapter
uses Echo's existing store interface; no Redis client or Redis setting exists
in this packet.

## Model declarations and preparation

`Models.Validate()` requires unique catalog IDs, one served catalog entry and a
unique download set containing only entries with `source.kind=download`. A served
download may be outside the set: it then requires an already verified cache entry.
Relationships are checked within a source when its catalog and references coexist,
and again after merging all sources. Overrides cannot hide intrinsically invalid
lower sources. Partial sources may supply catalog and references separately.

| JSON setting | Environment | CLI flag |
| --- | --- | --- |
| `models.catalog` | `WHISPER_CATALOG_JSON` | `--catalog-json` |
| `models.served` | `WHISPER_SERVED_MODEL` | `--served-model` |
| `models.downloadSet` | `WHISPER_DOWNLOAD_SET_JSON` | `--download-set-json` |
| `storage.cacheDir` | `WHISPER_CACHE_DIR` | `--cache-dir` |
| `timeouts.provisioningSeconds` | `WHISPER_PROVISIONING_SECONDS` | `--provisioning-seconds` |
| `downloads.attempts` | `WHISPER_DOWNLOAD_ATTEMPTS` | `--download-attempts` |

For an operator-prepared file, use this JSON fragment; other settings retain
canonical defaults:

```json
{
  "models": {
    "catalog": [
      {"id": "base", "source": {"kind": "prepared", "path": "/models/base.bin"}}
    ],
    "served": "base",
    "downloadSet": []
  }
}
```

Prepared paths must be absolute regular files. The adapter opens them read-only,
computes their SHA256, checks optional `sha256`/`expected_bytes` and reads the fixed
GGML prefix. Prepared-only provisioning does not create cache directories. It never
repairs, downloads or deletes prepared files.

Download sources require HTTPS without URL credentials, SHA256 and optional
positive `expected_bytes`. `models.Provision(ctx, opts, client)` copies its
collections and uses one total deadline for sequential download-set preparation
and served-artifact verification. Parent cancellation or an earlier parent deadline
also applies to locks, streams and retry delays. Only the served `Artifact` is
returned; extra verified downloads remain cached.

The Linux cache layout is:

```text
<cacheDir>/artifacts/<lowercase-sha256>.bin
<cacheDir>/artifacts/<lowercase-sha256>.partial-<random>
<cacheDir>/locks/<lowercase-sha256>.lock
```

Operations use `os.Root` confinement and reject symlink/nonregular artifacts and
locks. A stable, never-unlinked `flock` file serializes processes for each artifact.
Verified hits cause zero source requests. Missing/corrupt served downloads outside
the set fail without network fallback.

Downloads stream with constant memory into private same-directory partials.
Checksum, optional size and metadata checks precede file sync, atomic rename and
directory sync. Redirects stay HTTPS, reject credentials, are bounded and use no
cookies. Attempts share the total deadline. The injected client's configuration
and transport remain caller-owned.

Failures preserve prior artifacts and remove only owned partials, or stale partials
for the same artifact while holding its lock. A killed process may leave a partial
for the next owner to recover. Unrelated files/partials remain. Cache ownership
persists across provisioning and request cleanup; this adapter has no eviction
operation. Filesystem/network/integrity/metadata/deadline failures use
`domain.ProvisioningFailed`, with internal causes available through `errors.Is` /
`errors.As`. Public errors do not include source URLs, paths or raw content.

`ReadMetadata` reads exactly the 48-byte little-endian prefix used by
[the pinned native loader](https://github.com/OpenWhispr/whisper.cpp/blob/6ac14546046ffb17d144a08bcad65337e535cf5/src/whisper.cpp).
Vocabulary determines multilingual capability (`n_vocab >= 51865`); names do not.
This does not validate full tensors or prove native loading, GPU use or readiness;
those require later runtime checks.

## Configuration compatibility fingerprint

`CanonicalFingerprint(settings)` validates settings and returns lowercase SHA256
of deterministic JSON with every field present in this order:

```json
{"version":1,"model":{"id":"base","source":{"kind":"prepared","url":"","path":"/models/a.bin","sha256":"","expected_bytes":0}},"backend":{"gpu_device":0}}
```

This vector produces
`bc7ffae51e3da44601328b3e545adc57538efc7526a44040ad42c607b7b80714`.
Only the declared served source and GPU ordinal participate. Catalog order,
unserved models, download selection, cache path, addresses/ports, logging,
rate/proxy settings, queue, limits and timeouts are excluded. The API can compute
it without opening files, including prepared models without declared checksums.
It checks configuration compatibility; actual artifact integrity and protocol
authentication are separate responsibilities.
