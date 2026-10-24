{{- define "whisper-server.name" -}}
{{ default .Chart.Name .Values.nameOverride -}}
{{- end }}
{{- define "whisper-server.fullname" -}}
{{ default (printf "%s-%s" .Release.Name (include "whisper-server.name" .) | trunc 45 | trimSuffix "-") .Values.fullnameOverride -}}
{{- end }}
{{- define "whisper-server.labels" -}}
app.kubernetes.io/name: {{ include "whisper-server.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
{{- end }}
{{- define "whisper-server.selector" -}}
app.kubernetes.io/name: {{ include "whisper-server.name" .root | quote }}
app.kubernetes.io/instance: {{ .root.Release.Name | quote }}
app.kubernetes.io/component: {{ .role | quote }}
{{- end }}
{{- define "whisper-server.config" -}}
{{- $base := mustFromJson (.Files.Get "files/defaults.json") -}}
{{- mustMergeOverwrite (deepCopy $base) .Values.config | mustToJson -}}
{{- end }}
{{- define "whisper-server.image" -}}
{{- if .digest -}}
  {{ printf "%s@%s" .repository .digest -}}
{{- else -}}
  {{ printf "%s:%s" .repository .tag -}}
{{- end -}}
{{- end }}
{{- define "whisper-server.bytes" -}}
{{- $amount := int64 (regexFind "^[0-9]+" .) -}}
{{- $factor := ternary 1073741824 1048576 (hasSuffix "Gi" .) -}}
{{- if or (le $amount 0) (gt $amount (div 9223372036854775807 $factor)) -}}
  {{ fail "storage quantity exceeds supported capacity" -}}
{{ end -}}
{{- mul $amount $factor -}}
{{- end }}
