{{/*
Expand the name of the chart.
*/}}
{{- define "osb-broker-go.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "osb-broker-go.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "osb-broker-go.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "osb-broker-go.selectorLabels" -}}
app.kubernetes.io/name: {{ include "osb-broker-go.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Broker fullname
*/}}
{{- define "osb-broker-go.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "osb-broker-go.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Auth secret name
*/}}
{{- define "osb-broker-go.authSecretName" -}}
{{- if .Values.auth.existingSecret }}
{{- .Values.auth.existingSecret }}
{{- else }}
{{- printf "%s-auth" (include "osb-broker-go.fullname" .) }}
{{- end }}
{{- end }}
