{{- define "whisper-server.validate" -}}
{{- $c := include "whisper-server.config" . | mustFromJson -}}
{{- if ne (int .Values.backend.gpu.requests) (int .Values.backend.gpu.limits) -}}
  {{ fail "backend GPU requests and limits must match" -}}
{{ end -}}
{{- if ge (int $c.backend.gpuDevice) (int .Values.backend.gpu.requests) -}}
  {{ fail "GPU ordinal exceeds allocated devices" -}}
{{ end -}}
{{- if eq (int $c.backend.port) (int $c.backend.nativePort) -}}
  {{ fail "backend and native ports must differ" -}}
{{ end -}}

{{- $backendURL := urlParse $c.api.backendUrl -}}
{{- if or
  (not (has $backendURL.scheme (list "http" "https")))
  (empty $backendURL.host)
  (not (empty $backendURL.userinfo))
  (not (empty $backendURL.query))
  (not (empty $backendURL.fragment))
  (not (has $backendURL.path (list "" "/")))
-}}
  {{ fail "invalid private backend URL" -}}
{{ end -}}

{{- if has "ReadOnlyMany" .Values.cache.accessModes -}}
  {{ fail "model cache requires writable access modes" -}}
{{ end -}}

{{- if and
  .Values.terminationGracePeriodSeconds
  (lt (int .Values.terminationGracePeriodSeconds) (int (add $c.timeouts.shutdownSeconds $c.timeouts.cleanupSeconds 5)))
-}}
  {{ fail "termination grace is shorter than shutdown plus cleanup plus 5" -}}
{{ end -}}

{{- $startupTicks := div
  (add
    $c.timeouts.provisioningSeconds
    $c.timeouts.operationSeconds
    $c.timeouts.cleanupSeconds
    (sub .Values.probes.periodSeconds 1)
  )
  .Values.probes.periodSeconds
-}}
{{- if gt $startupTicks 2147483647 -}}
  {{ fail "startup probe budget exceeds Kubernetes int32 bounds" -}}
{{ end -}}

{{- if and
  (gt (int .Values.replicaCount) 1)
  .Values.cache.existingClaim
  (not (has "ReadWriteMany" .Values.cache.accessModes))
-}}
  {{ fail "shared writable cache requires ReadWriteMany" -}}
{{ end -}}
{{- if and
  (gt (int .Values.replicaCount) 1)
  .Values.prepared.existingClaim
  (not (or (has "ReadOnlyMany" .Values.prepared.accessModes) (has "ReadWriteMany" .Values.prepared.accessModes)))
-}}
  {{ fail "prepared claim cannot be shared across replicas" -}}
{{ end -}}

{{- if and .Values.ingress.enabled (empty .Values.ingress.host) -}}
  {{ fail "enabled Ingress requires a host" -}}
{{ end -}}

{{- $rate := $c.api.rateLimit -}}
{{- if lt (float64 $rate.idleSeconds) (divf $rate.burst $rate.requestsPerSecond) -}}
  {{ fail "rate-limit TTL must cover burst refill" -}}
{{ end -}}
{{- if or
  (and (eq $c.api.clientIP.source "direct") (not (empty $c.api.clientIP.trustedProxyCIDRs)))
  (and (eq $c.api.clientIP.source "x-forwarded-for") (empty $c.api.clientIP.trustedProxyCIDRs))
-}}
  {{ fail "client-IP source and explicit trusted proxies disagree" -}}
{{ end -}}

{{- $prefixes := dict -}}
{{- range $cidr := $c.api.clientIP.trustedProxyCIDRs -}}
  {{- $normalized := include "whisper-server.cidr" $cidr -}}
  {{- if hasKey $prefixes $normalized -}}
    {{ fail "duplicate normalized trusted proxy prefix" -}}
  {{ end -}}
  {{- $_ := set $prefixes $normalized true -}}
{{- end -}}

{{- $ids := dict -}}
{{- range $model := $c.models.catalog -}}
  {{- if hasKey $ids $model.id -}}
    {{ fail "duplicate model ID" -}}
  {{ end -}}
  {{- $_ := set $ids $model.id $model.source.kind -}}
  {{- if eq $model.source.kind "prepared" -}}
    {{- if or
      (empty $.Values.prepared.existingClaim)
      (not (hasPrefix "/prepared/" $model.source.path))
      (contains "/../" $model.source.path)
    -}}
      {{ fail "prepared models require a read-only claim under /prepared" -}}
    {{ end -}}
  {{- else -}}
    {{- $u := urlParse $model.source.url -}}
    {{- if or (ne $u.scheme "https") (empty $u.host) (not (empty $u.userinfo)) (not (empty $u.fragment)) -}}
      {{ fail "download source must be HTTPS without credentials or fragment" -}}
    {{ end -}}
  {{- end -}}
{{- end -}}
{{- if not (hasKey $ids $c.models.served) -}}
  {{ fail "served model is absent" -}}
{{ end -}}
{{- range $id := $c.models.downloadSet -}}
  {{ if ne (get $ids $id) "download" -}}
    {{ fail "downloadSet must reference download models" -}}
  {{ end -}}
{{ end -}}

{{- $cache := clean $c.storage.cacheDir -}}
{{- range $reserved := list "/etc/whisper" "/tmp" "/prepared" -}}
  {{- if or
    (eq $cache "/")
    (eq $cache $reserved)
    (hasPrefix (printf "%s/" $reserved) $cache)
    (hasPrefix (printf "%s/" $cache) $reserved)
  -}}
    {{ fail "cache must be separate from configuration/transient/prepared storage" -}}
  {{ end -}}
{{- end -}}

{{- $wav := add 44 (mul $c.limits.decodedSeconds 32000) -}}
{{- if gt
  (int64 (add $c.queue.waitingCapacity 2))
  (div (sub 9223372036854775807 $c.limits.audioBytes) (add $c.limits.audioBytes $wav))
-}}
  {{ fail "aggregate scratch bounds overflow supported storage capacity" -}}
{{ end -}}

{{- $apiBytes := add (mul (add $c.queue.waitingCapacity 2) (add $c.limits.audioBytes $wav)) $c.limits.audioBytes -}}
{{- if lt (include "whisper-server.bytes" .Values.transientStorage.api | int64) (int64 $apiBytes) -}}
  {{ fail "API scratch is smaller than bounded staging/decoder data" -}}
{{ end -}}
{{- if lt
  (include "whisper-server.bytes" .Values.transientStorage.backend | int64)
  (int64 (add $wav $c.limits.promptBytes 65536))
-}}
  {{ fail "backend scratch is smaller than bounded inference data" -}}
{{ end -}}
{{- end -}}
