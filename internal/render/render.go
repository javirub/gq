// Package render executes a Go-templated file with the gq data context,
// producing plain YAML/JSON.
package render

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
)

// Render executes the template source against the data context. Template
// functions are text/template built-ins plus sprig (like helm/helmfile).
// Missing keys resolve to zero values (helm semantics, so the common
// `.x | default "y"` idiom works), but any "<no value>" leaking into the
// output is reported as an error instead of silently emitted.
func Render(src string, data map[string]any) (string, error) {
	tmpl, err := template.New("gotmpl").
		Funcs(sprig.TxtFuncMap()).
		Option("missingkey=zero").
		Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	rendered := buf.String()
	if strings.Contains(rendered, "<no value>") {
		return "", fmt.Errorf("the template referenced keys that are missing from the data context (the output contains %q)", "<no value>")
	}
	return rendered, nil
}
