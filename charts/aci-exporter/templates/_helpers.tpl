{{/*
Expand the name of the chart.
*/}}
{{- define "aci-exporter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "aci-exporter.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "aci-exporter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "aci-exporter.labels" -}}
helm.sh/chart: {{ include "aci-exporter.chart" . }}
{{ include "aci-exporter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "aci-exporter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "aci-exporter.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "aci-exporter.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "aci-exporter.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Name of the secret holding fabric credentials
*/}}
{{- define "aci-exporter.secretName" -}}
{{- default (include "aci-exporter.fullname" .) .Values.credentials.existingSecret }}
{{- end }}

{{/*
Environment variable prefix for a fabric, as expected by aci-exporter
*/}}
{{- define "aci-exporter.fabricEnv" -}}
{{- printf "ACI_EXPORTER_FABRICS_%s" (. | replace "-" "_" | upper) }}
{{- end }}
