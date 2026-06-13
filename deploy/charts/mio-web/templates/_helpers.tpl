{{- define "mio-web.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mio-web.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "mio-web.api.fullname" -}}
{{- printf "%s-api" (include "mio-web.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mio-web.frontend.fullname" -}}
{{- printf "%s-frontend" (include "mio-web.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "mio-web.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "mio-web.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "mio-web.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mio-web.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "mio-web.api.selectorLabels" -}}
{{ include "mio-web.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end -}}

{{- define "mio-web.frontend.selectorLabels" -}}
{{ include "mio-web.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end -}}
