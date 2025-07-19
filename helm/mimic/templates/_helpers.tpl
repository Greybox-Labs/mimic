{{/*
Expand the name of the chart.
*/}}
{{- define "mimic.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "mimic.fullname" -}}
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
{{- define "mimic.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "mimic.labels" -}}
helm.sh/chart: {{ include "mimic.chart" . }}
{{ include "mimic.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "mimic.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mimic.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "mimic.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "mimic.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the image name
*/}}
{{- define "mimic.image" -}}
{{- if .Values.global.imageRegistry }}
{{- printf "%s/%s:%s" .Values.global.imageRegistry .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- else }}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) }}
{{- end }}
{{- end }}

{{/*
Generate config file content
*/}}
{{- define "mimic.config" -}}
mode: {{ .Values.config.mode | quote }}

server:
  listen_host: {{ .Values.config.server.listen_host | quote }}
  listen_port: {{ .Values.config.server.listen_port }}
  grpc_port: {{ .Values.config.server.grpc_port }}

database:
  path: {{ .Values.config.database.path | quote }}
  connection_pool_size: {{ .Values.config.database.connection_pool_size }}

recording:
  session_name: {{ .Values.config.recording.session_name | quote }}
  capture_headers: {{ .Values.config.recording.capture_headers }}
  capture_body: {{ .Values.config.recording.capture_body }}
  {{- if .Values.config.recording.redact_patterns }}
  redact_patterns:
    {{- range .Values.config.recording.redact_patterns }}
    - {{ . | quote }}
    {{- end }}
  {{- end }}

mock:
  matching_strategy: {{ .Values.config.mock.matching_strategy | quote }}
  sequence_mode: {{ .Values.config.mock.sequence_mode | quote }}
  not_found_response:
    status: {{ .Values.config.mock.not_found_response.status }}
    body:
      {{- toYaml .Values.config.mock.not_found_response.body | nindent 6 }}

grpc:
  {{- if .Values.config.grpc.proto_paths }}
  proto_paths:
    {{- range .Values.config.grpc.proto_paths }}
    - {{ . | quote }}
    {{- end }}
  {{- end }}
  reflection_enabled: {{ .Values.config.grpc.reflection_enabled }}

export:
  format: {{ .Values.config.export.format | quote }}
  pretty_print: {{ .Values.config.export.pretty_print }}
  compress: {{ .Values.config.export.compress }}

{{- if .Values.config.proxies }}
proxies:
  {{- toYaml .Values.config.proxies | nindent 2 }}
{{- end }}
{{- end }}