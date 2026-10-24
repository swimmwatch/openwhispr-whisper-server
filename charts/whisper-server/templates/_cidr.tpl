{{/* Helm lacks netip.ParsePrefix: validate and normalize prefixes for duplicate checks. */}}

{{- define "whisper-server.ipv4hex" -}}
{{- $octets := splitList "." . -}}
{{- if ne (len $octets) 4 -}}
  {{ fail "invalid IPv4 trusted proxy" -}}
{{ end -}}
{{- range $octet := $octets -}}
  {{- if or (not (regexMatch "^(0|[1-9][0-9]{0,2})$" $octet)) (gt (int $octet) 255) -}}
    {{ fail "invalid IPv4 trusted proxy" -}}
  {{ end -}}
  {{- printf "%02x" (int $octet) -}}
{{- end -}}
{{- end -}}

{{- define "whisper-server.cidr" -}}
{{- $parts := splitList "/" . -}}
{{- if ne (len $parts) 2 -}}
  {{ fail "invalid trusted proxy CIDR" -}}
{{ end -}}
{{- $ip := index $parts 0 -}}
{{- $prefix := index $parts 1 -}}
{{- if not (regexMatch "^[1-9][0-9]{0,2}$" $prefix) -}}
  {{ fail "wildcard or invalid trusted proxy prefix" -}}
{{ end -}}
{{- $bits := int $prefix -}}

{{- $hex := "" -}}
{{- $family := "ipv4" -}}
{{- if not (contains ":" $ip) -}}
  {{- if gt $bits 32 -}}
    {{ fail "invalid IPv4 prefix" -}}
  {{ end -}}
  {{- $hex = include "whisper-server.ipv4hex" $ip -}}
{{- else -}}
  {{- $family = "ipv6" -}}
  {{- if gt $bits 128 -}}
    {{ fail "invalid IPv6 prefix" -}}
  {{ end -}}
  {{- if contains "." $ip -}}
    {{- $tail := last (splitList ":" $ip) -}}
    {{- $v4 := include "whisper-server.ipv4hex" $tail -}}
    {{- $ip = printf "%s%s:%s" (trimSuffix $tail $ip) (substr 0 4 $v4) (substr 4 8 $v4) -}}
  {{- end -}}
  {{- $sides := splitList "::" $ip -}}
  {{- if gt (len $sides) 2 -}}
    {{ fail "invalid IPv6 compression" -}}
  {{ end -}}
  {{- $groups := list -}}
  {{- if eq (len $sides) 1 -}}
    {{- $groups = splitList ":" $ip -}}
    {{- if ne (len $groups) 8 -}}
      {{ fail "invalid IPv6 group count" -}}
    {{ end -}}
  {{- else -}}
    {{- $left := list -}}
    {{- $right := list -}}
    {{- if index $sides 0 -}}
      {{ $left = splitList ":" (index $sides 0) -}}
    {{ end -}}
    {{- if index $sides 1 -}}
      {{ $right = splitList ":" (index $sides 1) -}}
    {{ end -}}
    {{- $missing := int (sub (sub 8 (len $left)) (len $right)) -}}
    {{- if lt $missing 1 -}}
      {{ fail "invalid IPv6 group count" -}}
    {{ end -}}
    {{- $groups = $left -}}
    {{- range until $missing -}}
      {{ $groups = append $groups "0" -}}
    {{ end -}}
    {{- $groups = concat $groups $right -}}
  {{- end -}}
  {{- range $group := $groups -}}
    {{- if not (regexMatch "^[a-fA-F0-9]{1,4}$" $group) -}}
      {{ fail "invalid IPv6 trusted proxy" -}}
    {{ end -}}
    {{- $hex = printf "%s%s" $hex (printf "%4s" (lower $group) | replace " " "0") -}}
  {{- end -}}
  {{- if hasPrefix "00000000000000000000ffff" $hex -}}
    {{ fail "IPv4-mapped trusted prefixes are unsupported" -}}
  {{ end -}}
{{- end -}}

{{- $digits := splitList "" "0123456789abcdef" -}}
{{- $masked := "" -}}
{{- range $index, $digit := splitList "" $hex -}}
  {{- $remaining := int (sub $bits (mul $index 4)) -}}
  {{- if le $remaining 0 -}}
    {{- $masked = printf "%s0" $masked -}}
  {{- else if ge $remaining 4 -}}
    {{- $masked = printf "%s%s" $masked $digit -}}
  {{- else -}}
    {{- $value := 0 -}}
    {{- range $number, $candidate := $digits -}}
      {{ if eq $candidate $digit -}}
        {{ $value = $number -}}
      {{ end -}}
    {{ end -}}
    {{- $unit := index (list 16 8 4 2) $remaining | int -}}
    {{- $masked = printf "%s%s" $masked (index $digits (int (mul (div $value $unit) $unit))) -}}
  {{- end -}}
{{- end -}}

{{- printf "%s:%s/%d" $family $masked $bits -}}
{{- end -}}
