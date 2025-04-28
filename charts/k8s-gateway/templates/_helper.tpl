{{/*
Expand the name of the chart.
*/}}
{{- define "k8s-gateway.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "k8s-gateway.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "k8s-gateway.fullname" -}}
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

{{/*
Create a default fully qualified controller name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "k8s-gateway.controller.fullname" -}}
{{- printf "%s-%s" (include "k8s-gateway.fullname" .) .Values.controller.name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "k8s-gateway.labels" -}}
helm.sh/chart: {{ include "k8s-gateway.chart" . }}
{{ include "k8s-gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "k8s-gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "k8s-gateway.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the controller service account to use
*/}}
{{- define "k8s-gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "k8s-gateway.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Create the "name" + "." + "namespace" fqdn
*/}}
{{- define "k8s-gateway.fqdn" -}}
{{- printf "%s.%s" (include "k8s-gateway.fullname" .) .Release.Namespace | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create the matchable regex from domain
*/}}
{{- define "k8s-gateway.regex" -}}
{{- if .Values.domain -}}
{{- .Values.domain | replace "." "[.]" -}}
{{- else -}}
    {{ "unset" }}
{{- end -}}
{{- end -}}

{{/*
  k8s-gateway.dnsEndpoint:
  Returns "true" if "DNSEndpoint" is in .Values.watchedResources,
  or if watchedResources is not set. Otherwise returns "false".
*/}}
{{- define "k8s-gateway.dnsEndpoint" -}}
  {{- if .Values.watchedResources -}}
    {{- $found := false -}}
    {{- range .Values.watchedResources -}}
      {{- if eq . "DNSEndpoint" -}}
        {{- $found = true -}}
      {{- end -}}
    {{- end -}}
    {{- if $found -}}
true
    {{- else -}}
false
    {{- end -}}
  {{- else -}}
false
  {{- end -}}
{{- end }}

{{/*
  k8s-gateway.gatewayAPIs:
  Returns "true" if any one of the Gateway API resources
  (HTTPRoute, TLSRoute, GRPCRoute) is in .Values.watchedResources,
  or if watchedResources is not set; returns "false" otherwise.
*/}}
{{- define "k8s-gateway.gatewayAPI" -}}
  {{- if .Values.watchedResources -}}
    {{- $found := false -}}
    {{- range .Values.watchedResources -}}
      {{- if or (eq . "HTTPRoute") (eq . "TLSRoute") (eq . "GRPCRoute") -}}
        {{- $found = true -}}
      {{- end -}}
    {{- end -}}
    {{- if $found -}}
true
    {{- else -}}
false
    {{- end -}}
  {{- else -}}
false
  {{- end -}}
{{- end }}

{{/*
  k8s-gateway.ingress:
  Returns "true" if "Ingress" is in .Values.watchedResources,
  or if watchedResources is not set. Otherwise returns "false".
*/}}
{{- define "k8s-gateway.ingress" -}}
  {{- if .Values.watchedResources -}}
    {{- $found := false -}}
    {{- range .Values.watchedResources -}}
      {{- if eq . "Ingress" -}}
        {{- $found = true -}}
      {{- end -}}
    {{- end -}}
    {{- if $found -}}
true
    {{- else -}}
false
    {{- end -}}
  {{- else -}}
false
  {{- end -}}
{{- end }}

{{/*
  k8s-gateway.Service:
  Returns "true" if "DNSEndpoint" is in .Values.watchedResources,
  or if watchedResources is not set. Otherwise returns "false".
*/}}
{{- define "k8s-gateway.service" -}}
  {{- if .Values.watchedResources -}}
    {{- $found := false -}}
    {{- range .Values.watchedResources -}}
      {{- if eq . "Service" -}}
        {{- $found = true -}}
      {{- end -}}
    {{- end -}}
    {{- if $found -}}
true
    {{- else -}}
false
    {{- end -}}
  {{- else -}}
false
  {{- end -}}
{{- end }}

{{- define "k8s-gateway.securityContext" -}}
  {{- $securityContext := .Values.securityContext -}}
  {{- if .Values.secure -}}
    {{- $_ := set $securityContext "runAsUser" 1000 -}}
  {{- end -}}

  {{- $securityContext | toYaml -}}
{{- end -}}
