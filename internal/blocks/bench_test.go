package blocks

import (
	"testing"

	"github.com/javirub/gq/internal/codec"
)

const benchSrc = `name: base
{{ if .Values.ingress.enabled }}
host: a
{{ else if .Values.ingress.legacy }}
host: b
{{ else }}
host: c
{{ end }}
{{ if .Values.tls }}
tls: true
{{ end }}
`

func BenchmarkResolve(b *testing.B) {
	f, err := codec.Parse(benchSrc, codec.Yaml)
	if err != nil {
		b.Fatal(err)
	}
	data := map[string]any{
		"Values": map[string]any{
			"ingress": map[string]any{"enabled": true, "legacy": false},
			"tls":     false,
		},
	}
	for b.Loop() {
		res := Resolve(f, data)
		if !res.FullyResolved() {
			b.Fatal("expected fully resolved")
		}
	}
}
