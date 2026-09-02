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

{{/*
TLS secret name: an operator-supplied kubernetes.io/tls secret, else the one
cert-manager issues for this release.
*/}}
{{- define "osb-broker-go.tlsSecretName" -}}
{{- if .Values.tls.existingSecret }}
{{- .Values.tls.existingSecret }}
{{- else }}
{{- printf "%s-tls" (include "osb-broker-go.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Port name, used for the container port, the Service targetPort and the probe
port. Renaming it with the scheme keeps `kubectl get svc` honest about what
is actually spoken on the wire.
*/}}
{{- define "osb-broker-go.portName" -}}
{{- if .Values.tls.enabled }}https{{ else }}http{{ end }}
{{- end }}

{{- define "osb-broker-go.containerPort" -}}
{{- if .Values.tls.enabled }}8443{{ else }}8080{{ end }}
{{- end }}

{{/*
Probe scheme. The kubelet does not verify the server certificate on HTTPS
probes, so no CA has to be distributed to the nodes.
*/}}
{{- define "osb-broker-go.probeScheme" -}}
{{- if .Values.tls.enabled }}HTTPS{{ else }}HTTP{{ end }}
{{- end }}
