{{- define "agentic-controller.name" -}}agentic-controller{{- end -}}

{{- define "agentic-controller.labels" -}}
app.kubernetes.io/name: {{ include "agentic-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "agentic-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "agentic-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
