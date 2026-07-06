package codec

import "testing"

// benchSrc is a realistic helmfile-style values template.
const benchSrc = `# environment values
name: {{ .Environment.Name }}
replicas: {{ .Values.replicas | default 3 }}
image: "repo/{{ .Values.tag }}:stable"
{{ if .Values.ingress.enabled }}
ingress:
  host: {{ .Values.host }}
  tls: true
{{ else if .Values.ingress.legacy }}
ingress:
  host: legacy.example.com
{{ else }}
ingress: null
{{ end }}
services:
{{ range .Values.services }}
  - name: {{ .name }}
    port: {{ .port }}
{{ end }}
{{ with .Values.extra }}
extra: {{ . }}
{{ end }}
tail: end
`

func BenchmarkParse(b *testing.B) {
	for b.Loop() {
		if _, err := Parse(benchSrc, Yaml); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProject(b *testing.B) {
	f, err := Parse(benchSrc, Yaml)
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := f.Project(nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeRestore(b *testing.B) {
	for b.Loop() {
		doc, err := Encode(benchSrc, Yaml)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := doc.RestoreFull(doc.Projection()); err != nil {
			b.Fatal(err)
		}
	}
}
